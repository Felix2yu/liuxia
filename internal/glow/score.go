// Package glow 是「霞 · 全球火烧云预报」algo.js 的 Go 纯函数移植。
//
// 本包只依赖标准库：输入是已经对齐到事件时刻的观测/预报数值，输出是评分、
// 等级、概率、通透度与可解释分项。网络取数、时间对齐、单位换算均不在本包内。
// 原算法 MIT 许可，仓库：github.com/baiiiii521/sunset-glow-forecast。
package glow

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"
)

const (
	earthRadiusKM   = 6371.0
	rayleighTau550  = 0.098
	aerosolAngstrom = 1.3
	rayleighScaleH  = 7.5
	aerosolScaleH   = 1.4
	aerosolAirmass  = 12.0
	glowElevDeg     = -1.0
	lightPathKM     = 450.0
	decorrelationKM = 200.0
	lowCloudTopKM   = 3.5
	midCloudBaseKM  = 3.0
	midCloudTopKM   = 6.5
)

// 云底高度（km）。高云光路更干净，中云次之。
const (
	DeckHigh = 8.6
	DeckMid  = 5.2
	DeckLow  = 1.2
)

// Flags 对应原算法 v2 口径开关。gapV3 打开时使用光路几何缺口；
// windowAlign 由取数侧负责，本包提供 BlendCloudAt 供其使用。
var Flags = struct {
	GapV3       bool
	WindowAlign bool
}{GapV3: true, WindowAlign: true}

// Levels 是六级分档，min 为最低分数（含）。
var Levels = []struct {
	Name string
	Min  int
	Tip  string
}{
	{"无烧", 0, "云况不足，基本看不到"},
	{"微烧", 20, "云边可能泛一点淡色"},
	{"小烧", 35, "有机会出现小片霞色"},
	{"小到中烧", 50, "出现火烧云的机会较大"},
	{"中到大烧", 65, "大概率烧起来，值得去看"},
	{"大烧", 80, "条件极好，可能非常壮观"},
}

var levelBounds = []float64{20, 35, 50, 65, 80}

// Clamp 将 x 限制在 [lo, hi]。
func Clamp(x, lo, hi float64) float64 {
	return math.Max(lo, math.Min(hi, x))
}

// Lerp 在 x0 处取 y0、x1 处取 y1，区间外截断。
func Lerp(x, x0, x1, y0, y1 float64) float64 {
	if x <= x0 {
		return y0
	}
	if x >= x1 {
		return y1
	}
	return y0 + (y1-y0)*(x-x0)/(x1-x0)
}

// PyRound 实现 Python 的银行家舍入（half-even），与 Go 的 math.Round 不同。
func PyRound(x float64) float64 {
	f := math.Floor(x)
	d := x - f
	if d > 0.5 {
		return f + 1
	}
	if d < 0.5 {
		return f
	}
	if math.Mod(f, 2) == 0 {
		return f
	}
	return f + 1
}

// Fmt0 对应 Python 的 f"{x:.0f}"。
func Fmt0(x float64) string {
	return strconv.FormatInt(int64(PyRound(x)), 10)
}

func fmtFixed(x float64, digits int) string {
	p := math.Pow(10, float64(digits))
	return strconv.FormatFloat(PyRound(x*p)/p, 'f', digits, 64)
}

func val(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

func ptr(v float64) *float64 { return &v }

func sum(vs []float64) float64 {
	var s float64
	for _, v := range vs {
		s += v
	}
	return s
}

// Destination 返回从 (lat, lon) 沿 bearingDeg 走 distKm 后的经纬度。
func Destination(lat, lon, bearingDeg, distKm float64) (float64, float64) {
	d := distKm / earthRadiusKM
	br := bearingDeg * math.Pi / 180
	lat1 := lat * math.Pi / 180
	lon1 := lon * math.Pi / 180
	lat2 := math.Asin(math.Sin(lat1)*math.Cos(d) + math.Cos(lat1)*math.Sin(d)*math.Cos(br))
	lon2 := lon1 + math.Atan2(math.Sin(br)*math.Sin(d)*math.Cos(lat1), math.Cos(d)-math.Sin(lat1)*math.Sin(lat2))
	return lat2 * 180 / math.Pi, lon2 * 180 / math.Pi
}

// SolarDeclination 返回太阳赤纬（弧度）。
func SolarDeclination(dayOfYear int) float64 {
	return -23.44 * math.Pi / 180 * math.Cos(2*math.Pi*(float64(dayOfYear)+10)/365.25)
}

func sunsetSunriseCosine(lat float64, dayOfYear int) float64 {
	phi := lat * math.Pi / 180
	return Clamp(math.Sin(SolarDeclination(dayOfYear))/math.Cos(phi), -1, 1)
}

// SunsetBearing 返回日落方位角（度，0–360）。
func SunsetBearing(lat float64, dayOfYear int) float64 {
	return 360 - math.Acos(sunsetSunriseCosine(lat, dayOfYear))*180/math.Pi
}

// SunriseBearing 返回日出方位角（度，0–360）。
func SunriseBearing(lat float64, dayOfYear int) float64 {
	return math.Acos(sunsetSunriseCosine(lat, dayOfYear)) * 180 / math.Pi
}

// ToEpoch 把某时区的墙上时间 "YYYY-MM-DDTHH:MM" 换算成 Unix 毫秒。
func ToEpoch(timeStr string, offsetSec int) (int64, error) {
	t, err := time.Parse("2006-01-02T15:04:05Z", timeStr+":00Z")
	if err != nil {
		return 0, err
	}
	return t.UnixMilli() - int64(offsetSec)*1000, nil
}

// IdxFloor 返回 times 中 <= target 的最后一个下标（times 需已升序）。
func IdxFloor(times []int64, target int64) int {
	best := 0
	for i, t := range times {
		if t <= target {
			best = i
		} else {
			break
		}
	}
	return best
}

// IdxNearest 返回离 target 最近的下标。
func IdxNearest(times []int64, target int64) int {
	best, bestDiff := 0, math.Inf(1)
	for i, t := range times {
		d := math.Abs(float64(t - target))
		if d < bestDiff {
			best, bestDiff = i, d
		}
	}
	return best
}

// HourlyCloud 是逐小时云量序列，nil 元素代表缺测。
type HourlyCloud struct {
	Time       []string
	CloudCover []*float64
	Low        []*float64
	Mid        []*float64
	High       []*float64
}

// BlendCloudAt 把整点云量在 epoch 处做线性插值，返回浅修改后的副本。
// 某一格缺测时取另一格，两格都缺测保持缺测。
func BlendCloudAt(h HourlyCloud, epochMS int64, offsetSec int) HourlyCloud {
	if len(h.Time) == 0 {
		return h
	}
	times := make([]int64, len(h.Time))
	for i, t := range h.Time {
		e, err := ToEpoch(t, offsetSec)
		if err != nil {
			return h
		}
		times[i] = e
	}
	i := IdxFloor(times, epochMS)
	if i < 0 || i+1 >= len(times) {
		return h
	}
	frac := Clamp(float64(epochMS-times[i])/3600000, 0, 1)
	if frac <= 0 {
		return h
	}
	out := h
	blend := func(arr []*float64) []*float64 {
		if len(arr) < i+2 {
			return arr
		}
		copyArr := append([]*float64(nil), arr...)
		a, b := arr[i], arr[i+1]
		var m *float64
		switch {
		case a == nil && b == nil:
			m = nil
		case a == nil:
			m = ptr(*b)
		case b == nil:
			m = ptr(*a)
		default:
			m = ptr((1-frac)*(*a) + frac*(*b))
		}
		copyArr[i] = m
		return copyArr
	}
	out.CloudCover = blend(h.CloudCover)
	out.Low = blend(h.Low)
	out.Mid = blend(h.Mid)
	out.High = blend(h.High)
	return out
}

// IlluminationLimit 返回云底受光的太阳高度下限（负值，度）。
func IlluminationLimit(baseKM float64) float64 {
	return -math.Sqrt(2*baseKM/earthRadiusKM) * 180 / math.Pi
}

func airmass(sunElevDeg, baseKM float64) (float64, float64) {
	tanRay := 38 * math.Exp(-baseKM/rayleighScaleH)
	tanAer := aerosolAirmass * math.Exp(-baseKM/aerosolScaleH)
	if sunElevDeg >= 0 {
		s := math.Max(math.Sin(sunElevDeg*math.Pi/180), 0.005)
		return math.Min(tanRay, 1/s), math.Min(tanAer, 0.75/s)
	}
	alpha := -sunElevDeg * math.Pi / 180
	ht := math.Max(0, baseKM-earthRadiusKM*alpha*alpha/2)
	return 38 * math.Exp(-ht/rayleighScaleH), aerosolAirmass * math.Exp(-ht/aerosolScaleH)
}

// DeckLum 返回云底相对太阳直射的亮度（0–1），不积累 RGB。
// sunElev 省略时取火烧云视觉峰值 -1°。
func DeckLum(baseKM float64, aod *float64, sunElev ...float64) float64 {
	elev := glowElevDeg
	if len(sunElev) > 0 {
		elev = sunElev[0]
	}
	mRay, mAer := airmass(elev, baseKM)
	a := 0.15
	if aod != nil {
		a = math.Max(0, *aod)
	}
	gy := func(l, mu, s1, s2 float64) float64 {
		s := s1
		if l >= mu {
			s = s2
		}
		return math.Exp(-0.5 * math.Pow((l-mu)/s, 2))
	}
	ybar := func(l float64) float64 {
		return 0.821*gy(l, 568.8, 46.9, 40.5) + 0.286*gy(l, 530.9, 16.3, 31.1)
	}
	const (
		h    = 6.626e-34
		c    = 3e8
		k    = 1.381e-23
		tSun = 5778.0
	)
	Y, Yw := 0.0, 0.0
	for l := 400.0; l <= 700; l += 5 {
		lm := l * 1e-9
		planck := (2 * h * c * c / math.Pow(lm, 5)) / (math.Exp(h*c/(lm*k*tSun)) - 1)
		tau := rayleighTau550*math.Pow(550/l, 4.1)*mRay + a*math.Pow(550/l, aerosolAngstrom)*mAer
		spec := planck * math.Exp(-tau)
		yb := ybar(l)
		Y += spec * yb
		Yw += planck * yb
	}
	return Y / math.Max(Yw, 1e-9)
}

// ScoreToProb 把评分转成经验概率；参数锚定 Skyglow 在上海 1109 条标签上的拟合档位。
func ScoreToProb(score float64) float64 {
	z := 0.0375 * (score - 77.9)
	if z < -60 {
		return 0
	}
	if z > 60 {
		return 1
	}
	return 1 / (1 + math.Exp(-z))
}

// Component 是一个分项评分与可解释文本。Score 为 nil 表示缺数据。
type Component struct {
	Score *float64
	Text  string
}

func unknown(text string) Component { return Component{Text: text} }
func scored(s float64, text string) Component {
	return Component{Score: ptr(s), Text: text}
}

// CCurtain 评中高云幕布。mid/high 为 0–100 的云量百分比。
func CCurtain(mid, high float64) Component {
	c := Clamp(mid+high, 0, 100)
	switch {
	case c <= 0:
		return scored(0, "无中高云幕布")
	case c <= 30:
		return scored(70*c/30, fmt.Sprintf("中高云偏薄（%s%%）", Fmt0(c)))
	case c <= 70:
		return scored(70+30*(c-30)/40, fmt.Sprintf("中高云幕布充足（%s%%）", Fmt0(c)))
	case c <= 90:
		return scored(100-30*(c-70)/20, fmt.Sprintf("中高云较厚（%s%%）", Fmt0(c)))
	default:
		return scored(70-40*(c-90)/10, fmt.Sprintf("中高云近全覆盖（%s%%）", Fmt0(c)))
	}
}

// CCurtainV2 用随机重叠合成中高云覆盖，避免简单相加重复计算重叠部分。
func CCurtainV2(mid, high float64) Component {
	m := Clamp(mid, 0, 100)
	hh := Clamp(high, 0, 100)
	return CCurtain(0, 100*(1-(1-m/100)*(1-hh/100)))
}

// CurtainLight 返回幕布受光亮度乘子，范围 0.6–1.0。
func CurtainLight(high, mid float64, aod *float64) float64 {
	hi, mi := Clamp(high, 0, 100), Clamp(mid, 0, 100)
	if hi+mi <= 0 {
		return 1
	}
	lumHigh := DeckLum(DeckHigh, aod)
	lumMid := DeckLum(DeckMid, aod)
	light := (hi*lumHigh + mi*lumMid) / math.Max(hi+mi, 1)
	return Clamp(light/math.Max(lumHigh, 1e-9), 0.6, 1)
}

// CLow 评本地低云遮挡。
func CLow(low float64) Component {
	switch {
	case low <= 20:
		return scored(100, "低云少")
	case low <= 60:
		return scored(Lerp(low, 20, 60, 100, 30), fmt.Sprintf("低云 %s%%", Fmt0(low)))
	default:
		return scored(Lerp(low, 60, 100, 30, 0), fmt.Sprintf("低云厚 %s%%", Fmt0(low)))
	}
}

// CGap 评 v1 上游晴空缺口，使用总云量。
func CGap(upLowAvg, upTotalAvg *float64) Component {
	if upTotalAvg == nil {
		return unknown("无上游云量数据")
	}
	var s float64
	switch {
	case *upTotalAvg <= 30:
		s = Clamp(100-*upTotalAvg*1.2, 0, 100)
	case *upTotalAvg <= 70:
		s = Lerp(*upTotalAvg, 30, 70, 100, 40)
	default:
		s = Lerp(*upTotalAvg, 70, 100, 40, 0)
	}
	if val(upLowAvg) > 50 {
		s = math.Max(0, s-20)
	}
	if s >= 80 {
		return scored(s, "太阳方向晴空缺口大")
	}
	if s >= 50 {
		return scored(s, "太阳方向有部分云")
	}
	return scored(s, "太阳方向上游云多，缺口小")
}

// CGap2 只把上游低云和一半中云算作挡光。
func CGap2(upLowAvg, upMidAvg *float64) Component {
	if upLowAvg == nil {
		return unknown("无上游云量数据")
	}
	block := Clamp(*upLowAvg+0.5*val(upMidAvg), 0, 100)
	s := Clamp(Lerp(block, 10, 90, 100, 0), 0, 100)
	txt := fmt.Sprintf("上游挡光云 %s%%", Fmt0(block))
	if s >= 80 {
		return scored(s, "太阳方向晴空缺口大（"+txt+"）")
	}
	if s >= 50 {
		return scored(s, "太阳方向有部分低 / 中云（"+txt+"）")
	}
	return scored(s, "太阳方向低 / 中云多，缺口小（"+txt+"）")
}

// UpstreamPoint 是太阳方向某一距离的预报点，nil 表示缺测。
type UpstreamPoint struct {
	DistanceKM float64
	Low        *float64
	Mid        *float64
	Total      *float64
}

func gap3MembershipLow(z float64) float64 {
	return Clamp((lowCloudTopKM-z)/lowCloudTopKM, 0, 1)
}

func gap3MembershipMid(z float64) float64 {
	return Clamp((z-midCloudBaseKM+1)/1, 0, 1) *
		Clamp((midCloudTopKM-z)/(midCloudTopKM-midCloudBaseKM), 0, 1)
}

// CGap3 按光路几何计算缺口。
//
// 太阳压到地平线下 α 度时，射向本地高度 h 云底的光在朝太阳方向 d km 处离地
// z = h − d·tanα + d²/2R。按 α ∈ {1°, 2°, 2.9°} 取阶段平均，每点用软隶属度
// 判断低/中云遮挡，再按去相关长度 200 km 合成透射率 T = Π(1−cᵢ)^(Δdᵢ/L)。
func CGap3(pts []UpstreamPoint, midLocal, highLocal *float64) Component {
	usable := make([]UpstreamPoint, 0, len(pts))
	for _, p := range pts {
		if p.Low != nil || p.Mid != nil {
			usable = append(usable, p)
		}
	}
	if len(usable) == 0 {
		return unknown("无上游云量数据")
	}
	sort.Slice(usable, func(i, j int) bool { return usable[i].DistanceKM < usable[j].DistanceKM })

	spans := make([]float64, len(usable))
	for k, p := range usable {
		prev := p.DistanceKM - 100
		if k > 0 {
			prev = usable[k-1].DistanceKM
		}
		if prev < 0 {
			prev = 0
		}
		next := p.DistanceKM + 100
		if k < len(usable)-1 {
			next = usable[k+1].DistanceKM
		}
		spans[k] = math.Max(1, (next-prev)/2)
	}
	spanTotal := sum(spans)
	if spanTotal <= 0 {
		return unknown("无上游云量数据")
	}
	for i := range spans {
		spans[i] = spans[i] * lightPathKM / spanTotal
	}

	deckT := func(deckH float64) *float64 {
		limit := -IlluminationLimit(deckH)
		stages := []float64{}
		for _, a := range []float64{1, 2, 2.9} {
			if a < limit {
				stages = append(stages, a)
			}
		}
		if len(stages) == 0 {
			return nil
		}
		ts := make([]float64, 0, len(stages))
		for _, aDeg := range stages {
			tanA := math.Tan(aDeg * math.Pi / 180)
			T := 1.0
			for k, p := range usable {
				z := deckH - p.DistanceKM*tanA + p.DistanceKM*p.DistanceKM/(2*earthRadiusKM)
				if z < 0 {
					z = 0
				}
				c := 0.0
				if p.Low != nil {
					cl := gap3MembershipLow(z) * (*p.Low) / 100
					c = c + cl - c*cl
				}
				if p.Mid != nil {
					cm := gap3MembershipMid(z) * (*p.Mid) / 100
					c = c + cm - c*cm
				}
				T *= math.Pow(1-c, spans[k]/decorrelationKM)
			}
			if T < 0.05 {
				T = 0.05
			}
			ts = append(ts, T)
		}
		avg := sum(ts) / float64(len(ts))
		return &avg
	}

	tHigh, tMid := deckT(DeckHigh), deckT(DeckMid)
	hi, mi := Clamp(val(highLocal), 0, 100), Clamp(val(midLocal), 0, 100)
	share := 0.5
	if hi+mi > 0 {
		share = hi / (hi + mi)
	}
	type part struct{ w, t float64 }
	parts := []part{}
	if tHigh != nil {
		parts = append(parts, part{share, *tHigh})
	}
	if tMid != nil {
		parts = append(parts, part{1 - share, *tMid})
	}
	if len(parts) == 0 {
		return unknown("无上游云量数据")
	}
	wsum := 0.0
	for _, p := range parts {
		wsum += p.w
	}
	if wsum == 0 {
		return unknown("无上游云量数据")
	}
	T := 0.0
	for _, p := range parts {
		T += p.w * p.t
	}
	T /= wsum
	s := Clamp(100*T, 0, 100)
	txt := fmt.Sprintf("光路透射 %s%%", fmtFixed(T*100, 0))
	if s >= 80 {
		return scored(s, "太阳方向光路通畅（"+txt+"）")
	}
	if s >= 50 {
		return scored(s, "太阳方向光路部分被挡（"+txt+"）")
	}
	return scored(s, "太阳方向光路被挡（"+txt+"）")
}

// CPrecip 评霞时刻降水。
func CPrecip(precip, prob *float64) Component {
	p, q := val(precip), val(prob)
	if p > 0.2 || q > 60 {
		return scored(0, "霞时刻有降水")
	}
	if q > 30 || p > 0 {
		return scored(50, "霞时刻降水概率 "+Fmt0(q)+"%")
	}
	return scored(100, "霞时刻无降水")
}

// CWashed 评雨后洗净加成。
func CWashed(pre3 float64, precipNow, probNow *float64) Component {
	dryNow := val(precipNow) <= 0.2 && val(probNow) <= 60
	if pre3 >= 1 && dryNow {
		return scored(100, "霞前 3 小时降水 "+fmtFixed(pre3, 1)+"mm 已停（雨后洗净）")
	}
	if pre3 >= 1 && !dryNow {
		return scored(0, "霞前降水 "+fmtFixed(pre3, 1)+"mm 但霞时刻未停")
	}
	if pre3 > 0.2 && dryNow {
		return scored(50, "霞前有少量降水")
	}
	return scored(0, "霞前无降水")
}

// CAod 评气溶胶光学厚度。
func CAod(aod *float64) Component {
	if aod == nil {
		return unknown("无 AOD 数据")
	}
	a := *aod
	switch {
	case a < 0.05:
		return scored(40, fmt.Sprintf("AOD 过低（%s），颜色偏淡", fmtFixed(a, 2)))
	case a <= 0.35:
		return scored(100, fmt.Sprintf("AOD 适中（%s）", fmtFixed(a, 2)))
	case a <= 0.5:
		return scored(Lerp(a, 0.35, 0.5, 100, 30), fmt.Sprintf("AOD 偏高（%s）", fmtFixed(a, 2)))
	case a <= 1.0:
		return scored(Lerp(a, 0.5, 1.0, 30, 0), fmt.Sprintf("AOD 很高（%s），压暗", fmtFixed(a, 2)))
	default:
		return scored(0, fmt.Sprintf("AOD 极高（%s）", fmtFixed(a, 2)))
	}
}

// CVis 评能见度，输入单位为米。
func CVis(visM *float64) Component {
	if visM == nil {
		return unknown("无能见度数据")
	}
	km := *visM / 1000
	switch {
	case km >= 15:
		return scored(100, "能见度 "+Fmt0(km)+"km")
	case km >= 8:
		return scored(Lerp(km, 8, 15, 40, 100), "能见度 "+Fmt0(km)+"km")
	default:
		return scored(Lerp(km, 0, 8, 0, 40), "能见度低 "+Fmt0(km)+"km")
	}
}

// CPm 评 PM2.5。
func CPm(pm *float64) Component {
	if pm == nil {
		return unknown("无 PM2.5 数据")
	}
	p := *pm
	switch {
	case p <= 15:
		return scored(100, "PM2.5 "+Fmt0(p))
	case p <= 35:
		return scored(Lerp(p, 15, 35, 100, 40), "PM2.5 "+Fmt0(p))
	case p <= 75:
		return scored(Lerp(p, 35, 75, 40, 0), "PM2.5 高 "+Fmt0(p))
	default:
		return scored(0, "PM2.5 很高 "+Fmt0(p))
	}
}

// CDew 评露点差。
func CDew(temp, dew *float64) Component {
	if temp == nil || dew == nil {
		return unknown("无露点数据")
	}
	d := *temp - *dew
	switch {
	case d >= 5:
		return scored(100, "露点差 "+fmtFixed(d, 1)+"°C（干透）")
	case d >= 2:
		return scored(Lerp(d, 2, 5, 30, 100), "露点差 "+fmtFixed(d, 1)+"°C")
	default:
		return scored(Lerp(d, 0, 2, 0, 30), "露点差小 "+fmtFixed(d, 1)+"°C（近饱和）")
	}
}

// CWind 评风速。
func CWind(ws *float64) Component {
	if ws == nil {
		return unknown("无风速数据")
	}
	w := *ws
	switch {
	case w < 5:
		return scored(60, "风速 "+Fmt0(w)+"km/h（近静风）")
	case w <= 20:
		return scored(100, "风速 "+Fmt0(w)+"km/h（微风）")
	case w <= 35:
		return scored(Lerp(w, 20, 35, 100, 40), "风速 "+Fmt0(w)+"km/h")
	default:
		return scored(Lerp(w, 35, 60, 40, 0), "风速大 "+Fmt0(w)+"km/h")
	}
}

// CCape 评对流不稳定能量。
func CCape(cape, low *float64, prob *float64) Component {
	if cape == nil {
		return unknown("无 CAPE 数据")
	}
	c := *cape
	if c > 2000 && val(low) > 50 && val(prob) > 50 {
		return scored(0, "CAPE 高（"+Fmt0(c)+"）+ 低云厚，对流未消")
	}
	if c > 1500 {
		return scored(60, "CAPE "+Fmt0(c)+"（对流活跃）")
	}
	return scored(100, "CAPE "+Fmt0(c))
}

// Components 保存全部分项，便于看板解释评分来源。
type Components struct {
	Curtain    Component
	CurtainV2  Component
	Low        Component
	Gap        Component
	GapV2      Component
	GapV3      Component
	Precip     Component
	PrecipV2   Component
	Washed     Component
	WashedV2   Component
	AOD        Component
	Vis        Component
	PM         Component
	Dew        Component
	Wind       Component
	Cape       Component
	Incomplete bool
}

// Input 是评分所需的全部输入。指针为 nil 表示缺数据；0 表示真实的晴/无降水。
type Input struct {
	Mid       *float64
	High      *float64
	Low       *float64
	Precip    *float64
	Prob      *float64
	PrecipV2  *float64
	ProbV2    *float64
	Pre3      float64
	Pre3V2    float64
	VisM      *float64
	Temp      *float64
	DewPoint  *float64
	WindSpeed *float64
	WindDir   *float64
	Cape      *float64
	AOD       *float64
	PM25      *float64
	Upstream  []UpstreamPoint
}

// Clarity 通透度（1–7）。
type Clarity struct {
	Level int
	Name  string
	Score int
}

var clarityNames = []string{"", "浑浊", "灰蒙", "微霾", "一般", "较通透", "通透", "极通透"}

// ClarityOf 按 AOD、能见度、PM2.5、露点差评通透度。
func ClarityOf(aod, visM, pm, dewSpread *float64) *Clarity {
	type part struct{ w, s float64 }
	parts := []part{}
	if aod != nil {
		s := 100.0
		if *aod > 0.1 {
			s = Lerp(*aod, 0.1, 0.8, 100, 0)
		}
		parts = append(parts, part{35, s})
	}
	if visM != nil {
		parts = append(parts, part{30, Lerp(*visM/1000, 5, 30, 0, 100)})
	}
	if pm != nil {
		s := 100.0
		if *pm > 10 {
			s = Lerp(*pm, 10, 75, 100, 0)
		}
		parts = append(parts, part{25, s})
	}
	if dewSpread != nil {
		parts = append(parts, part{10, Lerp(*dewSpread, 1, 8, 0, 100)})
	}
	if len(parts) == 0 {
		return nil
	}
	w := sum(func() []float64 {
		ws := make([]float64, len(parts))
		for i, p := range parts {
			ws[i] = p.w
		}
		return ws
	}())
	s := 0.0
	for _, p := range parts {
		s += p.w * p.s
	}
	s /= w
	lv := 1
	for _, b := range []struct {
		min int
		lv  int
	}{{88, 7}, {75, 6}, {62, 5}, {48, 4}, {35, 3}, {22, 2}} {
		if s >= float64(b.min) {
			lv = b.lv
			break
		}
	}
	return &Clarity{Level: lv, Name: clarityNames[lv], Score: int(PyRound(s))}
}

// Result 是单个事件的评分结果。
type Result struct {
	Score        int
	RawScore     float64
	Probability  float64
	Level        int
	Grade        string
	Conf         string
	Clarity      *Clarity
	CondScore    int
	CondRawScore float64
	CondGrade    string
	CondConf     string
	Reasons      []string
	ReasonsV2    []string
	Script       string
	Components   Components
	Ensemble     *EnsembleInfo
	Input        Input
}

// Member 是多模式集合的一个成员。
type Member struct {
	Model  string
	Label  string
	Weight float64
	Result *Result
}

// MemberSummary 是集合中单个成员的摘要。
type MemberSummary struct {
	Model string `json:"model"`
	Label string `json:"label"`
	Score int    `json:"score"`
	Grade string `json:"grade"`
}

// EnsembleInfo 是多模式集合的一致度信息。
type EnsembleInfo struct {
	N          int             `json:"n"`
	Good       int             `json:"good"`
	Near       int             `json:"near"`
	Spread     float64         `json:"spread"`
	AgreePct   int             `json:"agreePct"`
	NearPct    int             `json:"nearPct"`
	WeightSum  float64         `json:"weightSum"`
	AgreeBasis string          `json:"agreeBasis"`
	Members    []MemberSummary `json:"members"`
}

type weightItem struct {
	name string
	w    float64
}

var v1Weights = []weightItem{
	{"curtain", 30}, {"low", 20}, {"gap", 20}, {"precip", 10},
	{"aod", 5}, {"vis", 3}, {"pm", 3},
	{"dew", 2}, {"wind", 2}, {"cape", 2},
}

var airWeights = []weightItem{
	{"precipV2", 10}, {"aod", 5}, {"vis", 3}, {"pm", 3},
	{"dew", 2}, {"wind", 2}, {"cape", 2},
}

// LevelOf 返回分数对应的等级下标，见 Levels。
func LevelOf(score float64) int {
	for k := len(Levels) - 1; k > 0; k-- {
		if score >= float64(Levels[k].Min) {
			return k
		}
	}
	return 0
}

// LevelConf 返回离最近等级边界的置信度。
func LevelConf(score float64, anyMissing bool) string {
	dist := math.Inf(1)
	for _, b := range levelBounds {
		dist = math.Min(dist, math.Abs(score-b))
	}
	c := "高"
	if dist < 3 {
		c = "低"
	} else if dist < 6 {
		c = "中"
	}
	if anyMissing && c == "高" {
		c = "中"
	}
	return c
}

func condGrade(score float64) (string, string) {
	switch {
	case score >= 75:
		return "大烧", "🔥🔥🔥"
	case score >= 55:
		return "中烧", "🔥🔥"
	case score >= 35:
		return "小烧", "🔥"
	default:
		return "无烧", "☁️"
	}
}

func componentsFromMap(m map[string]Component) Components {
	get := func(k string) Component {
		if c, ok := m[k]; ok {
			return c
		}
		return unknown("")
	}
	return Components{
		Curtain: get("curtain"), CurtainV2: get("curtainV2"),
		Low: get("low"), Gap: get("gap"), GapV2: get("gapV2"), GapV3: get("gapV3"),
		Precip: get("precip"), PrecipV2: get("precipV2"),
		Washed: get("washed"), WashedV2: get("washedV2"),
		AOD: get("aod"), Vis: get("vis"), PM: get("pm"),
		Dew: get("dew"), Wind: get("wind"), Cape: get("cape"),
		Incomplete: m["__incomplete"].Text == "true",
	}
}

func glowScore(comps map[string]Component) (float64, bool) {
	gapName := "gapV2"
	if Flags.GapV3 && comps["gapV3"].Score != nil {
		gapName = "gapV3"
	}
	lw, ls := 0.0, 0.0
	keyMissing := false
	minKey := 100.0
	for _, item := range []weightItem{{"curtainV2", 0.5}, {gapName, 0.3}, {"low", 0.2}} {
		s := comps[item.name].Score
		if s == nil {
			keyMissing = true
			continue
		}
		if *s < minKey {
			minKey = *s
		}
		lw += item.w
		ls += item.w * math.Log(math.Max(*s, 1)/100)
	}
	key := 0.0
	if lw > 0 {
		key = 100 * math.Exp(ls/lw)
	}
	if minKey < 10 {
		key *= minKey / 10
	}
	aw, as := 0.0, 0.0
	for _, item := range airWeights {
		c, ok := comps[item.name]
		if !ok && item.name == "precipV2" {
			c = comps["precip"]
		}
		if c.Score == nil {
			continue
		}
		aw += item.w
		as += item.w * *c.Score
	}
	air := 70.0
	if aw > 0 {
		air = as / aw
	}
	g := key * (0.7 + 0.3*air/100)
	washed := comps["washedV2"]
	if washed.Score == nil {
		washed = comps["washed"]
	}
	if washed.Score != nil && *washed.Score != 0 {
		g += *washed.Score * 0.05
	}
	precipCap := comps["precipV2"]
	if precipCap.Score == nil {
		precipCap = comps["precip"]
	}
	if precipCap.Score != nil && *precipCap.Score == 0 {
		g = math.Min(g, 34)
	}
	if keyMissing {
		g = math.Min(g, 34)
	}
	comps["__incomplete"] = unknown(fmt.Sprintf("%t", keyMissing))
	return Clamp(g, 0, 100), keyMissing
}

// Evaluate 对单个事件评分。输入必须已经对齐到事件时刻。
func Evaluate(in Input) Result {
	cloudMissing := in.Mid == nil || in.High == nil || in.Low == nil
	mid, high, low := val(in.Mid), val(in.High), val(in.Low)

	precip := val(in.Precip)
	precipV2, probV2 := in.PrecipV2, in.ProbV2
	if precipV2 == nil {
		precipV2 = in.Precip
	}
	if probV2 == nil {
		probV2 = in.Prob
	}

	upLow, upTotal, upMid, upW := []float64{}, []float64{}, []float64{}, []float64{}
	for _, p := range in.Upstream {
		w := 1.0
		if p.DistanceKM > 0 {
			w = 1 / (1 + p.DistanceKM/100)
		}
		if p.Low != nil {
			upLow = append(upLow, *p.Low)
			upW = append(upW, w)
		}
		if p.Mid != nil {
			upMid = append(upMid, *p.Mid)
		}
		if p.Total != nil {
			upTotal = append(upTotal, *p.Total)
		}
	}
	var upLowAvg, upTotalAvg, upMidAvg *float64
	if len(upLow) > 0 {
		avg := sum(upLow) / float64(len(upLow))
		if upWsum := sum(upW); upWsum > 0 {
			weighted := 0.0
			for i, v := range upLow {
				weighted += v * upW[i]
			}
			avg = weighted / upWsum
		}
		upLowAvg = ptr(avg)
	}
	if len(upTotal) > 0 {
		upTotalAvg = ptr(sum(upTotal) / float64(len(upTotal)))
	}
	if len(upMid) > 0 {
		upMidAvg = ptr(sum(upMid) / float64(len(upMid)))
	}

	comps := map[string]Component{}
	if cloudMissing {
		comps["curtain"] = unknown("缺云量数据")
		comps["low"] = unknown("缺云量数据")
		comps["gapV2"] = unknown("缺云量数据")
		comps["gapV3"] = unknown("缺云量数据")
	} else {
		comps["curtain"] = CCurtain(mid, high)
		comps["low"] = CLow(low)
		comps["gapV2"] = CGap2(upLowAvg, upMidAvg)
		comps["gapV3"] = CGap3(in.Upstream, ptr(mid), ptr(high))
	}
	comps["gap"] = CGap(upLowAvg, upTotalAvg)
	comps["precip"] = CPrecip(in.Precip, in.Prob)
	comps["precipV2"] = CPrecip(precipV2, probV2)
	comps["washed"] = CWashed(in.Pre3, in.Precip, in.Prob)
	comps["washedV2"] = CWashed(in.Pre3V2, precipV2, probV2)
	comps["aod"] = CAod(in.AOD)
	comps["vis"] = CVis(in.VisM)
	comps["pm"] = CPm(in.PM25)
	comps["dew"] = CDew(in.Temp, in.DewPoint)
	comps["wind"] = CWind(in.WindSpeed)
	comps["cape"] = CCape(in.Cape, ptr(low), in.Prob)

	baseCurtain := CCurtainV2(mid, high)
	if cloudMissing || baseCurtain.Score == nil {
		comps["curtainV2"] = unknown("缺云量数据")
	} else {
		light := CurtainLight(high, mid, in.AOD)
		comps["curtainV2"] = scored(Clamp(*baseCurtain.Score*(0.75+0.25*light), 0, 100), baseCurtain.Text)
	}

	totalW, totalS := 0.0, 0.0
	for _, item := range v1Weights {
		if s := comps[item.name].Score; s != nil {
			totalW += item.w
			totalS += *s * item.w
		}
	}
	score := 50.0
	if totalW > 0 {
		score = totalS / totalW
	}
	if s := comps["washed"].Score; s != nil && *s != 0 {
		score = math.Min(100, score+*s*0.03)
	}

	hazeNote := ""
	if in.WindDir != nil && *in.WindDir >= 135 && *in.WindDir <= 225 && in.AOD != nil && *in.AOD > 0.35 {
		score = math.Max(0, score-5)
		hazeNote = "偏南风 + AOD 高，跨境烟霾风险"
	}
	if s := comps["precip"].Score; s != nil && *s == 0 {
		score = math.Min(score, 40)
	}
	condGradeName, _ := condGrade(score)
	dist := math.Inf(1)
	for _, b := range []float64{35, 55, 75} {
		dist = math.Min(dist, math.Abs(score-b))
	}
	condConf := "高"
	if dist < 6 {
		condConf = "中"
	}
	if dist < 12 && condConf == "高" {
		condConf = "中"
	}
	anyMissing := false
	for _, item := range v1Weights {
		if comps[item.name].Score == nil {
			anyMissing = true
			break
		}
	}
	if anyMissing && condConf == "高" {
		condConf = "中"
	}

	lowestOthers := func(replacements map[string]string, curtainKey string) []string {
		type entry struct {
			s    float64
			text string
		}
		others := []entry{}
		for _, item := range v1Weights {
			if item.name == "curtain" {
				continue
			}
			name := item.name
			if r, ok := replacements[name]; ok {
				name = r
			}
			c := comps[name]
			if c.Score != nil {
				others = append(others, entry{*c.Score, c.Text})
			}
		}
		sort.Slice(others, func(i, j int) bool { return others[i].s < others[j].s })
		_ = curtainKey
		out := []string{}
		for i := 0; i < len(others) && i < 2; i++ {
			out = append(out, others[i].text)
		}
		return out
	}

	reasons := []string{comps["curtain"].Text}
	reasons = append(reasons, lowestOthers(nil, "curtain")...)
	if s := comps["washed"].Score; s != nil && *s == 100 {
		reasons = append(reasons, comps["washed"].Text)
	}
	if hazeNote != "" {
		reasons = append(reasons, hazeNote)
	}

	gapName := "gapV2"
	if Flags.GapV3 && comps["gapV3"].Score != nil {
		gapName = "gapV3"
	}
	reasonsV2 := []string{comps["curtainV2"].Text}
	reasonsV2 = append(reasonsV2, lowestOthers(map[string]string{"gap": gapName, "precip": "precipV2"}, "curtainV2")...)
	if s := comps["washedV2"].Score; s != nil && *s == 100 {
		reasonsV2 = append(reasonsV2, comps["washedV2"].Text)
	}
	if hazeNote != "" {
		reasonsV2 = append(reasonsV2, hazeNote)
	}

	script := "混合型"
	switch {
	case comps["precip"].Score != nil && *comps["precip"].Score == 0:
		script = "降水遮挡型"
	case comps["cape"].Score != nil && *comps["cape"].Score == 0:
		script = "对流未消型"
	case comps["washed"].Score != nil && *comps["washed"].Score == 100 && score >= 55:
		script = "雨后型"
	case (comps["pm"].Score != nil && *comps["pm"].Score < 40) ||
		(comps["vis"].Score != nil && *comps["vis"].Score < 40):
		script = "霾型"
	case comps["curtain"].Score != nil && *comps["curtain"].Score >= 70 && precip <= 0.2:
		script = "晴空薄高云型"
	}

	g, keyMissing := glowScore(comps)
	comps["__incomplete"] = unknown(fmt.Sprintf("%t", keyMissing))
	shown := int(PyRound(g))
	lv := LevelOf(float64(shown))
	var dewSpread *float64
	if in.Temp != nil && in.DewPoint != nil {
		dewSpread = ptr(*in.Temp - *in.DewPoint)
	}

	return Result{
		Score:        shown,
		RawScore:     g,
		Probability:  ScoreToProb(g),
		Level:        lv,
		Grade:        Levels[lv].Name,
		Conf:         LevelConf(g, anyMissing),
		Clarity:      ClarityOf(in.AOD, in.VisM, in.PM25, dewSpread),
		CondScore:    int(PyRound(score)),
		CondRawScore: score,
		CondGrade:    condGradeName,
		CondConf:     condConf,
		Reasons:      reasons,
		ReasonsV2:    reasonsV2,
		Script:       script,
		Components:   componentsFromMap(comps),
		Input:        in,
	}
}

// Combine 对多模式结果做加权平均。说明沿用最接近综合分数的那个模式。
func Combine(members []Member) *Result {
	ok := []Member{}
	for _, m := range members {
		if m.Result != nil {
			ok = append(ok, m)
		}
	}
	if len(ok) == 0 {
		return nil
	}
	scores := make([]float64, len(ok))
	for i, m := range ok {
		scores[i] = m.Result.RawScore
	}
	sort.Float64s(scores)
	n := len(scores)
	tw, ts := 0.0, 0.0
	for _, m := range ok {
		w := m.Weight
		if w == 0 {
			w = 1
		}
		tw += w
		ts += w * m.Result.RawScore
	}
	mean := ts / tw
	rep := ok[0]
	repDist := math.Abs(rep.Result.RawScore - mean)
	for _, m := range ok[1:] {
		if d := math.Abs(m.Result.RawScore - mean); d < repDist {
			rep, repDist = m, d
		}
	}
	shownMean := int(PyRound(mean))
	lv := LevelOf(float64(shownMean))
	near, good := 0, 0
	nearW := 0.0
	for _, m := range ok {
		if math.Abs(float64(LevelOf(m.Result.RawScore)-lv)) <= 1 {
			near++
			w := m.Weight
			if w == 0 {
				w = 1
			}
			nearW += w
		}
		if m.Result.RawScore >= 50 {
			good++
		}
	}
	spread := scores[n-1] - scores[0]
	agreeW := 0.0
	if tw > 0 {
		agreeW = nearW / tw
	}
	conf := rep.Result.Conf
	if n >= 3 {
		switch {
		case agreeW >= 0.8 && spread <= 30:
			conf = "高"
		case agreeW >= 0.6:
			conf = "中"
		default:
			conf = "低"
		}
	}

	out := *rep.Result
	out.Score = shownMean
	out.RawScore = mean
	out.Probability = ScoreToProb(mean)
	out.Level = lv
	out.Grade = Levels[lv].Name
	out.Conf = conf
	summaries := make([]MemberSummary, 0, len(ok))
	for _, m := range ok {
		summaries = append(summaries, MemberSummary{
			Model: m.Model, Label: m.Label,
			Score: m.Result.Score, Grade: m.Result.Grade,
		})
	}
	out.Ensemble = &EnsembleInfo{
		N: n, Good: good, Near: near, Spread: PyRound(spread),
		AgreePct:  int(math.Round(agreeW * 100)),
		NearPct:   int(math.Round(100 * float64(near) / float64(n))),
		WeightSum: PyRound(tw), AgreeBasis: "weighted", Members: summaries,
	}
	return &out
}
