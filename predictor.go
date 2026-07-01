package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var eventMap = map[string]string{
	"TODAY_MORNING":    "rise_1",
	"TOMORROW_MORNING": "rise_2",
	"TODAY_EVENING":    "set_1",
	"TOMORROW_EVENING": "set_2",
}

var numRe = regexp.MustCompile(`\d+\.\d+`)

type WeatherPredictor struct {
	config   *Config
	client   *http.Client
	logger   *log.Logger
	store    *Store
	notifier Notifier
}

type WeatherData struct {
	PushStr    string
	QualityNum *float64
	AODNum     float64
	DateStr    string
	TimeStr    string
}

type tbResponse struct {
	Quality     string `json:"tb_quality"`
	AOD         string `json:"tb_aod"`
	EventTime   string `json:"tb_event_time"`
}

func NewWeatherPredictor(config *Config, logger *log.Logger, store *Store) *WeatherPredictor {
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	return &WeatherPredictor{
		config:   config,
		client:   client,
		logger:   logger,
		store:    store,
		notifier: NewNotifier(&config.Push, client, logger),
	}
}

func (wp *WeatherPredictor) buildURL(event, model, city string) string {
	base := wp.config.Request.BaseURL
	params := url.Values{}
	params.Set("query_id", fmt.Sprintf("%d", rand.Intn(900000)+100000))
	params.Set("event", event)
	params.Set("model", model)
	params.Set("query_city", city)
	params.Set("intend", "select_city")
	params.Set("event_date", "None")
	params.Set("times", "None")
	return fmt.Sprintf("%s?%s", strings.TrimRight(base, "/"), params.Encode())
}

func calculatePriority(qualityNum float64) int {
	switch {
	case qualityNum < 0.4:
		return 1
	case qualityNum < 0.6:
		return 2
	case qualityNum < 0.8:
		return 3
	case qualityNum < 1.0:
		return 4
	default:
		return 5
	}
}

func (wp *WeatherPredictor) parseWeatherData(content string) *WeatherData {
	var jsonContent tbResponse
	if err := json.Unmarshal([]byte(content), &jsonContent); err != nil {
		wp.logger.Printf("JSON解析失败: %v, 内容: %.100s...", err, content)
		return nil
	}

	qualityStr := jsonContent.Quality
	qualityMatch := numRe.FindString(qualityStr)
	var qualityNum *float64
	if qualityMatch == "" {
		wp.logger.Printf("质量数据无法解析数值: %s，记录为空值", qualityStr)
	} else {
		if v, err := strconv.ParseFloat(qualityMatch, 64); err == nil {
			qualityNum = &v
		}
	}

	aodStr := jsonContent.AOD
	if aodStr == "" {
		aodStr = "N/A"
	}
	aodMatch := numRe.FindString(aodStr)
	var aodNum *float64
	if aodMatch != "" {
		if v, err := strconv.ParseFloat(aodMatch, 64); err == nil {
			aodNum = &v
		}
	}

	eventTime := jsonContent.EventTime
	var dateStr, timeStr string
	if len(eventTime) >= 10 {
		dateStr = eventTime[:10]
	}
	if len(eventTime) >= 11 {
		timeStr = eventTime[11:]
	}

	var pushStr strings.Builder
	if qualityNum == nil {
		pushStr.WriteString(fmt.Sprintf("鲜艳度：%s（数据异常）\n", qualityStr))
	} else if *qualityNum >= 0.4 {
		pushStr.WriteString(fmt.Sprintf("鲜艳度：**%s**\n", qualityStr))
	} else {
		pushStr.WriteString(fmt.Sprintf("鲜艳度：%s\n", qualityStr))
	}

	if aodNum != nil && *aodNum <= 0.4 {
		pushStr.WriteString(fmt.Sprintf("气溶胶：**%s**\n", aodStr))
	} else {
		pushStr.WriteString(fmt.Sprintf("气溶胶：%s\n", aodStr))
	}

	return &WeatherData{
		PushStr:    pushStr.String(),
		QualityNum: qualityNum,
		AODNum:     derefFloat(aodNum),
		DateStr:    dateStr,
		TimeStr:    timeStr,
	}
}

func (wp *WeatherPredictor) errorResult(msg string) *WeatherData {
	if wp.config.Schedule.PushError {
		return &WeatherData{
			PushStr: fmt.Sprintf("[失败] %s\n", msg),
		}
	}
	return nil
}

func (wp *WeatherPredictor) fetchSingleData(fetchURL string) *WeatherData {
	req, err := http.NewRequest("GET", fetchURL, nil)
	if err != nil {
		wp.logger.Printf("构建请求失败: %s", fetchURL)
		return wp.errorResult(fmt.Sprintf("请求错误: %.100s", err.Error()))
	}

	resp, err := wp.client.Do(req)
	if err != nil {
		wp.logger.Printf("请求失败: %s, 错误: %v", fetchURL, err)
		return wp.errorResult(fmt.Sprintf("请求错误: %.100s", err.Error()))
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		wp.logger.Printf("请求返回错误状态: %s -> %d", fetchURL, resp.StatusCode)
		return wp.errorResult(fmt.Sprintf("请求错误: HTTP %d", resp.StatusCode))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		wp.logger.Printf("读取响应失败: %v", err)
		return wp.errorResult(fmt.Sprintf("请求错误: %.100s", err.Error()))
	}

	wp.logger.Printf("请求成功: %s", fetchURL)
	return wp.parseWeatherData(string(body))
}

func (wp *WeatherPredictor) FetchData(isMorning bool) {
	now := time.Now()
	taskName := "朝霞"
	if !isMorning {
		taskName = "晚霞"
	}
	wp.logger.Printf("[任务执行] %s任务开始执行，当前时间: %s", taskName, now.Format("2006-01-02 15:04:05"))

	section := "morning"
	if !isMorning {
		section = "evening"
	}
	var models []string
	if section == "morning" {
		models = wp.config.Schedule.Morning.Model
	} else {
		models = wp.config.Schedule.Evening.Model
	}
	if len(models) == 0 {
		models = []string{"GFS"}
	}

	cities := wp.config.Schedule.Cities
	if len(cities) == 0 {
		cities = []string{wp.config.Schedule.City}
	}

	for _, city := range cities {
		wp.fetchDataForCity(city, models, isMorning, now, section)
	}
}

func (wp *WeatherPredictor) fetchDataForCity(city string, models []string, isMorning bool, now time.Time, section string) {
	urls := map[string]string{}
	eventPrefix := "MORNING"
	if !isMorning {
		eventPrefix = "EVENING"
	}

	for _, model := range models {
		urlTomorrow := wp.buildURL(eventMap["TOMORROW_"+eventPrefix], model, city)
		urls[urlTomorrow] = model

		if isMorning {
			if now.Hour() < 12 {
				urlToday := wp.buildURL(eventMap["TODAY_"+eventPrefix], model, city)
				urls[urlToday] = model
			}
		} else {
			if now.Hour() < 19 {
				urlToday := wp.buildURL(eventMap["TODAY_"+eventPrefix], model, city)
				urls[urlToday] = model
			}
		}
	}

	urlList := make([]string, 0, len(urls))
	for u := range urls {
		urlList = append(urlList, u)
	}
	wp.logger.Printf("[URL构建] 城市 %s 构建了 %d 个请求URL: %v", city, len(urls), urlList)

	displayCity := city
	if idx := strings.LastIndex(city, "-"); idx >= 0 {
		displayCity = city[idx+1:]
	}

	eventTitle := fmt.Sprintf("%s朝霞预报", displayCity)
	eventTag := "sunrise"
	if !isMorning {
		eventTitle = fmt.Sprintf("%s晚霞预报", displayCity)
		eventTag = "city_sunset"
	}

	markdownLines, maxPriority, hasData := wp.buildMarkdownResponse(city, urls, section)

	if hasData {
		pushContent := strings.Join(markdownLines, "\n")
		if maxPriority == nil {
			p := 3
			maxPriority = &p
		}
		wp.sendNotification(eventTitle, pushContent, *maxPriority, []string{eventTag})
	} else {
		wp.logger.Printf("[推送] 城市 %s 没有符合条件的数据", city)
	}
}

type dateEntry struct {
	model      string
	pushStr    string
	qualityNum *float64
	aodNum     float64
	timeStr    string
}

func (wp *WeatherPredictor) sendNotification(title, content string, priority int, tags []string) {
	if !wp.config.Push.Enable {
		wp.logger.Println("[推送已关闭]")
		return
	}

	if wp.config.Push.AppriseURL == "" && wp.config.Push.NtfyTopic == "" {
		wp.logger.Println("[推送失败] 未配置通知渠道 (需设置 APPRISE_URL 或 NTFY_TOPIC)")
		return
	}

	body := content
	markdown := wp.config.Push.Markdown
	if !markdown {
		body = stripMarkdown(content)
	}

	if err := wp.notifier.Send(title, body, priority, tags, markdown); err != nil {
		wp.logger.Printf("[推送失败] %v", err)
	}
}

func (wp *WeatherPredictor) buildMarkdownResponse(city string, urls map[string]string, eventType string) ([]string, *int, bool) {
	dataByDate := map[string][]dateEntry{}
	var maxPriority *int

	urlList := make([]string, 0, len(urls))
	for u := range urls {
		urlList = append(urlList, u)
	}
	sort.Strings(urlList)

	for _, u := range urlList {
		model := urls[u]
		result := wp.fetchSingleData(u)
		if result == nil {
			continue
		}

		if wp.store != nil && result.DateStr != "" {
			wp.store.UpsertRecord(SunsetRecord{
				City:      city,
				Date:      result.DateStr,
				Time:      result.TimeStr,
				EventType: eventType,
				Model:     model,
				Quality:   result.QualityNum,
				AOD:       floatPtr(result.AODNum),
			})
		}

		if result.QualityNum == nil || *result.QualityNum < 0.2 {
			if result.QualityNum == nil {
				wp.logger.Printf("[过滤] 质量数据为空，跳过通知")
			} else {
				wp.logger.Printf("[过滤] 质量 %.2f 低于 0.2，跳过通知", *result.QualityNum)
			}
			continue
		}

		priority := calculatePriority(*result.QualityNum)
		if maxPriority == nil || priority > *maxPriority {
			maxPriority = &priority
		}

		dataByDate[result.DateStr] = append(dataByDate[result.DateStr], dateEntry{
			model:      model,
			pushStr:    result.PushStr,
			qualityNum: result.QualityNum,
			aodNum:     result.AODNum,
			timeStr:    result.TimeStr,
		})
	}

	var markdownLines []string
	hasData := len(dataByDate) > 0

	dateKeys := make([]string, 0, len(dataByDate))
	for k := range dataByDate {
		dateKeys = append(dateKeys, k)
	}
	sort.Strings(dateKeys)

	for dateIdx, dateStr := range dateKeys {
		if dateIdx > 0 {
			markdownLines = append(markdownLines, "")
		}
		markdownLines = append(markdownLines, fmt.Sprintf("## 日期：%s", dateStr))

		if len(dataByDate[dateStr]) > 0 {
			firstTime := dataByDate[dateStr][0].timeStr
			if firstTime != "" {
				markdownLines = append(markdownLines, fmt.Sprintf("时间：%s", firstTime))
			}
		}
		markdownLines = append(markdownLines, "")

		for _, entry := range dataByDate[dateStr] {
			markdownLines = append(markdownLines, fmt.Sprintf("### %s模型", entry.model))
			for _, line := range strings.Split(strings.TrimSpace(entry.pushStr), "\n") {
				if strings.TrimSpace(line) != "" {
					markdownLines = append(markdownLines, fmt.Sprintf("- %s", line))
				}
			}
			markdownLines = append(markdownLines, "")
		}
	}

	return markdownLines, maxPriority, hasData
}

func derefFloat(f *float64) float64 {
	if f != nil {
		return *f
	}
	return 0
}

func floatPtr(f float64) *float64 {
	return &f
}
