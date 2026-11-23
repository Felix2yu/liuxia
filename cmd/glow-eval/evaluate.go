package main

import (
	"fmt"
	"sort"
	"strings"

	"liuxia/internal/glow"
)

func componentValues(c glow.Components) map[string]float64 {
	pairs := map[string]glow.Component{
		"curtain": c.Curtain, "curtainV2": c.CurtainV2,
		"low": c.Low, "gap": c.Gap, "gapV2": c.GapV2, "gapV3": c.GapV3,
		"precip": c.Precip, "precipV2": c.PrecipV2,
		"washed": c.Washed, "washedV2": c.WashedV2,
		"aod": c.AOD, "vis": c.Vis, "pm": c.PM,
		"dew": c.Dew, "wind": c.Wind, "cape": c.Cape,
	}
	out := make(map[string]float64, len(pairs))
	for k, v := range pairs {
		if v.Score != nil {
			out[k] = *v.Score
		}
	}
	return out
}

func toEpochs(times []string, offsetSec int) []int64 {
	out := make([]int64, 0, len(times))
	for _, t := range times {
		e, err := glow.ToEpoch(t, offsetSec)
		if err != nil {
			continue
		}
		out = append(out, e)
	}
	return out
}

func dailyIndex(d daily, date string) int {
	for i, t := range d.Time {
		if t == date {
			return i
		}
	}
	return -1
}

func floatAt(arr []*float64, i int) *float64 {
	if i < 0 || i >= len(arr) {
		return nil
	}
	return arr[i]
}

func floatOrZero(arr []*float64, i int) float64 {
	v := floatAt(arr, i)
	if v == nil {
		return 0
	}
	return *v
}

// evaluateEvent 用与线上 fetchGlowData 相同的字段映射重建输入并评分。
func evaluateEvent(ev *event, data bundle) (evalRow, error) {
	if len(data.Locations) == 0 {
		return evalRow{}, fmt.Errorf("没有历史气象响应")
	}
	center := data.Locations[0]
	di := dailyIndex(center.Daily, ev.Label.Date)
	if di < 0 {
		return evalRow{}, fmt.Errorf("缺少日出日落数据")
	}
	eventTime := center.Daily.Sunset[di]
	if ev.Morning && di < len(center.Daily.Sunrise) {
		eventTime = center.Daily.Sunrise[di]
	}
	eventEpoch, err := glow.ToEpoch(eventTime, center.UtcOffsetSeconds)
	if err != nil {
		return evalRow{}, err
	}

	centerEpochs := toEpochs(center.Hourly.Time, center.UtcOffsetSeconds)
	ci := glow.IdxFloor(centerEpochs, eventEpoch)
	if ci < 0 || ci+1 >= len(centerEpochs) {
		return evalRow{}, fmt.Errorf("中心逐小时数据未覆盖事件时刻")
	}
	wi := glow.IdxNearest(centerEpochs, eventEpoch)

	upstream := make([]glow.UpstreamPoint, 0, len(upstreamKM))
	for i := 1; i < len(data.Locations) && i-1 < len(upstreamKM); i++ {
		loc := data.Locations[i]
		epochs := toEpochs(loc.Hourly.Time, loc.UtcOffsetSeconds)
		if len(epochs) == 0 {
			continue
		}
		j := glow.IdxNearest(epochs, eventEpoch)
		upstream = append(upstream, glow.UpstreamPoint{
			DistanceKM: upstreamKM[i-1],
			Low:        floatAt(loc.Hourly.CloudCoverLow, j),
			Mid:        floatAt(loc.Hourly.CloudCoverMid, j),
			Total:      floatAt(loc.Hourly.CloudCover, j),
		})
	}

	aqIndex := -1
	if len(data.Air.Hourly.Time) > 0 {
		aqEpochs := toEpochs(data.Air.Hourly.Time, data.Air.UtcOffsetSeconds)
		if len(aqEpochs) > 0 {
			aqIndex = glow.IdxNearest(aqEpochs, eventEpoch)
		}
	}

	in := glow.Input{
		Mid:       floatAt(center.Hourly.CloudCoverMid, ci),
		High:      floatAt(center.Hourly.CloudCoverHigh, ci),
		Low:       floatAt(center.Hourly.CloudCoverLow, ci),
		Precip:    floatAt(center.Hourly.Precipitation, wi),
		Prob:      floatAt(center.Hourly.PrecipitationProbability, wi),
		PrecipV2:  floatAt(center.Hourly.Precipitation, wi+1),
		ProbV2:    floatAt(center.Hourly.PrecipitationProbability, wi+1),
		Pre3:      floatOrZero(center.Hourly.Precipitation, wi-1) + floatOrZero(center.Hourly.Precipitation, wi-2) + floatOrZero(center.Hourly.Precipitation, wi-3),
		Pre3V2:    floatOrZero(center.Hourly.Precipitation, wi) + floatOrZero(center.Hourly.Precipitation, wi-1) + floatOrZero(center.Hourly.Precipitation, wi-2),
		VisM:      floatAt(center.Hourly.Visibility, wi),
		Temp:      floatAt(center.Hourly.Temperature2m, wi),
		DewPoint:  floatAt(center.Hourly.DewPoint2m, wi),
		WindSpeed: floatAt(center.Hourly.WindSpeed10m, wi),
		WindDir:   floatAt(center.Hourly.WindDirection10m, wi),
		Cape:      floatAt(center.Hourly.Cape, wi),
		AOD:       floatAt(data.Air.Hourly.AOD, aqIndex),
		PM25:      floatAt(data.Air.Hourly.PM25, aqIndex),
		Upstream:  upstream,
	}
	res := glow.Evaluate(in)

	missing := []string{}
	if in.Mid == nil || in.High == nil || in.Low == nil {
		missing = append(missing, "中心云量")
	}
	if len(upstream) == 0 {
		missing = append(missing, "上游云量")
	}
	if in.VisM == nil {
		missing = append(missing, "能见度")
	}
	if in.Cape == nil {
		missing = append(missing, "CAPE")
	}
	if in.Prob == nil {
		missing = append(missing, "降水概率")
	}
	if in.AOD == nil {
		missing = append(missing, "AOD")
	}

	components := componentValues(res.Components)

	return evalRow{
		Date:       ev.Label.Date,
		Event:      ev.Label.Event,
		EventTime:  eventTime[11:],
		Actual:     labelLevel[ev.Label.Actual],
		Predicted:  res.Level,
		Sunset:     labelLevel[ev.Label.Predicted],
		Score:      res.RawScore,
		Prob:       res.Probability,
		Missing:    missing,
		Components: components,
	}, nil
}

type metrics struct {
	N        int
	Exact    int
	Within1  int
	MAE      float64
	AUCScore []float64
}

func confusionMatrix(rows []evalRow) [6][6]int {
	var m [6][6]int
	for _, r := range rows {
		if r.Actual >= 0 && r.Actual < 6 && r.Predicted >= 0 && r.Predicted < 6 {
			m[r.Actual][r.Predicted]++
		}
	}
	return m
}

func basicMetrics(rows []evalRow) metrics {
	m := metrics{N: len(rows)}
	if m.N == 0 {
		return m
	}
	for _, r := range rows {
		d := r.Actual - r.Predicted
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
	}
	m.MAE /= float64(m.N)
	return m
}

// auc 用平均秩处理并列分数，等价于 Mann-Whitney U 统计量。
func auc(values []float64, binary []bool) float64 {
	var pos, neg []float64
	for i, v := range values {
		if binary[i] {
			pos = append(pos, v)
		} else {
			neg = append(neg, v)
		}
	}
	if len(pos) == 0 || len(neg) == 0 {
		return 0
	}
	all := append(append([]float64{}, pos...), neg...)
	sort.Float64s(all)
	// 正例平均秩：并列值取秩区间中点，等价于 Mann-Whitney U 的并列修正。
	sort.Float64s(pos)
	posRank := 0.0
	for _, v := range pos {
		lo := sort.SearchFloat64s(all, v)
		hi := sort.Search(len(all), func(i int) bool { return all[i] > v })
		posRank += float64(lo+hi+1) / 2
	}
	return (posRank - float64(len(pos)*(len(pos)+1))/2) / float64(len(pos)*len(neg))
}

func binaryRows(rows []evalRow, minLevel int) []bool {
	out := make([]bool, len(rows))
	for i, r := range rows {
		out[i] = r.Actual >= minLevel
	}
	return out
}

func buildReport(labels []label, rows []evalRow) string {
	var b strings.Builder
	m := basicMetrics(rows)
	cm := confusionMatrix(rows)

	fmt.Fprintf(&b, "# glow 离线评估报告\n\n")
	fmt.Fprintf(&b, "- 标注数：%d；成功评分：%d\n", len(labels), len(rows))
	fmt.Fprintf(&b, "- 数据源：sunsetbot /reanalysis/（上海，%s～%s），glow 评分输入来自 Open-Meteo Historical Forecast / Air Quality\n\n", firstDate, lastDate)

	missingCounts := map[string]int{}
	for _, r := range rows {
		for _, k := range r.Missing {
			missingCounts[k]++
		}
	}
	b.WriteString("## 输入完整度\n\n")
	for _, k := range []string{"中心云量", "上游云量", "能见度", "CAPE", "降水概率", "AOD"} {
		fmt.Fprintf(&b, "- %s缺失：%d/%d\n", k, missingCounts[k], len(rows))
	}
	b.WriteString("\n")

	b.WriteString("## 实际等级分布\n\n")
	actualCounts := make([]int, 6)
	sunsetCounts := make([]int, 6)
	for _, r := range rows {
		actualCounts[r.Actual]++
		sunsetCounts[r.Sunset]++
	}
	b.WriteString("| 等级 | 实际 | sunsetbot 预测 | glow 预测 |\n|---|---:|---:|---:|\n")
	for i, name := range levelName {
		n := 0
		for _, r := range rows {
			if r.Predicted == i {
				n++
			}
		}
		fmt.Fprintf(&b, "| %s | %d | %d | %d |\n", name, actualCounts[i], sunsetCounts[i], n)
	}
	b.WriteString("\n")

	b.WriteString("## 六档混淆矩阵（行=实际，列=glow 预测）\n\n")
	b.WriteString("| 实际\\预测 | " + strings.Join(levelName, " | ") + " |\n")
	b.WriteString("|---|" + strings.Repeat("---:|", 6) + "\n")
	for i, name := range levelName {
		cells := make([]string, 6)
		for j := 0; j < 6; j++ {
			cells[j] = fmt.Sprintf("%d", cm[i][j])
		}
		fmt.Fprintf(&b, "| %s | %s |\n", name, strings.Join(cells, " | "))
	}
	b.WriteString("\n")

	fmt.Fprintf(&b, "## 总体指标\n\n- 精确一致：%d/%d（%.1f%%）\n- ±1 档内：%d/%d（%.1f%%）\n- 平均绝对档差：%.3f\n", m.Exact, m.N, pct(m.Exact, m.N), m.Within1, m.N, pct(m.Within1, m.N), m.MAE)

	sunsetRows := make([]evalRow, len(rows))
	copy(sunsetRows, rows)
	for i := range sunsetRows {
		sunsetRows[i].Predicted = sunsetRows[i].Sunset
	}
	sm := basicMetrics(sunsetRows)
	fmt.Fprintf(&b, "- sunsetbot 自身同口径：精确一致 %d/%d（%.1f%%），±1 档 %.1f%%，平均绝对档差 %.3f\n\n", sm.Exact, sm.N, pct(sm.Exact, sm.N), pct(sm.Within1, sm.N), sm.MAE)

	scores := make([]float64, len(rows))
	predLevels := make([]float64, len(rows))
	sunsetLevels := make([]float64, len(rows))
	for i, r := range rows {
		scores[i] = r.Score
		predLevels[i] = float64(r.Predicted)
		sunsetLevels[i] = float64(r.Sunset)
	}
	for _, th := range []struct {
		name string
		lv   int
	}{{"≥小到中烧", 3}, {"≥中到大烧", 4}, {"≥大烧", 5}} {
		bin := binaryRows(rows, th.lv)
		pos := 0
		for _, v := range bin {
			if v {
				pos++
			}
		}
		fmt.Fprintf(&b, "- AUC %s（正例 %d）：glow 分数 %.3f，glow 档位 %.3f，sunsetbot 档位 %.3f\n", th.name, pos, auc(scores, bin), auc(predLevels, bin), auc(sunsetLevels, bin))
	}
	b.WriteString("\n")

	b.WriteString("## glow 等级对应的实际观测率\n\n")
	b.WriteString("| glow 预测 | 场次 | 实际≥小到中烧 | 实际≥中到大烧 | 实际大烧 | 平均输出概率 |\n|---|---:|---:|---:|---:|---:|\n")
	for i, name := range levelName {
		var group []evalRow
		for _, r := range rows {
			if r.Predicted == i {
				group = append(group, r)
			}
		}
		if len(group) == 0 {
			continue
		}
		mid, high, big := 0, 0, 0
		prob := 0.0
		for _, r := range group {
			if r.Actual >= 3 {
				mid++
			}
			if r.Actual >= 4 {
				high++
			}
			if r.Actual >= 5 {
				big++
			}
			prob += r.Prob
		}
		fmt.Fprintf(&b, "| %s | %d | %.1f%% | %.1f%% | %.1f%% | %.1f%% |\n", name, len(group), pct(mid, len(group)), pct(high, len(group)), pct(big, len(group)), 100*prob/float64(len(group)))
	}
	b.WriteString("\n")

	b.WriteString("## 口径说明\n\n")
	b.WriteString("- 历史天气来自 Historical Forecast API（GFS 再预报），不是当时的实时 GFS 预报； precipitation_probability 在该接口无历史值，按缺数据处理。\n")
	b.WriteString("- 上游点按周代表日方位角取 50/100/200/300/400 km，非逐日方位角；AOD/PM2.5 自 2022-08 中旬起才有值。\n")
	b.WriteString("- 标签为 sunsetbot 站点提供的「真实质量」，标注协议未公开；本报告验证的是与该标签的一致性，不等同于独立真值验证。\n")
	return b.String()
}

func pct(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return 100 * float64(a) / float64(b)
}
