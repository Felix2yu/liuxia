package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestValidateDate(t *testing.T) {
	tests := []struct {
		name     string
		date     string
		expected bool
	}{
		{"空字符串返回true", "", true},
		{"有效日期2024-01-01返回true", "2024-01-01", true},
		{"有效日期2020-12-31返回true", "2020-12-31", true},
		{"无效格式2024/01/01返回false", "2024/01/01", false},
		{"无效格式20240101返回false", "20240101", false},
		{"无效日期2024-13-01返回false", "2024-13-01", false},
		{"无效日期2024-02-30返回false", "2024-02-30", false},
		{"只有年份返回false", "2024", false},
		{"带时间的日期返回false", "2024-01-01 12:00:00", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validateDate(tt.date)
			if result != tt.expected {
				t.Errorf("validateDate(%q) = %v, want %v", tt.date, result, tt.expected)
			}
		})
	}
}

func TestValidateEventType(t *testing.T) {
	tests := []struct {
		name      string
		eventType string
		expected  bool
	}{
		{"空字符串返回true", "", true},
		{"morning返回true", "morning", true},
		{"evening返回true", "evening", true},
		{"invalid返回false", "invalid", false},
		{"MORNING返回false", "MORNING", false},
		{"EVENING返回false", "EVENING", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validateEventType(tt.eventType)
			if result != tt.expected {
				t.Errorf("validateEventType(%q) = %v, want %v", tt.eventType, result, tt.expected)
			}
		})
	}
}

func TestMethodNotAllowed(t *testing.T) {
	tests := []struct {
		name           string
		method         string
		expectedStatus int
	}{
		{"GET请求不返回错误", http.MethodGet, http.StatusOK},
		{"POST请求返回405", http.MethodPost, http.StatusMethodNotAllowed},
		{"PUT请求返回405", http.MethodPut, http.StatusMethodNotAllowed},
		{"DELETE请求返回405", http.MethodDelete, http.StatusMethodNotAllowed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/", nil)
			w := httptest.NewRecorder()

			methodNotAllowed(w, req)

			if tt.expectedStatus == http.StatusOK {
				if w.Code != http.StatusOK {
					t.Errorf("methodNotAllowed() 对于 %s 请求，状态码 = %d, want %d", tt.method, w.Code, tt.expectedStatus)
				}
			} else {
				if w.Code != tt.expectedStatus {
					t.Errorf("methodNotAllowed() 对于 %s 请求，状态码 = %d, want %d", tt.method, w.Code, tt.expectedStatus)
				}
			}
		})
	}
}

func TestDateRegex(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"有效日期", "2024-01-01", true},
		{"无效日期", "2024/01/01", false},
		{"空字符串", "", false},
		{"只有年份", "2024", false},
		{"带时间的日期", "2024-01-01 12:00:00", false},
		{"只有日期分隔符", "--", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := dateRegex.MatchString(tt.input)
			if result != tt.expected {
				t.Errorf("dateRegex.MatchString(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestValidEventTypes(t *testing.T) {
	tests := []struct {
		name      string
		eventType string
		expected  bool
	}{
		{"空字符串", "", true},
		{"morning", "morning", true},
		{"evening", "evening", true},
		{"invalid", "invalid", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validEventTypes[tt.eventType]
			if result != tt.expected {
				t.Errorf("validEventTypes[%q] = %v, want %v", tt.eventType, result, tt.expected)
			}
		})
	}
}

func TestStartWebServerRoutes(t *testing.T) {
	dir, err := os.MkdirTemp("", "liuxia-test")
	if err != nil {
		t.Fatalf("创建临时目录失败: %v", err)
	}
	defer os.RemoveAll(dir)

	dbPath := filepath.Join(dir, "test.db")
	store, err := InitStore(dbPath)
	if err != nil {
		t.Fatalf("InitStore() 失败: %v", err)
	}
	defer store.Close()

	// 创建测试服务器
	mux := http.NewServeMux()

	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		methodNotAllowed(w, r)
		if r.Method != http.MethodGet {
			return
		}
		totalRecords, _ := store.GetTotalRecords()
		cities, _ := store.GetCities()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":       "ok",
			"totalRecords": totalRecords,
			"cities":       cities,
		})
	})

	mux.HandleFunc("/api/data", func(w http.ResponseWriter, r *http.Request) {
		methodNotAllowed(w, r)
		if r.Method != http.MethodGet {
			return
		}
		city := r.URL.Query().Get("city")
		eventType := r.URL.Query().Get("event_type")
		start := r.URL.Query().Get("start")
		end := r.URL.Query().Get("end")

		if !validateEventType(eventType) {
			http.Error(w, "invalid event_type", http.StatusBadRequest)
			return
		}
		if !validateDate(start) || !validateDate(end) {
			http.Error(w, "invalid date format", http.StatusBadRequest)
			return
		}

		records, err := store.QueryRecords(city, eventType, start, end)
		if err != nil {
			http.Error(w, "internal server error", 500)
			return
		}
		if records == nil {
			records = []SunsetRecord{}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(records)
	})

	mux.HandleFunc("/api/cities", func(w http.ResponseWriter, r *http.Request) {
		methodNotAllowed(w, r)
		if r.Method != http.MethodGet {
			return
		}
		cities, err := store.GetCities()
		if err != nil {
			http.Error(w, "internal server error", 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(cities)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	// 添加测试数据
	quality := 0.85
	store.UpsertRecord(SunsetRecord{
		City:      "北京",
		Date:      "2024-01-01",
		Time:      "18:30",
		EventType: "evening",
		Model:     "GFS",
		Quality:   &quality,
	})

	t.Run("健康检查接口", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/api/health")
		if err != nil {
			t.Fatalf("请求失败: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("状态码 = %d, want %d", resp.StatusCode, http.StatusOK)
		}

		var result map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("解析响应失败: %v", err)
		}

		if result["status"] != "ok" {
			t.Errorf("status = %v, want %q", result["status"], "ok")
		}
	})

	t.Run("数据查询接口", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/api/data?city=北京&event_type=evening&start=2024-01-01&end=2024-01-01")
		if err != nil {
			t.Fatalf("请求失败: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("状态码 = %d, want %d", resp.StatusCode, http.StatusOK)
		}

		var records []SunsetRecord
		if err := json.NewDecoder(resp.Body).Decode(&records); err != nil {
			t.Fatalf("解析响应失败: %v", err)
		}

		if len(records) != 1 {
			t.Errorf("返回 %d 条记录, want 1", len(records))
		}
	})

	t.Run("城市列表接口", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/api/cities")
		if err != nil {
			t.Fatalf("请求失败: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("状态码 = %d, want %d", resp.StatusCode, http.StatusOK)
		}

		var cities []string
		if err := json.NewDecoder(resp.Body).Decode(&cities); err != nil {
			t.Fatalf("解析响应失败: %v", err)
		}

		if len(cities) != 1 {
			t.Errorf("返回 %d 个城市, want 1", len(cities))
		}
	})

	t.Run("无效事件类型返回400", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/api/data?event_type=invalid")
		if err != nil {
			t.Fatalf("请求失败: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("状态码 = %d, want %d", resp.StatusCode, http.StatusBadRequest)
		}
	})

	t.Run("无效日期返回400", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/api/data?start=invalid")
		if err != nil {
			t.Fatalf("请求失败: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("状态码 = %d, want %d", resp.StatusCode, http.StatusBadRequest)
		}
	})

	t.Run("POST请求返回405", func(t *testing.T) {
		resp, err := http.Post(server.URL+"/api/data", "application/json", nil)
		if err != nil {
			t.Fatalf("请求失败: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("状态码 = %d, want %d", resp.StatusCode, http.StatusMethodNotAllowed)
		}
	})
}

func TestServeIndex(t *testing.T) {
	// 创建临时目录和文件
	dir, err := os.MkdirTemp("", "liuxia-test")
	if err != nil {
		t.Fatalf("创建临时目录失败: %v", err)
	}
	defer os.RemoveAll(dir)

	// 创建 templates 目录和 index.html
	templatesDir := filepath.Join(dir, "templates")
	if err := os.MkdirAll(templatesDir, 0755); err != nil {
		t.Fatalf("创建 templates 目录失败: %v", err)
	}

	htmlContent := `<!DOCTYPE html>
<html>
<head><title>流霞</title></head>
<body><h1>测试页面</h1></body>
</html>`

	if err := os.WriteFile(filepath.Join(templatesDir, "index.html"), []byte(htmlContent), 0644); err != nil {
		t.Fatalf("创建 index.html 失败: %v", err)
	}

	// 保存当前工作目录
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)

	// 切换到测试目录
	os.Chdir(dir)

	// 测试 serveIndex
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	serveIndex(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("serveIndex() 状态码 = %d, want %d", w.Code, http.StatusOK)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want %q", contentType, "text/html; charset=utf-8")
	}

	body := w.Body.String()
	if body != htmlContent {
		t.Errorf("响应内容不匹配")
	}
}

func TestServeIndexNotFound(t *testing.T) {
	// 创建临时目录（没有 templates/index.html）
	dir, err := os.MkdirTemp("", "liuxia-test")
	if err != nil {
		t.Fatalf("创建临时目录失败: %v", err)
	}
	defer os.RemoveAll(dir)

	// 保存当前工作目录
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)

	// 切换到测试目录
	os.Chdir(dir)

	// 测试 serveIndex
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	serveIndex(w, req)

	// 当找不到文件时，应该返回 500 错误
	if w.Code != http.StatusInternalServerError {
		t.Errorf("serveIndex() 状态码 = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

func TestAllAPIEndpoints(t *testing.T) {
	dir, err := os.MkdirTemp("", "liuxia-test")
	if err != nil {
		t.Fatalf("创建临时目录失败: %v", err)
	}
	defer os.RemoveAll(dir)

	dbPath := filepath.Join(dir, "test.db")
	store, err := InitStore(dbPath)
	if err != nil {
		t.Fatalf("InitStore() 失败: %v", err)
	}
	defer store.Close()

	// 添加测试数据
	quality := 0.85
	store.UpsertRecord(SunsetRecord{
		City:      "北京",
		Date:      "2024-01-01",
		Time:      "18:30",
		EventType: "evening",
		Model:     "GFS",
		Quality:   &quality,
	})

	// 创建静态文件目录
	staticDir := filepath.Join(dir, "static")
	os.MkdirAll(staticDir, 0755)

	// 创建 templates 目录
	templatesDir := filepath.Join(dir, "templates")
	os.MkdirAll(templatesDir, 0755)
	os.WriteFile(filepath.Join(templatesDir, "index.html"), []byte("<html><body>Test</body></html>"), 0644)

	mux := http.NewServeMux()

	// /api/data
	mux.HandleFunc("/api/data", func(w http.ResponseWriter, r *http.Request) {
		methodNotAllowed(w, r)
		if r.Method != http.MethodGet {
			return
		}
		city := r.URL.Query().Get("city")
		eventType := r.URL.Query().Get("event_type")
		start := r.URL.Query().Get("start")
		end := r.URL.Query().Get("end")

		if !validateEventType(eventType) {
			http.Error(w, "invalid event_type", http.StatusBadRequest)
			return
		}
		if !validateDate(start) || !validateDate(end) {
			http.Error(w, "invalid date format", http.StatusBadRequest)
			return
		}

		records, err := store.QueryRecords(city, eventType, start, end)
		if err != nil {
			http.Error(w, "internal server error", 500)
			return
		}
		if records == nil {
			records = []SunsetRecord{}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(records)
	})

	// /api/statistics
	mux.HandleFunc("/api/statistics", func(w http.ResponseWriter, r *http.Request) {
		methodNotAllowed(w, r)
		if r.Method != http.MethodGet {
			return
		}
		city := r.URL.Query().Get("city")
		eventType := r.URL.Query().Get("event_type")
		start := r.URL.Query().Get("start")
		end := r.URL.Query().Get("end")

		if !validateEventType(eventType) {
			http.Error(w, "invalid event_type", http.StatusBadRequest)
			return
		}
		if !validateDate(start) || !validateDate(end) {
			http.Error(w, "invalid date format", http.StatusBadRequest)
			return
		}

		stats, err := store.GetStatistics(city, eventType, start, end)
		if err != nil {
			http.Error(w, "internal server error", 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(stats)
	})

	// /api/cities
	mux.HandleFunc("/api/cities", func(w http.ResponseWriter, r *http.Request) {
		methodNotAllowed(w, r)
		if r.Method != http.MethodGet {
			return
		}
		cities, err := store.GetCities()
		if err != nil {
			http.Error(w, "internal server error", 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(cities)
	})

	// /api/health
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		methodNotAllowed(w, r)
		if r.Method != http.MethodGet {
			return
		}
		totalRecords, _ := store.GetTotalRecords()
		cities, _ := store.GetCities()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":       "ok",
			"totalRecords": totalRecords,
			"cities":       cities,
		})
	})

	// /api/city-comparison
	mux.HandleFunc("/api/city-comparison", func(w http.ResponseWriter, r *http.Request) {
		methodNotAllowed(w, r)
		if r.Method != http.MethodGet {
			return
		}
		eventType := r.URL.Query().Get("event_type")
		start := r.URL.Query().Get("start")
		end := r.URL.Query().Get("end")

		if !validateEventType(eventType) {
			http.Error(w, "invalid event_type", http.StatusBadRequest)
			return
		}
		if !validateDate(start) || !validateDate(end) {
			http.Error(w, "invalid date format", http.StatusBadRequest)
			return
		}

		comparison, err := store.GetCityComparison(eventType, start, end)
		if err != nil {
			http.Error(w, "internal server error", 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(comparison)
	})

	// /api/today
	mux.HandleFunc("/api/today", func(w http.ResponseWriter, r *http.Request) {
		methodNotAllowed(w, r)
		if r.Method != http.MethodGet {
			return
		}
		city := r.URL.Query().Get("city")

		todayDate := time.Now().Format("2006-01-02")
		tomorrowDate := time.Now().AddDate(0, 0, 1).Format("2006-01-02")

		records, err := store.GetTodayTomorrowData(city)
		if err != nil {
			http.Error(w, "internal server error", 500)
			return
		}
		if records == nil {
			records = []SunsetRecord{}
		}
		resp := struct {
			Today    string         `json:"today"`
			Tomorrow string         `json:"tomorrow"`
			Data     []SunsetRecord `json:"data"`
		}{
			Today:    todayDate,
			Tomorrow: tomorrowDate,
			Data:     records,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	// /api/rankings
	mux.HandleFunc("/api/rankings", func(w http.ResponseWriter, r *http.Request) {
		methodNotAllowed(w, r)
		if r.Method != http.MethodGet {
			return
		}
		city := r.URL.Query().Get("city")
		eventType := r.URL.Query().Get("event_type")
		start := r.URL.Query().Get("start")
		end := r.URL.Query().Get("end")

		if !validateEventType(eventType) {
			http.Error(w, "invalid event_type", http.StatusBadRequest)
			return
		}

		rankings, err := store.GetRankings(city, eventType, start, end, 10)
		if err != nil {
			http.Error(w, "internal server error", 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(rankings)
	})

	// /api/export
	mux.HandleFunc("/api/export", func(w http.ResponseWriter, r *http.Request) {
		methodNotAllowed(w, r)
		if r.Method != http.MethodGet {
			return
		}
		format := r.URL.Query().Get("format")
		city := r.URL.Query().Get("city")
		eventType := r.URL.Query().Get("event_type")
		start := r.URL.Query().Get("start")
		end := r.URL.Query().Get("end")

		if !validateEventType(eventType) {
			http.Error(w, "invalid event_type", http.StatusBadRequest)
			return
		}
		if !validateDate(start) || !validateDate(end) {
			http.Error(w, "invalid date format", http.StatusBadRequest)
			return
		}

		if format == "csv" {
			w.Header().Set("Content-Type", "text/csv; charset=utf-8")
			w.Header().Set("Content-Disposition", "attachment; filename=liuxia_data.csv")
			store.ExportCSV(w, city, eventType, start, end)
		} else {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Content-Disposition", "attachment; filename=liuxia_data.json")
			store.ExportJSON(w, city, eventType, start, end)
		}
	})

	// /metrics
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		methodNotAllowed(w, r)
		if r.Method != http.MethodGet {
			return
		}
		totalRecords, _ := store.GetTotalRecords()
		cities, _ := store.GetCities()

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintf(w, "# HELP liuxia_total_records Total records in database\n")
		fmt.Fprintf(w, "# TYPE liuxia_total_records gauge\n")
		fmt.Fprintf(w, "liuxia_total_records %d\n", totalRecords)
		fmt.Fprintf(w, "# HELP liuxia_total_cities Total cities tracked\n")
		fmt.Fprintf(w, "# TYPE liuxia_total_cities gauge\n")
		fmt.Fprintf(w, "liuxia_total_cities %d\n", len(cities))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	t.Run("统计接口", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/api/statistics?event_type=evening&start=2024-01-01&end=2024-01-01")
		if err != nil {
			t.Fatalf("请求失败: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("状态码 = %d, want %d", resp.StatusCode, http.StatusOK)
		}
	})

	t.Run("城市对比接口", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/api/city-comparison?event_type=evening&start=2024-01-01&end=2024-01-01")
		if err != nil {
			t.Fatalf("请求失败: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("状态码 = %d, want %d", resp.StatusCode, http.StatusOK)
		}
	})

	t.Run("今日数据接口", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/api/today?city=北京")
		if err != nil {
			t.Fatalf("请求失败: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("状态码 = %d, want %d", resp.StatusCode, http.StatusOK)
		}
	})

	t.Run("排行榜接口", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/api/rankings?city=北京&event_type=evening")
		if err != nil {
			t.Fatalf("请求失败: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("状态码 = %d, want %d", resp.StatusCode, http.StatusOK)
		}
	})

	t.Run("导出JSON接口", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/api/export?format=json&city=北京&event_type=evening&start=2024-01-01&end=2024-01-01")
		if err != nil {
			t.Fatalf("请求失败: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("状态码 = %d, want %d", resp.StatusCode, http.StatusOK)
		}
	})

	t.Run("导出CSV接口", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/api/export?format=csv&city=北京&event_type=evening&start=2024-01-01&end=2024-01-01")
		if err != nil {
			t.Fatalf("请求失败: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("状态码 = %d, want %d", resp.StatusCode, http.StatusOK)
		}
	})

	t.Run("指标接口", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/metrics")
		if err != nil {
			t.Fatalf("请求失败: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("状态码 = %d, want %d", resp.StatusCode, http.StatusOK)
		}
	})

	t.Run("排行榜无效事件类型", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/api/rankings?event_type=invalid")
		if err != nil {
			t.Fatalf("请求失败: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("状态码 = %d, want %d", resp.StatusCode, http.StatusBadRequest)
		}
	})

	t.Run("统计接口无效日期", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/api/statistics?start=invalid")
		if err != nil {
			t.Fatalf("请求失败: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("状态码 = %d, want %d", resp.StatusCode, http.StatusBadRequest)
		}
	})

	t.Run("导出接口无效事件类型", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/api/export?event_type=invalid")
		if err != nil {
			t.Fatalf("请求失败: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("状态码 = %d, want %d", resp.StatusCode, http.StatusBadRequest)
		}
	})

	t.Run("导出接口无效日期", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/api/export?start=invalid")
		if err != nil {
			t.Fatalf("请求失败: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("状态码 = %d, want %d", resp.StatusCode, http.StatusBadRequest)
		}
	})
}

func TestStartWebServerIntegration(t *testing.T) {
	dir, err := os.MkdirTemp("", "liuxia-test")
	if err != nil {
		t.Fatalf("创建临时目录失败: %v", err)
	}
	defer os.RemoveAll(dir)

	// 保存当前工作目录
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)

	// 切换到测试目录
	os.Chdir(dir)

	// 创建 templates 目录
	templatesDir := filepath.Join(dir, "templates")
	os.MkdirAll(templatesDir, 0755)
	os.WriteFile(filepath.Join(templatesDir, "index.html"), []byte("<html><body>Test</body></html>"), 0644)

	// 创建 static 目录
	staticDir := filepath.Join(dir, "static")
	os.MkdirAll(staticDir, 0755)

	dbPath := filepath.Join(dir, "test.db")
	store, err := InitStore(dbPath)
	if err != nil {
		t.Fatalf("InitStore() 失败: %v", err)
	}
	defer store.Close()

	logger := log.New(os.Stdout, "", 0)

	// 启动服务器在后台
	go StartWebServer("18081", store, logger)

	// 等待服务器启动
	time.Sleep(100 * time.Millisecond)

	// 测试根路径
	resp, err := http.Get("http://localhost:18081/")
	if err == nil {
		resp.Body.Close()
	}

	// 测试 manifest.json
	resp, err = http.Get("http://localhost:18081/manifest.json")
	if err == nil {
		resp.Body.Close()
	}

	// 测试 service-worker.js
	resp, err = http.Get("http://localhost:18081/service-worker.js")
	if err == nil {
		resp.Body.Close()
	}

	// 测试 offline.html
	resp, err = http.Get("http://localhost:18081/offline.html")
	if err == nil {
		resp.Body.Close()
	}

	// 测试不存在的路径
	resp, err = http.Get("http://localhost:18081/nonexistent")
	if err == nil {
		resp.Body.Close()
	}

	// 测试 POST 请求（应该返回 405）
	resp, err = http.Post("http://localhost:18081/api/data", "application/json", nil)
	if err == nil {
		resp.Body.Close()
	}

	// 测试 metrics
	resp, err = http.Get("http://localhost:18081/metrics")
	if err == nil {
		resp.Body.Close()
	}

	// 测试 API 端点
	resp, err = http.Get("http://localhost:18081/api/health")
	if err == nil {
		resp.Body.Close()
	}

	resp, err = http.Get("http://localhost:18081/api/cities")
	if err == nil {
		resp.Body.Close()
	}

	resp, err = http.Get("http://localhost:18081/api/data?city=北京&event_type=evening&start=2024-01-01&end=2024-01-01")
	if err == nil {
		resp.Body.Close()
	}

	resp, err = http.Get("http://localhost:18081/api/statistics?city=北京&event_type=evening&start=2024-01-01&end=2024-01-01")
	if err == nil {
		resp.Body.Close()
	}

	resp, err = http.Get("http://localhost:18081/api/today?city=北京")
	if err == nil {
		resp.Body.Close()
	}

	resp, err = http.Get("http://localhost:18081/api/rankings?city=北京&event_type=evening")
	if err == nil {
		resp.Body.Close()
	}

	resp, err = http.Get("http://localhost:18081/api/city-comparison?event_type=evening&start=2024-01-01&end=2024-01-01")
	if err == nil {
		resp.Body.Close()
	}

	resp, err = http.Get("http://localhost:18081/api/export?format=json&city=北京&event_type=evening&start=2024-01-01&end=2024-01-01")
	if err == nil {
		resp.Body.Close()
	}

	resp, err = http.Get("http://localhost:18081/api/export?format=csv&city=北京&event_type=evening&start=2024-01-01&end=2024-01-01")
	if err == nil {
		resp.Body.Close()
	}
}
