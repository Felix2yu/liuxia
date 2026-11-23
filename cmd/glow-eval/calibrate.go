package main

import (
	"flag"
	"fmt"
	"math"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

var calKeys = []string{
	"curtainV2", "gapV3", "gapV2", "low", "precipV2", "aod", "vis", "pm",
	"dew", "wind", "cape", "curtain", "gap", "washedV2",
}

var calTargets = []struct {
	name string
	min  int
}{
	{"≥微烧", 1},
	{"≥小到中烧", 3},
	{"≥中到大烧", 4},
	{"≥大烧", 5},
}

type calMetrics struct {
	N       int
	Exact   int
	Within1 int
	MAE     float64
	MSE     float64
	QWK     float64
}

type scoreGroup struct {
	score   float64
	cnt     int
	sum     float64
	sumsq   float64
	absCost [6]float64
}

func calibrateMain(args []string) {
	fs := flag.NewFlagSet("calibrate", flag.ExitOnError)
	cacheDir := fs.String("cache", ".cache/glow-eval", "标注与历史输入缓存目录")
	out := fs.String("out", "", "报告输出文件；为空则输出到标准输出")
	folds := fs.Int("folds", 5, "按月分组的交叉验证折数")
	lambda := fs.Float64("lambda", 10, "分项重加权的 L2 正则系数")
	refresh := fs.Bool("refresh", false, "忽略缓存重新抓取")
	_ = fs.Parse(args)
	if *folds < 2 {
		*folds = 2
	}

	client := &http.Client{Timeout: 45 * time.Second}
	labels, err := fetchLabels(client, *cacheDir, *refresh)
	if err != nil {
		fmt.Fprintln(os.Stderr, "抓取标注失败:", err)
		os.Exit(1)
	}
	rows, err := evaluateAll(client, *cacheDir, *refresh, labels)
	if err != nil {
		fmt.Fprintln(os.Stderr, "评估失败:", err)
		os.Exit(1)
	}

	report := buildCalibrationReport(rows, *folds, *lambda)
	if *out == "" {
		fmt.Print(report)
		return
	}
	if err := os.MkdirAll(dirOf(*out), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "创建输出目录失败:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*out, []byte(report), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "写入报告失败:", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "标定报告已写入 %s\n", *out)
}

func levelFromBounds(score float64, bounds []float64) int {
	n := 0
	for _, b := range bounds {
		if score >= b {
			n++
		}
	}
	return n
}

func metricsWithBounds(rows []evalRow, bounds []float64) calMetrics {
	m := calMetrics{N: len(rows)}
	if m.N == 0 {
		return m
	}
	var ha, hp [6]int
	for _, r := range rows {
		a := r.Actual
		p := levelFromBounds(r.Score, bounds)
		if a < 0 || a > 5 {
			continue
		}
		d := a - p
		if d < 0 {
			d = -d
		}
		if d == 0 {
			m.Exact++
		}
		if d <= 1 {
			m.Within1++
		}
		m.MAE += float64(d)
		m.MSE += float64(d * d)
		ha[a]++
		hp[p]++
	}
	m.MAE /= float64(m.N)
	m.MSE /= float64(m.N)
	expected := 0.0
	for i := 0; i < 6; i++ {
		for j := 0; j < 6; j++ {
			d := i - j
			expected += float64(ha[i]*hp[j]*d*d) / float64(m.N*m.N)
		}
	}
	if expected > 0 {
		m.QWK = 1 - m.MSE/expected
	}
	return m
}

func groupScores(rows []evalRow) []scoreGroup {
	byScore := make(map[float64]*scoreGroup)
	for _, r := range rows {
		g := byScore[r.Score]
		if g == nil {
			g = &scoreGroup{score: r.Score}
			byScore[r.Score] = g
		}
		a := r.Actual
		if a < 0 || a > 5 {
			continue
		}
		g.cnt++
		g.sum += float64(a)
		g.sumsq += float64(a * a)
		for k := 0; k < 6; k++ {
			d := a - k
			if d < 0 {
				d = -d
			}
			g.absCost[k] += float64(d)
		}
	}
	out := make([]scoreGroup, 0, len(byScore))
	for _, g := range byScore {
		if g.cnt > 0 {
			out = append(out, *g)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].score < out[j].score })
	return out
}

// optimizeBounds 用动态规划把有序分数切成至多 6 个连续段，
// 每段对应一个等级，objective 取 mse 或 mae。
func optimizeBounds(rows []evalRow, objective string) []float64 {
	groups := groupScores(rows)
	m := len(groups)
	bounds := []float64{math.Inf(1), math.Inf(1), math.Inf(1), math.Inf(1), math.Inf(1)}
	if m == 0 {
		return bounds
	}
	inf := math.Inf(1)
	dp := make([][]float64, 6)
	back := make([][]int, 6)
	for k := 0; k < 6; k++ {
		dp[k] = make([]float64, m+1)
		back[k] = make([]int, m+1)
		for j := range dp[k] {
			dp[k][j] = inf
			back[k][j] = -1
		}
	}
	dp[0][0] = 0
	for k := 0; k < 6; k++ {
		if k > 0 {
			for j := 0; j <= m; j++ {
				dp[k][j] = dp[k-1][j]
			}
		}
		for j := 1; j <= m; j++ {
			acc := 0.0
			best := inf
			bestI := -1
			for i := j; i >= 1; i-- {
				g := groups[i-1]
				var cost float64
				if objective == "mae" {
					cost = g.absCost[k]
				} else {
					kf := float64(k)
					cost = g.sumsq - 2*kf*g.sum + kf*kf*float64(g.cnt)
				}
				acc += cost
				prev := inf
				if k == 0 {
					if i-1 == 0 {
						prev = 0
					}
				} else {
					prev = dp[k-1][i-1]
				}
				if prev+acc < best {
					best = prev + acc
					bestI = i - 1
				}
			}
			if best < dp[k][j] {
				dp[k][j] = best
				back[k][j] = bestI
			}
		}
	}
	assign := make([]int, m)
	for i := range assign {
		assign[i] = -1
	}
	j := m
	for k := 5; k >= 0; k-- {
		i := back[k][j]
		if i < 0 {
			continue
		}
		for t := i; t < j; t++ {
			assign[t] = k
		}
		j = i
	}
	for k := 1; k <= 5; k++ {
		for i, a := range assign {
			if a >= k {
				bounds[k-1] = groups[i].score
				break
			}
		}
	}
	return bounds
}

func sigmoid(z float64) float64 {
	if z < -60 {
		return 0
	}
	if z > 60 {
		return 1
	}
	return 1 / (1 + math.Exp(-z))
}

func binaryFor(rows []evalRow, minLevel int) []bool {
	out := make([]bool, len(rows))
	for i, r := range rows {
		out[i] = r.Actual >= minLevel
	}
	return out
}

func componentValuesFor(row evalRow, key string) float64 {
	if v, ok := row.Components[key]; ok {
		return v
	}
	return 50
}

func componentAUC(rows []evalRow, key string, minLevel int) float64 {
	vals := make([]float64, len(rows))
	for i, r := range rows {
		vals[i] = componentValuesFor(r, key)
	}
	return auc(vals, binaryFor(rows, minLevel))
}

func monthFolds(rows []evalRow, k int) [][]int {
	months := make([]string, 0)
	seen := make(map[string]bool)
	for _, r := range rows {
		m := r.Date
		if len(m) > 7 {
			m = m[:7]
		}
		if !seen[m] {
			seen[m] = true
			months = append(months, m)
		}
	}
	sort.Strings(months)
	idx := make(map[string]int, len(months))
	for i, m := range months {
		idx[m] = i % k
	}
	folds := make([][]int, k)
	for i, r := range rows {
		m := r.Date
		if len(m) > 7 {
			m = m[:7]
		}
		folds[idx[m]] = append(folds[idx[m]], i)
	}
	return folds
}

func otherIndices(folds [][]int, hold int) []int {
	var out []int
	for f, idxs := range folds {
		if f == hold {
			continue
		}
		out = append(out, idxs...)
	}
	return out
}

func subsetRows(rows []evalRow, idxs []int) []evalRow {
	out := make([]evalRow, 0, len(idxs))
	for _, i := range idxs {
		out = append(out, rows[i])
	}
	return out
}

// fitLogisticVec 用带 L2 的梯度下降拟合二分类逻辑回归。
func fitLogisticVec(x [][]float64, y []float64, lambda, lr float64, iters int) (float64, []float64) {
	if len(x) == 0 {
		return 0, nil
	}
	d := len(x[0])
	w := make([]float64, d)
	b := 0.0
	n := float64(len(x))
	for it := 0; it < iters; it++ {
		gw := make([]float64, d)
		gb := 0.0
		for i, xi := range x {
			p := sigmoid(b + dot(w, xi))
			e := p - y[i]
			gb += e
			for j := range xi {
				gw[j] += e * xi[j]
			}
		}
		for j := range w {
			w[j] -= lr * (gw[j]/n + lambda*w[j]/n)
		}
		b -= lr * gb / n
	}
	return b, w
}

func dot(a, b []float64) float64 {
	s := 0.0
	for i := range a {
		s += a[i] * b[i]
	}
	return s
}

type compModel struct {
	b     float64
	w     []float64
	means []float64
	stds  []float64
}

func componentStats(rows []evalRow) ([]float64, []float64) {
	means := make([]float64, len(calKeys))
	stds := make([]float64, len(calKeys))
	for j, k := range calKeys {
		vals := make([]float64, len(rows))
		for i, r := range rows {
			vals[i] = componentValuesFor(r, k)
		}
		sum := 0.0
		for _, v := range vals {
			sum += v
		}
		mean := sum / float64(maxInt(len(vals), 1))
		means[j] = mean
		ss := 0.0
		for _, v := range vals {
			d := v - mean
			ss += d * d
		}
		sd := math.Sqrt(ss / float64(maxInt(len(vals), 1)))
		if sd < 1e-9 {
			sd = 1
		}
		stds[j] = sd
	}
	return means, stds
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func componentFeatures(rows []evalRow, means, stds []float64) [][]float64 {
	x := make([][]float64, len(rows))
	for i, r := range rows {
		x[i] = make([]float64, len(calKeys))
		for j, k := range calKeys {
			x[i][j] = (componentValuesFor(r, k) - means[j]) / stds[j]
		}
	}
	return x
}

func fitCompModel(rows []evalRow, minLevel int, lambda float64) compModel {
	means, stds := componentStats(rows)
	x := componentFeatures(rows, means, stds)
	y := make([]float64, len(rows))
	bin := binaryFor(rows, minLevel)
	for i, v := range bin {
		if v {
			y[i] = 1
		}
	}
	b, w := fitLogisticVec(x, y, lambda, 0.15, 1500)
	return compModel{b: b, w: w, means: means, stds: stds}
}

func (m compModel) predict(row evalRow) float64 {
	x := make([]float64, len(calKeys))
	for j, k := range calKeys {
		x[j] = (componentValuesFor(row, k) - m.means[j]) / m.stds[j]
	}
	return sigmoid(m.b + dot(m.w, x))
}

type probModel struct{ a, b float64 }

func fitProbModel(rows []evalRow) probModel {
	x := make([][]float64, len(rows))
	for i, r := range rows {
		x[i] = []float64{r.Score / 50}
	}
	y := make([]float64, len(rows))
	bin := binaryFor(rows, 1)
	for i, v := range bin {
		if v {
			y[i] = 1
		}
	}
	b, w := fitLogisticVec(x, y, 0, 0.1, 2000)
	// x = score/50，因此原尺度 a = b，原斜率 = w/50。
	return probModel{a: b, b: w[0] / 50}
}

func (m probModel) p(score float64) float64 {
	return sigmoid(m.a + m.b*score)
}

func logloss(pred []float64, bin []bool) float64 {
	if len(pred) == 0 {
		return 0
	}
	s := 0.0
	for i, p := range pred {
		y := 0.0
		if bin[i] {
			y = 1
		}
		s += y*math.Log(math.Max(p, 1e-12)) + (1-y)*math.Log(math.Max(1-p, 1e-12))
	}
	return -s / float64(len(pred))
}

func brier(pred []float64, bin []bool) float64 {
	if len(pred) == 0 {
		return 0
	}
	s := 0.0
	for i, p := range pred {
		y := 0.0
		if bin[i] {
			y = 1
		}
		d := p - y
		s += d * d
	}
	return s / float64(len(pred))
}

func formatBounds(bounds []float64) string {
	parts := make([]string, len(bounds))
	for i, b := range bounds {
		if math.IsInf(b, 1) {
			parts[i] = "∞"
		} else {
			parts[i] = fmt.Sprintf("%.1f", b)
		}
	}
	return strings.Join(parts, " / ")
}

func fmtPct(a, b int) string {
	if b == 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f%%", 100*float64(a)/float64(b))
}

func buildCalibrationReport(rows []evalRow, foldsN int, lambda float64) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# glow 标定实验报告\n\n")
	fmt.Fprintf(&b, "- 样本数：%d\n- 交叉验证：按月分 %d 折（月份序号取模）\n- 分项重加权 L2 系数：%g\n", len(rows), foldsN, lambda)
	b.WriteString("- 标签为 sunsetbot 同口径「真实质量」，拟合结果只代表与该标签的一致性，不代表独立真值验证。\n\n")

	baseBounds := []float64{20, 35, 50, 65, 80}
	mseBounds := optimizeBounds(rows, "mse")
	maeBounds := optimizeBounds(rows, "mae")
	folds := monthFolds(rows, foldsN)

	b.WriteString("## 一、六档阈值标定\n\n")
	b.WriteString("| 方案 | 阈值 | 精确一致 | ±1 档 | MAE | MSE | QWK |\n|---|---|---:|---:|---:|---:|---:|\n")
	for _, c := range []struct {
		name   string
		bounds []float64
	}{
		{"现状", baseBounds},
		{"MSE 最优（全量）", mseBounds},
		{"MAE 最优（全量）", maeBounds},
	} {
		m := metricsWithBounds(rows, c.bounds)
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %.3f | %.3f | %.3f |\n",
			c.name, formatBounds(c.bounds), fmtPct(m.Exact, m.N), fmtPct(m.Within1, m.N), m.MAE, m.MSE, m.QWK)
	}
	b.WriteString("\n训练折求阈值、测试折评估：\n\n")
	b.WriteString("| 折 | 样本 | MSE 阈值 | MAE 阈值 | MSE 方案 MAE/QWK | MAE 方案 MAE/QWK | 基线 MAE/QWK |\n|---|---:|---|---|---:|---:|---:|\n")
	var sumMaeMSE, sumQwkMSE, sumMaeMAE, sumQwkMAE, sumMaeBase, sumQwkBase float64
	for f, idxs := range folds {
		tr := subsetRows(rows, otherIndices(folds, f))
		te := subsetRows(rows, idxs)
		bm := optimizeBounds(tr, "mse")
		am := optimizeBounds(tr, "mae")
		mm := metricsWithBounds(te, bm)
		ma := metricsWithBounds(te, am)
		mb := metricsWithBounds(te, baseBounds)
		sumMaeMSE += mm.MAE
		sumQwkMSE += mm.QWK
		sumMaeMAE += ma.MAE
		sumQwkMAE += ma.QWK
		sumMaeBase += mb.MAE
		sumQwkBase += mb.QWK
		fmt.Fprintf(&b, "| %d | %d | %s | %s | %.3f / %.3f | %.3f / %.3f | %.3f / %.3f |\n",
			f, len(te), formatBounds(bm), formatBounds(am), mm.MAE, mm.QWK, ma.MAE, ma.QWK, mb.MAE, mb.QWK)
	}
	n := float64(maxInt(len(folds), 1))
	fmt.Fprintf(&b, "| 平均 | %d |  |  | %.3f / %.3f | %.3f / %.3f | %.3f / %.3f |\n\n",
		len(rows), sumMaeMSE/n, sumQwkMSE/n, sumMaeMAE/n, sumQwkMAE/n, sumMaeBase/n, sumQwkBase/n)

	b.WriteString("## 二、概率标定（目标：任意烧，即实际≥微烧）\n\n")
	current := probModel{a: -0.0375 * 77.9, b: 0.0375}
	fitted := fitProbModel(rows)
	binAll := binaryFor(rows, 1)
	predCur := make([]float64, len(rows))
	predFit := make([]float64, len(rows))
	for i, r := range rows {
		predCur[i] = current.p(r.Score)
		predFit[i] = fitted.p(r.Score)
	}
	b.WriteString("| 方案 | a | b | logloss | Brier |\n|---|---:|---:|---:|---:|\n")
	fmt.Fprintf(&b, "| 当前 | %.4f | %.5f | %.4f | %.4f |\n", current.a, current.b, logloss(predCur, binAll), brier(predCur, binAll))
	fmt.Fprintf(&b, "| 全量重拟合 | %.4f | %.5f | %.4f | %.4f |\n\n", fitted.a, fitted.b, logloss(predFit, binAll), brier(predFit, binAll))
	b.WriteString("训练折拟合、测试折评估：\n\n")
	b.WriteString("| 折 | 样本 | 拟合 logloss | 当前 logloss | 拟合 Brier | 当前 Brier |\n|---|---:|---:|---:|---:|---:|\n")
	var sllFit, sllCur, sbrFit, sbrCur float64
	for f, idxs := range folds {
		tr := subsetRows(rows, otherIndices(folds, f))
		te := subsetRows(rows, idxs)
		m := fitProbModel(tr)
		pc := make([]float64, len(te))
		pf := make([]float64, len(te))
		for i, r := range te {
			pc[i] = current.p(r.Score)
			pf[i] = m.p(r.Score)
		}
		bt := binaryFor(te, 1)
		llf, llc := logloss(pf, bt), logloss(pc, bt)
		brf, brc := brier(pf, bt), brier(pc, bt)
		sllFit += llf
		sllCur += llc
		sbrFit += brf
		sbrCur += brc
		fmt.Fprintf(&b, "| %d | %d | %.4f | %.4f | %.4f | %.4f |\n", f, len(te), llf, llc, brf, brc)
	}
	fmt.Fprintf(&b, "| 平均 | %d | %.4f | %.4f | %.4f | %.4f |\n\n", len(rows), sllFit/n, sllCur/n, sbrFit/n, sbrCur/n)

	b.WriteString("## 三、分项重新加权\n\n")
	b.WriteString("### 3.1 各分项排序能力（AUC）\n\n")
	b.WriteString("| 分项 | ≥微烧 | ≥小到中烧 | ≥中到大烧 | ≥大烧 |\n|---|---:|---:|---:|---:|\n")
	for _, k := range calKeys {
		fmt.Fprintf(&b, "| %s | %.3f | %.3f | %.3f | %.3f |\n", k,
			componentAUC(rows, k, 1), componentAUC(rows, k, 3), componentAUC(rows, k, 4), componentAUC(rows, k, 5))
	}
	scoreVals := make([]float64, len(rows))
	for i, r := range rows {
		scoreVals[i] = r.Score
	}
	fmt.Fprintf(&b, "| 当前总分 | %.3f | %.3f | %.3f | %.3f |\n\n",
		auc(scoreVals, binaryFor(rows, 1)), auc(scoreVals, binaryFor(rows, 3)),
		auc(scoreVals, binaryFor(rows, 4)), auc(scoreVals, binaryFor(rows, 5)))

	b.WriteString("### 3.2 L2 逻辑回归交叉验证 AUC\n\n")
	b.WriteString("| 目标 | 当前分数 | 重加权 | 差值 |\n|---|---:|---:|---:|\n")
	for _, t := range calTargets {
		bin := binaryFor(rows, t.min)
		sBase := auc(scoreVals, bin)
		var sLR float64
		for f, idxs := range folds {
			tr := subsetRows(rows, otherIndices(folds, f))
			te := subsetRows(rows, idxs)
			m := fitCompModel(tr, t.min, lambda)
			pred := make([]float64, len(te))
			for i, r := range te {
				pred[i] = m.predict(r)
			}
			// 每折等权平均，避免折大小差异影响。
			sLR += auc(pred, binaryFor(te, t.min)) / float64(maxInt(len(folds), 1))
		}
		fmt.Fprintf(&b, "| %s | %.3f | %.3f | %+.3f |\n", t.name, sBase, sLR, sLR-sBase)
	}
	b.WriteString("\n逐折对比（≥3 档、≥5 档）：\n\n")
	b.WriteString("| 折 | ≥3 重加权 | ≥3 当前 | ≥5 重加权 | ≥5 当前 |\n|---|---:|---:|---:|---:|\n")
	for f, idxs := range folds {
		tr := subsetRows(rows, otherIndices(folds, f))
		te := subsetRows(rows, idxs)
		m3 := fitCompModel(tr, 3, lambda)
		m5 := fitCompModel(tr, 5, lambda)
		p3, p5 := make([]float64, len(te)), make([]float64, len(te))
		s3, s5 := make([]float64, len(te)), make([]float64, len(te))
		for i, r := range te {
			p3[i] = m3.predict(r)
			p5[i] = m5.predict(r)
			s3[i] = r.Score
			s5[i] = r.Score
		}
		fmt.Fprintf(&b, "| %d | %.3f | %.3f | %.3f | %.3f |\n", f,
			auc(p3, binaryFor(te, 3)), auc(s3, binaryFor(te, 3)),
			auc(p5, binaryFor(te, 5)), auc(s5, binaryFor(te, 5)))
	}
	b.WriteString("\n### 3.3 全量拟合权重（标准化空间）\n\n")
	for _, t := range []struct {
		name string
		min  int
	}{{"≥小到中烧", 3}, {"≥大烧", 5}} {
		m := fitCompModel(rows, t.min, lambda)
		type kv struct {
			k string
			v float64
		}
		items := make([]kv, len(calKeys))
		for i, k := range calKeys {
			items[i] = kv{k, m.w[i]}
		}
		sort.Slice(items, func(i, j int) bool { return math.Abs(items[i].v) > math.Abs(items[j].v) })
		parts := make([]string, len(items))
		for i, it := range items {
			parts[i] = fmt.Sprintf("%s %+.3f", it.k, it.v)
		}
		fmt.Fprintf(&b, "- %s：截距 %+.3f；%s\n", t.name, m.b, strings.Join(parts, "，"))
	}
	b.WriteString("\n## 四、口径与结论\n\n")
	b.WriteString("- 阈值优化以「同分段必须同档」为约束，用动态规划求最优，未使用独立真值。\n")
	b.WriteString("- 分项重加权使用训练折统计量标准化，缺失分项按 50（中性）填充。\n")
	b.WriteString("- 若阈值优化把六档压成两三档，说明当前分数对高档位的排序不足，而不是边界位置不合适。\n")
	b.WriteString("- 本报告只输出候选参数与指标，不修改 internal/glow、线上取数、存储与推送链路。\n")
	return b.String()
}
