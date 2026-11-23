package main

import (
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

type WeatherPredictor struct {
	config   *Config
	client   *http.Client
	logger   *log.Logger
	store    *Store
	notifier Notifier

	geoMu    sync.Mutex
	geoCache map[string]glowCoord
}

type WeatherData struct {
	PushStr     string
	AODNum      *float64
	DateStr     string
	TimeStr     string
	Score       *float64
	Level       int
	Grade       string
	Probability *float64
	Clarity     string
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
		notifier: NewNotifier(&config.Push, logger),
		geoCache: make(map[string]glowCoord),
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
	wp.fetchGlowForCity(city, models, isMorning, now, section)
}

type dateEntry struct {
	model   string
	pushStr string
	aodNum  *float64
	timeStr string
}

func (wp *WeatherPredictor) sendNotification(title, content string, priority int, tags []string) {
	if !wp.config.Push.Enable {
		wp.logger.Println("[推送已关闭]")
		return
	}

	if wp.config.Push.PushURL == "" {
		wp.logger.Println("[推送失败] 未配置通知渠道 (需设置 PUSH_URL)")
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

func renderMarkdownLines(dataByDate map[string][]dateEntry) []string {
	var markdownLines []string
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
	return markdownLines
}

// fetchGlowForCity 是 Open-Meteo + glow 评分路径的城市级入口。
func (wp *WeatherPredictor) fetchGlowForCity(city string, models []string, isMorning bool, now time.Time, section string) {
	dates := []string{now.AddDate(0, 0, 1).Format("2006-01-02")}
	if (isMorning && now.Hour() < 12) || (!isMorning && now.Hour() < 19) {
		dates = append([]string{now.Format("2006-01-02")}, dates...)
	}
	markdownLines, maxPriority, hasData := wp.buildGlowResponse(city, models, isMorning, now, section, dates)

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

func (wp *WeatherPredictor) buildGlowResponse(city string, models []string, isMorning bool, now time.Time, eventType string, dates []string) ([]string, *int, bool) {
	dataByDate := map[string][]dateEntry{}
	var maxPriority *int

	for _, model := range models {
		for _, date := range dates {
			result := wp.fetchGlowData(city, model, date, isMorning, now)
			if result == nil || result.Score == nil {
				continue
			}
			if wp.store != nil && result.DateStr != "" {
				if err := wp.store.UpsertRecord(SunsetRecord{
					City:        city,
					Date:        result.DateStr,
					Time:        result.TimeStr,
					EventType:   eventType,
					Model:       model,
					AOD:         result.AODNum,
					Score:       result.Score,
					Level:       &result.Level,
					Grade:       result.Grade,
					Probability: result.Probability,
					Clarity:     result.Clarity,
				}); err != nil {
					wp.logger.Printf("[存储] 写入霞分记录失败: %v", err)
				}
			}
			// 微烧以下只落库，不推送。
			if *result.Score < 35 {
				wp.logger.Printf("[过滤] 霞分 %.2f 低于 35，跳过通知", *result.Score)
				continue
			}
			priority := result.Level + 1
			if priority < 1 {
				priority = 1
			}
			if priority > 5 {
				priority = 5
			}
			if maxPriority == nil || priority > *maxPriority {
				maxPriority = &priority
			}
			dataByDate[result.DateStr] = append(dataByDate[result.DateStr], dateEntry{
				model:   model,
				pushStr: result.PushStr,
				aodNum:  result.AODNum,
				timeStr: result.TimeStr,
			})
		}
	}

	hasData := len(dataByDate) > 0
	return renderMarkdownLines(dataByDate), maxPriority, hasData
}

var minValidDate = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

func isReasonableDate(dateStr string) bool {
	if dateStr == "" {
		return false
	}
	d, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return false
	}
	return !d.Before(minValidDate)
}
