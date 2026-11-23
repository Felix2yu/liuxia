package main

import (
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func omTestTimes() []string {
	start := time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC)
	out := make([]string, 72)
	for i := range out {
		out[i] = start.Add(time.Duration(i) * time.Hour).Format("2006-01-02T15:04")
	}
	return out
}

func omTestArray(size, index int, value float64) []*float64 {
	arr := make([]*float64, size)
	arr[index] = &value
	return arr
}

func omTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	times := omTestTimes()
	const eventIdx = 34
	daily := omDaily{
		Time:    []string{"2026-11-22", "2026-11-23", "2026-11-24", "2026-11-25"},
		Sunrise: []string{"2026-11-22T01:00", "2026-11-23T01:00", "2026-11-24T01:00", "2026-11-25T01:00"},
		Sunset:  []string{"2026-11-22T10:00", "2026-11-23T10:00", "2026-11-24T10:00", "2026-11-25T10:00"},
	}
	cloudHourly := omHourly{
		Time:           times,
		CloudCover:     omTestArray(len(times), eventIdx, 60),
		CloudCoverLow:  omTestArray(len(times), eventIdx, 10),
		CloudCoverMid:  omTestArray(len(times), eventIdx, 20),
		CloudCoverHigh: omTestArray(len(times), eventIdx, 40),
	}

	upLow := []float64{5, 10, 20, 30, 40}
	upMid := []float64{20, 30, 40, 50, 60}
	upTotal := []float64{30, 40, 50, 60, 70}

	weatherHourly := omHourly{
		Time:                     times,
		Precipitation:            omTestArray(len(times), eventIdx, 0),
		PrecipitationProbability: omTestArray(len(times), eventIdx, 0),
		Visibility:               omTestArray(len(times), eventIdx, 20000),
		Temperature2m:            omTestArray(len(times), eventIdx, 22),
		DewPoint2m:               omTestArray(len(times), eventIdx, 12),
		WindSpeed10m:             omTestArray(len(times), eventIdx, 12),
		WindDirection10m:         omTestArray(len(times), eventIdx, 270),
		Cape:                     omTestArray(len(times), eventIdx, 500),
	}
	weatherLoc := omLocation{
		Timezone: "UTC", UtcOffsetSeconds: 0,
		Hourly: weatherHourly, Daily: daily,
	}

	aqBody, _ := json.Marshal(map[string]interface{}{
		"utc_offset_seconds": 0,
		"hourly": map[string]interface{}{
			"time":                  times,
			"aerosol_optical_depth": omTestArray(len(times), eventIdx, 0.2),
			"pm2_5":                 omTestArray(len(times), eventIdx, 20),
		},
	})

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/geocoding"):
			_, _ = w.Write([]byte(`{"results":[{"name":"北京","latitude":39.9,"longitude":116.4,"timezone":"Asia/Shanghai"}]}`))
		case strings.HasSuffix(r.URL.Path, "/air-quality"):
			_, _ = w.Write(aqBody)
		case strings.HasSuffix(r.URL.Path, "/forecast"):
			if strings.Contains(r.URL.Query().Get("hourly"), "cloud_cover") {
				lats := strings.Split(r.URL.Query().Get("latitude"), ",")
				locs := make([]omLocation, 0, len(lats))
				for i := range lats {
					h := cloudHourly
					if i > 0 {
						k := i - 1
						h.CloudCoverLow = omTestArray(len(times), eventIdx, upLow[k])
						h.CloudCoverMid = omTestArray(len(times), eventIdx, upMid[k])
						h.CloudCover = omTestArray(len(times), eventIdx, upTotal[k])
					}
					locs = append(locs, omLocation{
						Timezone: "UTC", UtcOffsetSeconds: 0,
						Hourly: h, Daily: daily,
					})
				}
				_ = json.NewEncoder(w).Encode(locs)
				return
			}
			_ = json.NewEncoder(w).Encode(weatherLoc)
		default:
			http.NotFound(w, r)
		}
	}))
}

func omTestPredictor(t *testing.T, server *httptest.Server, withStore bool) (*WeatherPredictor, *Store) {
	t.Helper()
	var store *Store
	if withStore {
		s, err := InitStore(filepath.Join(t.TempDir(), "test.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = s.Close() })
		store = s
	}
	cfg := &Config{
		Request: RequestConfig{
			Source:       "open-meteo",
			ForecastURL:  server.URL + "/forecast",
			AQURL:        server.URL + "/air-quality",
			GeocodingURL: server.URL + "/geocoding",
		},
		Push: PushConfig{Enable: false},
	}
	return NewWeatherPredictor(cfg, log.New(&strings.Builder{}, "", 0), store), store
}

func TestFetchGlowData(t *testing.T) {
	server := omTestServer(t)
	defer server.Close()
	wp, _ := omTestPredictor(t, server, false)

	got := wp.fetchGlowData("北京", "GFS", "2026-11-24", false, time.Now())
	if got == nil {
		t.Fatal("fetchGlowData 返回 nil")
	}
	if got.Score == nil {
		t.Fatal("Score 为空")
	}
	if *got.Score < 80 || *got.Score > 86 {
		t.Errorf("Score = %.2f, want 约 83.14", *got.Score)
	}
	if got.Level != 5 || got.Grade != "大烧" {
		t.Errorf("等级异常: level=%d grade=%s", got.Level, got.Grade)
	}
	if got.DateStr != "2026-11-24" || got.TimeStr != "10:00" {
		t.Errorf("事件时间异常: %s %s", got.DateStr, got.TimeStr)
	}
	if got.AODNum == nil || *got.AODNum != 0.2 {
		t.Errorf("AOD = %v, want 0.2", got.AODNum)
	}
	if !strings.Contains(got.PushStr, "霞分") {
		t.Errorf("推送文案缺少评分: %s", got.PushStr)
	}
}

func TestBuildGlowResponseStoresScore(t *testing.T) {
	server := omTestServer(t)
	defer server.Close()
	wp, store := omTestPredictor(t, server, true)

	lines, priority, hasData := wp.buildGlowResponse("北京", []string{"GFS"}, false, time.Now(), "evening", []string{"2026-11-24"})
	if !hasData {
		t.Fatal("buildGlowResponse 无数据")
	}
	if priority == nil || *priority != 5 {
		t.Errorf("priority = %v, want 5", priority)
	}
	if len(lines) == 0 {
		t.Error("推送内容为空")
	}
	records, err := store.QueryRecords("北京", "evening", "2026-11-24", "2026-11-24")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("记录数 = %d, want 1", len(records))
	}
	r := records[0]
	if r.Score == nil || *r.Score < 80 {
		t.Errorf("落库 Score = %v", r.Score)
	}
	if r.Grade != "大烧" || r.Clarity == "" || r.Probability == nil {
		t.Errorf("落库字段异常: %+v", r)
	}
	if r.Quality != nil {
		t.Errorf("新数据不应写 quality，得到 %v", *r.Quality)
	}

	stats, err := store.GetStatistics("北京", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(stats.Models) != 1 || stats.Models[0].AvgScore == nil {
		t.Errorf("统计缺少 avg_score: %+v", stats.Models)
	}
	rankings, err := store.GetRankings("北京", "", "", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rankings.BestScores) != 1 || rankings.BestScores[0].Grade != "大烧" {
		t.Errorf("新评分排行异常: %+v", rankings.BestScores)
	}
}

// TestLiveOpenMeteoSmoke 用真实 Open-Meteo 数据验证取数与评分链路。
// 默认跳过；设置 LIUXIA_LIVE_SMOKE=1 后运行。可用 FORECAST_URL / FORECAST_MODELS
// 指向镜像或集合接口，例如 ensemble-api.open-meteo.com + ecmwf_ifs025。
func TestLiveOpenMeteoSmoke(t *testing.T) {
	if os.Getenv("LIUXIA_LIVE_SMOKE") == "" {
		t.Skip("设置 LIUXIA_LIVE_SMOKE=1 后运行")
	}
	cfg := &Config{
		Request: RequestConfig{
			Source:         "open-meteo",
			ForecastURL:    os.Getenv("FORECAST_URL"),
			ForecastModels: os.Getenv("FORECAST_MODELS"),
		},
		Push: PushConfig{Enable: false},
	}
	wp := NewWeatherPredictor(cfg, log.New(os.Stdout, "", 0), nil)
	date := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	got := wp.fetchGlowData("北京", "EC", date, false, time.Now())
	if got == nil || got.Score == nil {
		t.Fatalf("真实取数失败: %+v", got)
	}
	if *got.Score < 0 || *got.Score > 100 {
		t.Errorf("霞分越界: %.2f", *got.Score)
	}
	t.Logf("真实数据：%s %s 霞分 %.2f，等级 %s，概率 %.1f%%",
		got.DateStr, got.TimeStr, *got.Score, got.Grade, *got.Probability*100)
}

func TestFetchDataUsesOpenMeteo(t *testing.T) {
	server := omTestServer(t)
	defer server.Close()
	wp, store := omTestPredictor(t, server, true)
	wp.config.Schedule.Cities = []string{"北京"}
	wp.config.Schedule.Evening.Model = []string{"GFS"}
	wp.config.Schedule.Evening.Time = []string{"23:59"}
	wp.config.Schedule.Morning.Enable = false

	wp.FetchData(false)

	records, err := store.QueryRecords("北京", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) == 0 {
		t.Fatal("FetchData 未落库")
	}
	for _, r := range records {
		if r.Score == nil {
			t.Errorf("记录缺少 score: %+v", r)
		}
	}
}
