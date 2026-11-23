package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"liuxia/internal/glow"
)

const (
	labelPageSize = 20
	maxLabelPages = 120
)

type labelPage struct {
	Status       string     `json:"status"`
	TableContent [][]string `json:"table_content"`
}

type hourly struct {
	Time                     []string   `json:"time"`
	CloudCover               []*float64 `json:"cloud_cover"`
	CloudCoverLow            []*float64 `json:"cloud_cover_low"`
	CloudCoverMid            []*float64 `json:"cloud_cover_mid"`
	CloudCoverHigh           []*float64 `json:"cloud_cover_high"`
	Precipitation            []*float64 `json:"precipitation"`
	PrecipitationProbability []*float64 `json:"precipitation_probability"`
	Visibility               []*float64 `json:"visibility"`
	Temperature2m            []*float64 `json:"temperature_2m"`
	DewPoint2m               []*float64 `json:"dew_point_2m"`
	WindSpeed10m             []*float64 `json:"wind_speed_10m"`
	WindDirection10m         []*float64 `json:"wind_direction_10m"`
	Cape                     []*float64 `json:"cape"`
}

type daily struct {
	Time    []string `json:"time"`
	Sunrise []string `json:"sunrise"`
	Sunset  []string `json:"sunset"`
}

type location struct {
	UtcOffsetSeconds int    `json:"utc_offset_seconds"`
	Hourly           hourly `json:"hourly"`
	Daily            daily  `json:"daily"`
}

type airHourly struct {
	Time []string   `json:"time"`
	AOD  []*float64 `json:"aerosol_optical_depth"`
	PM25 []*float64 `json:"pm2_5"`
}

type air struct {
	UtcOffsetSeconds int       `json:"utc_offset_seconds"`
	Hourly           airHourly `json:"hourly"`
}

type bundle struct {
	Locations []location
	Air       air
}

func getRaw(client *http.Client, rawURL string, ajax bool) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequest(http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, err
		}
		if ajax {
			req.Header.Set("X-Requested-With", "XMLHttpRequest")
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("HTTP %d: %.80s", resp.StatusCode, strings.TrimSpace(string(body)))
			continue
		}
		return body, nil
	}
	return nil, lastErr
}

func cacheFile(dir, name string) string { return filepath.Join(dir, name) }

func readOrFetch(client *http.Client, dir, name, rawURL string, ajax, refresh bool) ([]byte, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	path := cacheFile(dir, name)
	if !refresh {
		if b, err := os.ReadFile(path); err == nil && len(b) > 0 {
			return b, nil
		}
	}
	body, err := getRaw(client, rawURL, ajax)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return nil, err
	}
	return body, nil
}

// fetchLabels 枚举 sunsetbot /reanalysis/ 的公开标注。该接口每次最多返回
// 20 条（按日期升序），且实测筛选参数不生效，因此用滑动日期窗口取全集。
func fetchLabels(client *http.Client, dir string, refresh bool) ([]label, error) {
	path := cacheFile(dir, "labels.json")
	if !refresh {
		if b, err := os.ReadFile(path); err == nil {
			var cached []label
			if json.Unmarshal(b, &cached) == nil && len(cached) > 0 {
				return cached, nil
			}
		}
	}

	start, err := time.Parse("2006-01-02", firstDate)
	if err != nil {
		return nil, err
	}
	last, err := time.Parse("2006-01-02", lastDate)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]label)
	for page := 0; page < maxLabelPages && !start.After(last); page++ {
		q := url.Values{}
		q.Set("start_date", start.Format("2006-01-02"))
		q.Set("end_date", last.Format("2006-01-02"))
		q.Set("event", "0")
		q.Set("city_name", "上海")
		q.Set("intend", "select_event")
		q.Set("cloud", "0")
		q.Set("color", "0")
		q.Set("quality", "0")
		q.Set("sky", "0")
		q.Set("url", "0")
		body, err := getRaw(client, "https://sunsetbot.top/reanalysis/?"+q.Encode(), true)
		if err != nil {
			return nil, fmt.Errorf("第 %d 页: %w", page+1, err)
		}
		var p labelPage
		if err := json.Unmarshal(body, &p); err != nil {
			return nil, fmt.Errorf("第 %d 页解析失败: %w", page+1, err)
		}
		if p.Status != "ok" || len(p.TableContent) == 0 {
			break
		}
		lastSeen := start
		for _, row := range p.TableContent {
			if len(row) < 11 {
				continue
			}
			l := label{
				Date: row[0], Event: row[1], Cloud: row[2], Color: row[3],
				Actual: row[4], Predicted: row[5], Consistent: row[6],
				Phenomena: row[7],
			}
			if len(row[8]) >= 4 && strings.Contains(row[8], "http") {
				l.CaseURL = row[8]
			}
			l.MapURL = row[9]
			l.SectionURL = row[10]
			if _, ok := labelLevel[l.Actual]; !ok {
				continue
			}
			if _, ok := labelLevel[l.Predicted]; !ok {
				continue
			}
			seen[l.Date+"|"+l.Event] = l
			if d, err := time.Parse("2006-01-02", l.Date); err == nil && d.After(lastSeen) {
				lastSeen = d
			}
		}
		if !lastSeen.After(start) {
			break
		}
		start = lastSeen.AddDate(0, 0, 1)
	}
	if len(seen) == 0 {
		return nil, fmt.Errorf("没有抓到任何标注")
	}
	out := make([]label, 0, len(seen))
	for _, l := range seen {
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Date != out[j].Date {
			return out[i].Date < out[j].Date
		}
		return out[i].Event < out[j].Event
	})
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return nil, err
	}
	return out, nil
}

func groupEvents(labels []label) map[string]*batch {
	groups := make(map[string]*batch)
	for _, l := range labels {
		d, err := time.Parse("2006-01-02", l.Date)
		if err != nil {
			continue
		}
		morning := l.Event == "日出"
		if !morning && l.Event != "日落" {
			continue
		}
		year, week := d.ISOWeek()
		kind := "set"
		if morning {
			kind = "rise"
		}
		key := fmt.Sprintf("%04d-W%02d-%s", year, week, kind)
		b := groups[key]
		if b == nil {
			b = &batch{Key: key, Start: l.Date, End: l.Date}
			groups[key] = b
		}
		if l.Date < b.Start {
			b.Start = l.Date
		}
		if l.Date > b.End {
			b.End = l.Date
		}
		b.Events = append(b.Events, &event{Label: l, Date: d, Morning: morning})
	}
	for _, b := range groups {
		start, _ := time.Parse("2006-01-02", b.Start)
		end, _ := time.Parse("2006-01-02", b.End)
		mid := start.Add(end.Sub(start) / 2)
		doy := mid.YearDay()
		if strings.HasSuffix(b.Key, "-rise") {
			b.Bearing = glow.SunriseBearing(cityLat, doy)
		} else {
			b.Bearing = glow.SunsetBearing(cityLat, doy)
		}
		for _, e := range b.Events {
			doy := e.Date.YearDay()
			if e.Morning {
				e.Bearing = glow.SunriseBearing(cityLat, doy)
			} else {
				e.Bearing = glow.SunsetBearing(cityLat, doy)
			}
		}
	}
	return groups
}

func coordLists(bearing float64) (string, string) {
	lats := []string{strconv.FormatFloat(cityLat, 'f', 4, 64)}
	lons := []string{strconv.FormatFloat(cityLon, 'f', 4, 64)}
	for _, dist := range upstreamKM {
		lat, lon := glow.Destination(cityLat, cityLon, bearing, dist)
		lats = append(lats, strconv.FormatFloat(lat, 'f', 4, 64))
		lons = append(lons, strconv.FormatFloat(lon, 'f', 4, 64))
	}
	return strings.Join(lats, ","), strings.Join(lons, ",")
}

func fetchBundle(client *http.Client, dir string, b *batch, refresh bool) (bundle, error) {
	lats, lons := coordLists(b.Bearing)
	q := url.Values{}
	q.Set("latitude", lats)
	q.Set("longitude", lons)
	q.Set("start_date", b.Start)
	q.Set("end_date", b.End)
	q.Set("hourly", "cloud_cover,cloud_cover_low,cloud_cover_mid,cloud_cover_high,precipitation,precipitation_probability,visibility,temperature_2m,dew_point_2m,wind_speed_10m,wind_direction_10m,cape")
	q.Set("daily", "sunrise,sunset")
	q.Set("timezone", "Asia/Shanghai")
	historyURL := "https://historical-forecast-api.open-meteo.com/v1/forecast?" + q.Encode()
	histBody, err := readOrFetch(client, filepath.Join(dir, "history"), b.Key+".json", historyURL, false, refresh)
	if err != nil {
		return bundle{}, err
	}
	locs, err := decodeLocations(histBody)
	if err != nil {
		return bundle{}, err
	}

	aqQ := url.Values{}
	aqQ.Set("latitude", strconv.FormatFloat(cityLat, 'f', 4, 64))
	aqQ.Set("longitude", strconv.FormatFloat(cityLon, 'f', 4, 64))
	aqQ.Set("start_date", b.Start)
	aqQ.Set("end_date", b.End)
	aqQ.Set("hourly", "aerosol_optical_depth,pm2_5")
	aqQ.Set("timezone", "Asia/Shanghai")
	aqURL := "https://air-quality-api.open-meteo.com/v1/air-quality?" + aqQ.Encode()
	aqBody, err := readOrFetch(client, filepath.Join(dir, "air"), b.Key+".json", aqURL, false, refresh)
	if err != nil {
		return bundle{}, err
	}
	var airData air
	if err := json.Unmarshal(aqBody, &airData); err != nil {
		return bundle{}, err
	}
	var probe struct {
		Error  bool   `json:"error"`
		Reason string `json:"reason"`
	}
	_ = json.Unmarshal(aqBody, &probe)
	if probe.Error {
		return bundle{}, fmt.Errorf("Open-Meteo AQ: %s", probe.Reason)
	}
	return bundle{Locations: locs, Air: airData}, nil
}

func decodeLocations(body []byte) ([]location, error) {
	var probe struct {
		Error  bool   `json:"error"`
		Reason string `json:"reason"`
	}
	_ = json.Unmarshal(body, &probe)
	if probe.Error {
		return nil, fmt.Errorf("Open-Meteo: %s", probe.Reason)
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("Open-Meteo 返回空响应")
	}
	if trimmed[0] == '[' {
		var locs []location
		if err := json.Unmarshal(trimmed, &locs); err != nil {
			return nil, err
		}
		return locs, nil
	}
	var loc location
	if err := json.Unmarshal(trimmed, &loc); err != nil {
		return nil, err
	}
	return []location{loc}, nil
}
