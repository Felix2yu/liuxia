package glow

import (
	"math"
	"testing"
)

func fp(v float64) *float64 { return &v }

func closeTo(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("%s = %.12f, want %.12f", name, got, want)
	}
}

func TestPyRoundClampLerp(t *testing.T) {
	cases := []struct {
		name string
		in   float64
		want float64
	}{
		{"0.5 舍偶到 0", 0.5, 0},
		{"1.5 舍偶到 2", 1.5, 2},
		{"2.5 舍偶到 2", 2.5, 2},
		{"-1.5 舍偶到 -2", -1.5, -2},
		{"普通进位", 1.6, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { closeTo(t, "PyRound", PyRound(c.in), c.want) })
	}
	closeTo(t, "Clamp 下界", Clamp(-1, 0, 100), 0)
	closeTo(t, "Clamp 上界", Clamp(101, 0, 100), 100)
	closeTo(t, "Lerp 中点", Lerp(5, 0, 10, 0, 100), 50)
	closeTo(t, "Lerp 左截断", Lerp(-5, 0, 10, 0, 100), 0)
}

func TestSolarGeometry(t *testing.T) {
	cases := []struct {
		lat, doy        float64
		sunset, sunrise float64
	}{
		{31.2304, 298, 254.792080753, 105.207919247},
		{39.9042, 1, 239.349384115, 120.650615885},
		{0, 80, 269.470812394, 90.529187606},
		{51.5, 172, 309.714894615, 50.285105385},
	}
	for _, c := range cases {
		closeTo(t, "SunsetBearing", SunsetBearing(c.lat, int(c.doy)), c.sunset)
		closeTo(t, "SunriseBearing", SunriseBearing(c.lat, int(c.doy)), c.sunrise)
	}
	lat, lon := Destination(31.2304, 121.4737, 285, 300)
	closeTo(t, "Destination.lat", lat, 31.89225033259703)
	closeTo(t, "Destination.lon", lon, 118.40398837678148)
	closeTo(t, "IlluminationLimit high", IlluminationLimit(DeckHigh), -2.977028918)
	closeTo(t, "IlluminationLimit mid", IlluminationLimit(DeckMid), -2.314917401)
	closeTo(t, "DeckLum high", DeckLum(DeckHigh, fp(0.15)), 0.282273879503)
	closeTo(t, "DeckLum mid", DeckLum(DeckMid, fp(0.15)), 0.134878276925)
}

func TestComponentThresholds(t *testing.T) {
	t.Run("幕布", func(t *testing.T) {
		closeTo(t, "无云", *CCurtain(0, 0).Score, 0)
		closeTo(t, "30% 到顶前", *CCurtain(0, 30).Score, 70)
		closeTo(t, "70%", *CCurtain(0, 70).Score, 100)
		closeTo(t, "90%", *CCurtain(0, 90).Score, 70)
		closeTo(t, "100%", *CCurtain(0, 100).Score, 30)
		closeTo(t, "随机重叠", *CCurtainV2(60, 40).Score, *CCurtain(0, 76).Score)
	})
	t.Run("低云", func(t *testing.T) {
		closeTo(t, "20%", *CLow(20).Score, 100)
		closeTo(t, "60%", *CLow(60).Score, 30)
		closeTo(t, "100%", *CLow(100).Score, 0)
	})
	t.Run("AOD", func(t *testing.T) {
		closeTo(t, "0.04", *CAod(fp(0.04)).Score, 40)
		closeTo(t, "0.2", *CAod(fp(0.2)).Score, 100)
		closeTo(t, "0.35", *CAod(fp(0.35)).Score, 100)
		closeTo(t, "0.5", *CAod(fp(0.5)).Score, 30)
		closeTo(t, "1.0", *CAod(fp(1.0)).Score, 0)
		if CAod(nil).Score != nil {
			t.Error("缺 AOD 应返回 nil 分项")
		}
	})
	t.Run("能见度与 PM", func(t *testing.T) {
		closeTo(t, "15km", *CVis(fp(15000)).Score, 100)
		closeTo(t, "8km", *CVis(fp(8000)).Score, 40)
		closeTo(t, "PM15", *CPm(fp(15)).Score, 100)
		closeTo(t, "PM75", *CPm(fp(75)).Score, 0)
	})
	t.Run("风与对流", func(t *testing.T) {
		closeTo(t, "静风", *CWind(fp(0)).Score, 60)
		closeTo(t, "微风", *CWind(fp(12)).Score, 100)
		closeTo(t, "CAPE 活跃", *CCape(fp(1600), fp(10), fp(0)).Score, 60)
		closeTo(t, "CAPE 对流未消", *CCape(fp(2200), fp(60), fp(60)).Score, 0)
	})
}

func TestLightPathGeometry(t *testing.T) {
	mid, high := fp(35), fp(55)
	clear := []UpstreamPoint{
		{DistanceKM: 50, Low: fp(5), Mid: fp(20), Total: fp(30)},
		{DistanceKM: 100, Low: fp(10), Mid: fp(30), Total: fp(40)},
		{DistanceKM: 200, Low: fp(20), Mid: fp(40), Total: fp(50)},
		{DistanceKM: 300, Low: fp(30), Mid: fp(50), Total: fp(60)},
		{DistanceKM: 400, Low: fp(40), Mid: fp(60), Total: fp(70)},
	}
	blocked := []UpstreamPoint{
		{DistanceKM: 50, Low: fp(80), Mid: fp(60), Total: fp(90)},
		{DistanceKM: 100, Low: fp(70), Mid: fp(50), Total: fp(85)},
		{DistanceKM: 200, Low: fp(60), Mid: fp(40), Total: fp(75)},
		{DistanceKM: 300, Low: fp(50), Mid: fp(35), Total: fp(65)},
		{DistanceKM: 400, Low: fp(40), Mid: fp(30), Total: fp(60)},
	}
	clearScore := *CGap3(clear, mid, high).Score
	blockedScore := *CGap3(blocked, mid, high).Score
	if clearScore <= blockedScore {
		t.Errorf("干净光路 %.4f 应高于受阻光路 %.4f", clearScore, blockedScore)
	}
	closeTo(t, "干净光路 cGap3", clearScore, 74.776589354352)
	closeTo(t, "受阻光路 cGap3", blockedScore, 66.433328797646)
	closeTo(t, "高云照亮极限", -IlluminationLimit(DeckHigh), 2.977028918)
	if got := CGap3(nil, mid, high); got.Score != nil {
		t.Errorf("无上游数据应返回 nil，得到 %v", *got.Score)
	}
}

func TestEvaluateParity(t *testing.T) {
	cases := []struct {
		name    string
		in      Input
		score   int
		raw     float64
		prob    float64
		level   int
		cond    int
		condRaw float64
		clarity int
		comps   map[string]*float64
	}{
		{
			name: "晴空薄高云",
			in: Input{
				Mid: fp(20), High: fp(40), Low: fp(10),
				Precip: fp(0), Prob: fp(0), PrecipV2: fp(0), ProbV2: fp(0),
				VisM: fp(20000), Temp: fp(22), DewPoint: fp(12),
				WindSpeed: fp(12), WindDir: fp(270), Cape: fp(500),
				AOD: fp(0.2), PM25: fp(20),
				Upstream: []UpstreamPoint{
					{DistanceKM: 50, Low: fp(5), Mid: fp(20), Total: fp(30)},
					{DistanceKM: 100, Low: fp(10), Mid: fp(30), Total: fp(40)},
					{DistanceKM: 200, Low: fp(20), Mid: fp(40), Total: fp(50)},
					{DistanceKM: 300, Low: fp(30), Mid: fp(50), Total: fp(60)},
					{DistanceKM: 400, Low: fp(40), Mid: fp(60), Total: fp(70)},
				},
			},
			score: 83, raw: 83.13656262080285, prob: 0.5489356225174432,
			level: 5, cond: 91, condRaw: 91.03092783505154, clarity: 79,
			comps: map[string]*float64{
				"aod": fp(100), "cape": fp(100), "curtain": fp(92.5),
				"curtainV2": fp(82.65116292992748), "dew": fp(100), "gap": fp(70),
				"gapV2": fp(67.94871794871794), "gapV3": fp(75.47706379032887),
				"low": fp(100), "pm": fp(85), "precip": fp(100), "precipV2": fp(100),
				"vis": fp(100), "washed": fp(0), "washedV2": fp(0), "wind": fp(100),
			},
		},
		{
			name: "低云厚且有降水",
			in: Input{
				Mid: fp(60), High: fp(10), Low: fp(85),
				Precip: fp(0.5), Prob: fp(80), PrecipV2: fp(0.4), ProbV2: fp(70),
				VisM: fp(3000), Temp: fp(18), DewPoint: fp(17),
				WindSpeed: fp(6), WindDir: fp(180), Cape: fp(2200),
				AOD: fp(0.8), PM25: fp(90),
				Upstream: []UpstreamPoint{
					{DistanceKM: 50, Low: fp(80), Mid: fp(60), Total: fp(90)},
					{DistanceKM: 100, Low: fp(70), Mid: fp(50), Total: fp(85)},
					{DistanceKM: 200, Low: fp(60), Mid: fp(40), Total: fp(75)},
					{DistanceKM: 300, Low: fp(50), Mid: fp(35), Total: fp(65)},
					{DistanceKM: 400, Low: fp(40), Mid: fp(30), Total: fp(60)},
				},
			},
			score: 34, raw: 34, prob: 0.16161641710244362,
			level: 1, cond: 34, condRaw: 34.450171821305844, clarity: 0,
			comps: map[string]*float64{
				"aod": fp(11.999999999999996), "cape": fp(0), "curtain": fp(100),
				"curtainV2": fp(85.95), "dew": fp(15), "gap": fp(13.333333333333336),
				"gapV2": fp(3.039529914529922), "gapV3": fp(57.27249480251342),
				"low": fp(11.25), "pm": fp(0), "precip": fp(0), "precipV2": fp(0),
				"vis": fp(15), "washed": fp(0), "washedV2": fp(0), "wind": fp(100),
			},
		},
		{
			name: "雨后洗净",
			in: Input{
				Mid: fp(35), High: fp(55), Low: fp(20),
				Precip: fp(0.4), Prob: fp(10), PrecipV2: fp(0), ProbV2: fp(0),
				Pre3: 0.8, Pre3V2: 1.2,
				VisM: fp(15000), Temp: fp(20), DewPoint: fp(10),
				WindSpeed: fp(9), WindDir: fp(300), Cape: fp(300),
				AOD: fp(0.15), PM25: fp(12),
				Upstream: []UpstreamPoint{
					{DistanceKM: 50, Low: fp(10), Mid: fp(25), Total: fp(35)},
					{DistanceKM: 100, Low: fp(15), Mid: fp(30), Total: fp(45)},
					{DistanceKM: 200, Low: fp(20), Mid: fp(35), Total: fp(50)},
					{DistanceKM: 300, Low: fp(25), Mid: fp(40), Total: fp(55)},
					{DistanceKM: 400, Low: fp(30), Mid: fp(45), Total: fp(60)},
				},
			},
			score: 95, raw: 94.82151205658423, prob: 0.6535219613467863,
			level: 5, cond: 40, condRaw: 40, clarity: 79,
			comps: map[string]*float64{
				"aod": fp(100), "cape": fp(100), "curtain": fp(70),
				"curtainV2": fp(93.85543739894365), "dew": fp(100), "gap": fp(71.5),
				"gapV2": fp(69.41773504273505), "gapV3": fp(77.71451524093092),
				"low": fp(100), "pm": fp(100), "precip": fp(0), "precipV2": fp(100),
				"vis": fp(100), "washed": fp(0), "washedV2": fp(100), "wind": fp(100),
			},
		},
		{
			name: "缺云量",
			in: Input{
				Precip: fp(0), Prob: fp(20), PrecipV2: fp(0), ProbV2: fp(20),
				VisM: fp(12000), Temp: fp(19), DewPoint: fp(11),
				WindSpeed: fp(15), WindDir: fp(90), Cape: nil,
				AOD: fp(0.3), PM25: fp(30),
			},
			score: 0, raw: 0, prob: 0.05111304126548428,
			level: 0, cond: 92, condRaw: 91.51428571428572, clarity: 61,
			comps: map[string]*float64{
				"aod": fp(100), "cape": nil, "curtain": nil, "curtainV2": nil,
				"dew": fp(100), "gap": nil, "gapV2": nil, "gapV3": nil,
				"low": nil, "pm": fp(55), "precip": fp(100), "precipV2": fp(100),
				"vis": fp(74.28571428571428), "washed": fp(0), "washedV2": fp(0),
				"wind": fp(100),
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Evaluate(tc.in)
			if got.Score != tc.score {
				t.Errorf("score = %d, want %d", got.Score, tc.score)
			}
			closeTo(t, "rawScore", got.RawScore, tc.raw)
			closeTo(t, "probability", got.Probability, tc.prob)
			if got.Level != tc.level {
				t.Errorf("level = %d, want %d", got.Level, tc.level)
			}
			if got.CondScore != tc.cond {
				t.Errorf("condScore = %d, want %d", got.CondScore, tc.cond)
			}
			closeTo(t, "condRawScore", got.CondRawScore, tc.condRaw)
			if got.Clarity == nil || got.Clarity.Score != tc.clarity {
				t.Errorf("clarity = %+v, want %d", got.Clarity, tc.clarity)
			}
			gotComps := map[string]*float64{
				"curtain": got.Components.Curtain.Score, "curtainV2": got.Components.CurtainV2.Score,
				"low": got.Components.Low.Score, "gap": got.Components.Gap.Score,
				"gapV2": got.Components.GapV2.Score, "gapV3": got.Components.GapV3.Score,
				"precip": got.Components.Precip.Score, "precipV2": got.Components.PrecipV2.Score,
				"washed": got.Components.Washed.Score, "washedV2": got.Components.WashedV2.Score,
				"aod": got.Components.AOD.Score, "vis": got.Components.Vis.Score,
				"pm": got.Components.PM.Score, "dew": got.Components.Dew.Score,
				"wind": got.Components.Wind.Score, "cape": got.Components.Cape.Score,
			}
			for k, want := range tc.comps {
				g := gotComps[k]
				if want == nil || g == nil {
					if want != g {
						t.Errorf("comps[%s] nil 不匹配: got %v want %v", k, g, want)
					}
					continue
				}
				closeTo(t, "comps["+k+"]", *g, *want)
			}
		})
	}
}

func TestCombine(t *testing.T) {
	mk := func(raw float64) *Result {
		s := int(PyRound(raw))
		lv := LevelOf(float64(s))
		return &Result{RawScore: raw, Score: s, Level: lv, Grade: Levels[lv].Name, Conf: "中"}
	}
	got := Combine([]Member{
		{Model: "best_match", Label: "综合", Weight: 3, Result: mk(82)},
		{Model: "ecmwf_ifs025", Label: "ECMWF", Weight: 1, Result: mk(61)},
		{Model: "gfs_seamless", Label: "GFS", Weight: 1, Result: mk(48)},
		{Model: "icon_seamless", Label: "ICON", Weight: 1, Result: mk(76)},
	})
	if got == nil {
		t.Fatal("Combine 返回 nil")
	}
	if got.Score != 72 || got.Level != 4 || got.Grade != "中到大烧" || got.Conf != "中" {
		t.Errorf("集合结果异常: score=%d level=%d grade=%s conf=%s", got.Score, got.Level, got.Grade, got.Conf)
	}
	closeTo(t, "rawScore", got.RawScore, 71.83333333333333)
	closeTo(t, "probability", got.Probability, 0.4433690400777465)
	closeTo(t, "spread", got.Ensemble.Spread, 34)
	if got.Ensemble.AgreePct != 83 || got.Ensemble.NearPct != 75 || got.Ensemble.Near != 3 || got.Ensemble.Good != 3 {
		t.Errorf("一致度异常: %+v", got.Ensemble)
	}
	if Combine(nil) != nil {
		t.Error("空集合应返回 nil")
	}
}

func TestLevelBoundaries(t *testing.T) {
	cases := []struct {
		score int
		level int
	}{
		{0, 0}, {19, 0}, {20, 1}, {34, 1}, {35, 2}, {49, 2},
		{50, 3}, {64, 3}, {65, 4}, {79, 4}, {80, 5}, {100, 5},
	}
	for _, c := range cases {
		if got := LevelOf(float64(c.score)); got != c.level {
			t.Errorf("LevelOf(%d) = %d, want %d", c.score, got, c.level)
		}
	}
	closeTo(t, "ScoreToProb(77.9)", ScoreToProb(77.9), 0.5)
}

func TestBlendCloudAt(t *testing.T) {
	h := HourlyCloud{
		Time:       []string{"2026-11-24T17:00", "2026-11-24T18:00"},
		CloudCover: []*float64{fp(20), fp(80)},
		Low:        []*float64{nil, fp(40)},
	}
	epoch, err := ToEpoch("2026-11-24T17:30", 0)
	if err != nil {
		t.Fatal(err)
	}
	out := BlendCloudAt(h, epoch, 0)
	if out.CloudCover[0] == nil {
		t.Fatal("插值结果不应为 nil")
	}
	closeTo(t, "总云量插值", *out.CloudCover[0], 50)
	if out.Low[0] == nil || *out.Low[0] != 40 {
		t.Errorf("缺测应取另一格，得到 %v", out.Low[0])
	}
	if *h.CloudCover[0] != 20 {
		t.Error("BlendCloudAt 不应修改原数据")
	}
}
