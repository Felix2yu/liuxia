package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"

	"liuxia/internal/glow"
)

const (
	omCloudVars   = "cloud_cover,cloud_cover_low,cloud_cover_mid,cloud_cover_high"
	omWeatherVars = "precipitation,precipitation_probability,visibility,temperature_2m,dew_point_2m,wind_speed_10m,wind_direction_10m,cape"
	omAQVars      = "aerosol_optical_depth,pm2_5"
)

var omUpstreamKM = []float64{50, 100, 200, 300, 400}

type glowCoord struct {
	Lat      float64
	Lon      float64
	Timezone string
}

type omHourly struct {
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

type omDaily struct {
	Time    []string `json:"time"`
	Sunrise []string `json:"sunrise"`
	Sunset  []string `json:"sunset"`
}

type omLocation struct {
	Timezone         string   `json:"timezone"`
	UtcOffsetSeconds int      `json:"utc_offset_seconds"`
	Hourly           omHourly `json:"hourly"`
	Daily            omDaily  `json:"daily"`
}

type omAirQuality struct {
	UtcOffsetSeconds int `json:"utc_offset_seconds"`
	Hourly           struct {
		Time []string   `json:"time"`
		AOD  []*float64 `json:"aerosol_optical_depth"`
		PM25 []*float64 `json:"pm2_5"`
	} `json:"hourly"`
}

type omGeocoding struct {
	Results []struct {
		Name      string  `json:"name"`
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
		Timezone  string  `json:"timezone"`
	} `json:"results"`
}

func (wp *WeatherPredictor) omGetBytes(rawURL string) ([]byte, error) {
	resp, err := wp.client.Get(rawURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %.120s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}

func (wp *WeatherPredictor) omGetJSON(rawURL string, out interface{}) error {
	b, err := wp.omGetBytes(rawURL)
	if err != nil {
		return err
	}
	var probe struct {
		Error  bool   `json:"error"`
		Reason string `json:"reason"`
	}
	_ = json.Unmarshal(b, &probe)
	if probe.Error {
		return fmt.Errorf("Open-Meteo: %s", probe.Reason)
	}
	return json.Unmarshal(b, out)
}

func decodeOmLocations(body []byte) ([]omLocation, error) {
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
		var locs []omLocation
		if err := json.Unmarshal(trimmed, &locs); err != nil {
			return nil, err
		}
		return locs, nil
	}
	var loc omLocation
	if err := json.Unmarshal(trimmed, &loc); err != nil {
		return nil, err
	}
	return []omLocation{loc}, nil
}

func (wp *WeatherPredictor) omBaseURLs() (forecast, aq, geocoding string) {
	forecast = wp.config.Request.ForecastURL
	if forecast == "" {
		forecast = "https://api.open-meteo.com/v1/forecast"
	}
	aq = wp.config.Request.AQURL
	if aq == "" {
		aq = "https://air-quality-api.open-meteo.com/v1/air-quality"
	}
	geocoding = wp.config.Request.GeocodingURL
	if geocoding == "" {
		geocoding = "https://geocoding-api.open-meteo.com/v1/search"
	}
	return
}

// resolveGlowCity 把配置里的中文城市名解析成经纬度与 IANA 时区。
// 名称带 "-xxx" 后缀时（如「北京-朝阳」）依次尝试全名、后缀与前缀。
func (wp *WeatherPredictor) resolveGlowCity(city string) (glowCoord, error) {
	wp.geoMu.Lock()
	if c, ok := wp.geoCache[city]; ok {
		wp.geoMu.Unlock()
		return c, nil
	}
	wp.geoMu.Unlock()

	candidates := []string{city}
	if i := strings.LastIndex(city, "-"); i >= 0 {
		candidates = append(candidates, city[i+1:], city[:i])
	}
	_, _, geocodingURL := wp.omBaseURLs()
	for _, name := range candidates {
		if name == "" {
			continue
		}
		params := url.Values{}
		params.Set("name", name)
		params.Set("count", "1")
		params.Set("language", "zh")
		params.Set("format", "json")
		var out omGeocoding
		if err := wp.omGetJSON(geocodingURL+"?"+params.Encode(), &out); err != nil {
			wp.logger.Printf("[地理编码] %s 失败: %v", name, err)
			continue
		}
		if len(out.Results) == 0 {
			continue
		}
		r := out.Results[0]
		c := glowCoord{Lat: r.Latitude, Lon: r.Longitude, Timezone: r.Timezone}
		wp.geoMu.Lock()
		wp.geoCache[city] = c
		wp.geoMu.Unlock()
		return c, nil
	}
	return glowCoord{}, fmt.Errorf("无法定位城市 %q", city)
}

func openMeteoModelName(model string) string {
	switch strings.ToUpper(model) {
	case "EC", "ECMWF":
		return "ecmwf_ifs025"
	case "GFS":
		return "gfs_seamless"
	case "ICON":
		return "icon_seamless"
	default:
		return "best_match"
	}
}

func omFloatAt(arr []*float64, i int) *float64 {
	if i < 0 || i >= len(arr) {
		return nil
	}
	return arr[i]
}

func omEpochs(times []string, offsetSec int) []int64 {
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

func omDailyIndex(d omDaily, date string) int {
	for i, t := range d.Time {
		if t == date {
			return i
		}
	}
	return -1
}

// fetchGlowData 取一个城市、一个模型、一个日期的单次天文事件评分。
func (wp *WeatherPredictor) fetchGlowData(city, model, targetDate string, isMorning bool, now time.Time) *WeatherData {
	coord, err := wp.resolveGlowCity(city)
	if err != nil {
		wp.logger.Printf("[霞分] %s", err)
		return wp.errorResult(err.Error())
	}

	eventDate, err := time.Parse("2006-01-02", targetDate)
	if err != nil || !isReasonableDate(targetDate) {
		wp.logger.Printf("[霞分] 日期 %q 不合理", targetDate)
		return nil
	}
	bearing := glow.SunsetBearing(coord.Lat, eventDate.YearDay())
	if isMorning {
		bearing = glow.SunriseBearing(coord.Lat, eventDate.YearDay())
	}

	lats := []string{strconv.FormatFloat(coord.Lat, 'f', 4, 64)}
	lons := []string{strconv.FormatFloat(coord.Lon, 'f', 4, 64)}
	for _, dist := range omUpstreamKM {
		lat, lon := glow.Destination(coord.Lat, coord.Lon, bearing, dist)
		lats = append(lats, strconv.FormatFloat(lat, 'f', 4, 64))
		lons = append(lons, strconv.FormatFloat(lon, 'f', 4, 64))
	}

	forecastURL, aqURL, _ := wp.omBaseURLs()
	cloudParams := url.Values{}
	cloudParams.Set("latitude", strings.Join(lats, ","))
	cloudParams.Set("longitude", strings.Join(lons, ","))
	cloudParams.Set("hourly", omCloudVars)
	if models := wp.config.Request.ForecastModels; models != "" {
		cloudParams.Set("models", models)
	}
	cloudParams.Set("daily", "sunrise,sunset")
	cloudParams.Set("timezone", "auto")
	cloudParams.Set("forecast_days", "3")
	cloudParams.Set("past_days", "1")
	cloudBody, err := wp.omGetBytes(forecastURL + "?" + cloudParams.Encode())
	if err != nil {
		wp.logger.Printf("[霞分] 云量请求失败 %s: %v", city, err)
		return wp.errorResult(fmt.Sprintf("云量请求错误: %v", err))
	}
	locs, err := decodeOmLocations(cloudBody)
	if err != nil || len(locs) == 0 {
		wp.logger.Printf("[霞分] 云量解析失败 %s: %v", city, err)
		return wp.errorResult("云量数据解析失败")
	}
	center := locs[0]

	weatherParams := url.Values{}
	weatherParams.Set("latitude", lats[0])
	weatherParams.Set("longitude", lons[0])
	weatherParams.Set("hourly", omWeatherVars)
	weatherParams.Set("models", openMeteoModelName(model))
	weatherParams.Set("timezone", "auto")
	weatherParams.Set("forecast_days", "3")
	weatherParams.Set("past_days", "1")
	var weather omLocation
	if err := wp.omGetJSON(forecastURL+"?"+weatherParams.Encode(), &weather); err != nil {
		wp.logger.Printf("[霞分] 气象要素请求失败 %s/%s: %v", city, model, err)
		return wp.errorResult(fmt.Sprintf("气象要素请求错误: %v", err))
	}

	aqParams := url.Values{}
	aqParams.Set("latitude", lats[0])
	aqParams.Set("longitude", lons[0])
	aqParams.Set("hourly", omAQVars)
	aqParams.Set("timezone", "auto")
	aqParams.Set("forecast_days", "3")
	var air omAirQuality
	if err := wp.omGetJSON(aqURL+"?"+aqParams.Encode(), &air); err != nil {
		wp.logger.Printf("[霞分] 空气质量请求失败 %s: %v", city, err)
	}

	di := omDailyIndex(center.Daily, targetDate)
	if di < 0 {
		wp.logger.Printf("[霞分] %s 无 %s 的日出日落数据", city, targetDate)
		return nil
	}
	eventTime := center.Daily.Sunset[di]
	if isMorning && di < len(center.Daily.Sunrise) {
		eventTime = center.Daily.Sunrise[di]
	}
	if len(eventTime) < 11 {
		wp.logger.Printf("[霞分] %s 事件时间异常: %q", city, eventTime)
		return nil
	}
	eventEpoch, err := glow.ToEpoch(eventTime, center.UtcOffsetSeconds)
	if err != nil {
		wp.logger.Printf("[霞分] 事件时间解析失败: %v", err)
		return nil
	}

	centerEpochs := omEpochs(center.Hourly.Time, center.UtcOffsetSeconds)
	ci := glow.IdxFloor(centerEpochs, eventEpoch)
	if ci+1 >= len(centerEpochs) || ci < 0 {
		wp.logger.Printf("[霞分] %s 逐小时云量未覆盖事件时刻", city)
		return nil
	}

	upstream := make([]glow.UpstreamPoint, 0, len(omUpstreamKM))
	for i := 1; i < len(locs) && i-1 < len(omUpstreamKM); i++ {
		epochs := omEpochs(locs[i].Hourly.Time, locs[i].UtcOffsetSeconds)
		if len(epochs) == 0 {
			continue
		}
		j := glow.IdxNearest(epochs, eventEpoch)
		upstream = append(upstream, glow.UpstreamPoint{
			DistanceKM: omUpstreamKM[i-1],
			Low:        omFloatAt(locs[i].Hourly.CloudCoverLow, j),
			Mid:        omFloatAt(locs[i].Hourly.CloudCoverMid, j),
			Total:      omFloatAt(locs[i].Hourly.CloudCover, j),
		})
	}

	weatherEpochs := omEpochs(weather.Hourly.Time, weather.UtcOffsetSeconds)
	if len(weatherEpochs) == 0 {
		wp.logger.Printf("[霞分] %s/%s 逐小时气象要素为空", city, model)
		return nil
	}
	wi := glow.IdxNearest(weatherEpochs, eventEpoch)
	pre3 := omFloatOrZero(weather.Hourly.Precipitation, wi-1) +
		omFloatOrZero(weather.Hourly.Precipitation, wi-2) +
		omFloatOrZero(weather.Hourly.Precipitation, wi-3)
	pre3V2 := omFloatOrZero(weather.Hourly.Precipitation, wi) +
		omFloatOrZero(weather.Hourly.Precipitation, wi-1) +
		omFloatOrZero(weather.Hourly.Precipitation, wi-2)

	aqIndex := -1
	if len(air.Hourly.Time) > 0 {
		aqEpochs := omEpochs(air.Hourly.Time, air.UtcOffsetSeconds)
		if len(aqEpochs) > 0 {
			aqIndex = glow.IdxNearest(aqEpochs, eventEpoch)
		}
	}

	in := glow.Input{
		Mid:       omFloatAt(center.Hourly.CloudCoverMid, ci),
		High:      omFloatAt(center.Hourly.CloudCoverHigh, ci),
		Low:       omFloatAt(center.Hourly.CloudCoverLow, ci),
		Precip:    omFloatAt(weather.Hourly.Precipitation, wi),
		Prob:      omFloatAt(weather.Hourly.PrecipitationProbability, wi),
		PrecipV2:  omFloatAt(weather.Hourly.Precipitation, wi+1),
		ProbV2:    omFloatAt(weather.Hourly.PrecipitationProbability, wi+1),
		Pre3:      pre3,
		Pre3V2:    pre3V2,
		VisM:      omFloatAt(weather.Hourly.Visibility, wi),
		Temp:      omFloatAt(weather.Hourly.Temperature2m, wi),
		DewPoint:  omFloatAt(weather.Hourly.DewPoint2m, wi),
		WindSpeed: omFloatAt(weather.Hourly.WindSpeed10m, wi),
		WindDir:   omFloatAt(weather.Hourly.WindDirection10m, wi),
		Cape:      omFloatAt(weather.Hourly.Cape, wi),
		AOD:       omFloatAt(air.Hourly.AOD, aqIndex),
		PM25:      omFloatAt(air.Hourly.PM25, aqIndex),
		Upstream:  upstream,
	}
	result := glow.Evaluate(in)
	score := result.RawScore
	prob := result.Probability
	clarity := ""
	if result.Clarity != nil {
		clarity = result.Clarity.Name
	}
	aod := in.AOD
	pushStr := fmt.Sprintf("霞分：**%d**（%s）\n概率：%.0f%%\n通透度：%s\n理由：%s",
		result.Score, result.Grade, result.Probability*100, clarity, strings.Join(result.ReasonsV2, "；"))
	if result.Components.Incomplete {
		pushStr += "\n注意：关键云量数据缺失，评分已封顶"
	}

	wp.logger.Printf("[霞分] %s/%s/%s 得分 %d（%s），原始 %.2f", city, model, targetDate, result.Score, result.Grade, result.RawScore)

	return &WeatherData{
		PushStr:     pushStr,
		AODNum:      aod,
		DateStr:     targetDate,
		TimeStr:     eventTime[11:],
		Score:       &score,
		Level:       result.Level,
		Grade:       result.Grade,
		Probability: &prob,
		Clarity:     clarity,
	}
}

func omFloatOrZero(arr []*float64, i int) float64 {
	v := omFloatAt(arr, i)
	if v == nil {
		return 0
	}
	return *v
}
