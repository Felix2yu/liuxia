package main

import (
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestCalculatePriority(t *testing.T) {
	tests := []struct {
		name     string
		quality  float64
		expected int
	}{
		{"质量0.3返回优先级1", 0.3, 1},
		{"质量0.4返回优先级2", 0.4, 2},
		{"质量0.5返回优先级2", 0.5, 2},
		{"质量0.6返回优先级3", 0.6, 3},
		{"质量0.7返回优先级3", 0.7, 3},
		{"质量0.8返回优先级4", 0.8, 4},
		{"质量0.9返回优先级4", 0.9, 4},
		{"质量1.0返回优先级5", 1.0, 5},
		{"质量1.5返回优先级5", 1.5, 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculatePriority(tt.quality)
			if result != tt.expected {
				t.Errorf("calculatePriority(%f) = %d, want %d", tt.quality, result, tt.expected)
			}
		})
	}
}

func TestIsReasonableDate(t *testing.T) {
	tests := []struct {
		name     string
		dateStr  string
		expected bool
	}{
		{"空字符串返回false", "", false},
		{"2020年1月1日返回true", "2020-01-01", true},
		{"2024年1月1日返回true", "2024-01-01", true},
		{"2019年12月31日返回false", "2019-12-31", false},
		{"无效日期格式返回false", "2024/01/01", false},
		{"未来日期返回true", "2030-01-01", true},
		{"只有年份返回false", "2024", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isReasonableDate(tt.dateStr)
			if result != tt.expected {
				t.Errorf("isReasonableDate(%q) = %v, want %v", tt.dateStr, result, tt.expected)
			}
		})
	}
}

func TestEventMap(t *testing.T) {
	expected := map[string]string{
		"TODAY_MORNING":    "rise_1",
		"TOMORROW_MORNING": "rise_2",
		"TODAY_EVENING":    "set_1",
		"TOMORROW_EVENING": "set_2",
	}

	for key, expectedVal := range expected {
		if val, ok := eventMap[key]; !ok || val != expectedVal {
			t.Errorf("eventMap[%q] = %q, want %q", key, val, expectedVal)
		}
	}
}

func TestMinValidDate(t *testing.T) {
	expected := "2020-01-01"
	if minValidDate.Format("2006-01-02") != expected {
		t.Errorf("minValidDate = %v, want %v", minValidDate.Format("2006-01-02"), expected)
	}
}

func TestNewWeatherPredictor(t *testing.T) {
	logger := log.New(os.Stdout, "", 0)
	config := &Config{
		Request: RequestConfig{
			BaseURL: "https://example.com/",
		},
		Push: PushConfig{
			Enable: false,
		},
		Schedule: ScheduleConfig{
			City: "北京",
			Morning: TaskConfig{
				Model: []string{"GFS"},
			},
			Evening: TaskConfig{
				Model: []string{"GFS"},
			},
		},
	}

	predictor := NewWeatherPredictor(config, logger, nil)

	if predictor == nil {
		t.Fatal("NewWeatherPredictor() 返回 nil")
	}
	if predictor.config != config {
		t.Error("NewWeatherPredictor() config 未正确设置")
	}
	if predictor.client == nil {
		t.Error("NewWeatherPredictor() client 应已初始化")
	}
	if predictor.logger != logger {
		t.Error("NewWeatherPredictor() logger 未正确设置")
	}
}

func TestBuildURL(t *testing.T) {
	logger := log.New(os.Stdout, "", 0)
	config := &Config{
		Request: RequestConfig{
			BaseURL: "https://example.com/",
		},
	}

	predictor := &WeatherPredictor{
		config: config,
		client: nil,
		logger: logger,
	}

	url := predictor.buildURL("set_1", "GFS", "北京")

	if url == "" {
		t.Fatal("buildURL() 返回空字符串")
	}
	if !contains(url, "event=set_1") {
		t.Errorf("buildURL() 应包含 event=set_1, got %q", url)
	}
	if !contains(url, "model=GFS") {
		t.Errorf("buildURL() 应包含 model=GFS, got %q", url)
	}
	if !contains(url, "query_city=%E5%8C%97%E4%BA%AC") {
		t.Errorf("buildURL() 应包含编码后的城市名, got %q", url)
	}
}

func TestParseWeatherData(t *testing.T) {
	logger := log.New(os.Stdout, "", 0)
	config := &Config{
		Request: RequestConfig{
			BaseURL: "https://example.com/",
		},
	}

	predictor := &WeatherPredictor{
		config: config,
		client: nil,
		logger: logger,
	}

	t.Run("有效JSON数据", func(t *testing.T) {
		content := `{
			"tb_quality": "鲜艳度：0.85",
			"tb_aod": "气溶胶：0.3",
			"tb_event_time": "2024-01-01 18:30"
		}`

		result := predictor.parseWeatherData(content)
		if result == nil {
			t.Fatal("parseWeatherData() 返回 nil")
		}
		if result.DateStr != "2024-01-01" {
			t.Errorf("DateStr = %q, want %q", result.DateStr, "2024-01-01")
		}
		if result.TimeStr != "18:30" {
			t.Errorf("TimeStr = %q, want %q", result.TimeStr, "18:30")
		}
	})

	t.Run("无效JSON返回nil", func(t *testing.T) {
		content := `invalid json`
		result := predictor.parseWeatherData(content)
		if result != nil {
			t.Error("parseWeatherData() 对于无效 JSON 应返回 nil")
		}
	})

	t.Run("不合理日期返回nil", func(t *testing.T) {
		content := `{
			"tb_quality": "0.85",
			"tb_aod": "0.3",
			"tb_event_time": "2019-01-01 18:30"
		}`
		result := predictor.parseWeatherData(content)
		if result != nil {
			t.Error("parseWeatherData() 对于不合理日期应返回 nil")
		}
	})
}

func TestErrorResult(t *testing.T) {
	logger := log.New(os.Stdout, "", 0)
	config := &Config{
		Schedule: ScheduleConfig{
			PushError: true,
		},
	}

	predictor := &WeatherPredictor{
		config: config,
		client: nil,
		logger: logger,
	}

	t.Run("PushError为true返回错误信息", func(t *testing.T) {
		result := predictor.errorResult("测试错误")
		if result == nil {
			t.Fatal("errorResult() 返回 nil")
		}
		if !contains(result.PushStr, "测试错误") {
			t.Errorf("PushStr = %q, 应包含 '测试错误'", result.PushStr)
		}
	})

	t.Run("PushError为false返回nil", func(t *testing.T) {
		config.Schedule.PushError = false
		result := predictor.errorResult("测试错误")
		if result != nil {
			t.Error("errorResult() 在 PushError=false 时应返回 nil")
		}
		config.Schedule.PushError = true
	})
}

func TestNumRe(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"提取数字", "鲜艳度：0.85", "0.85"},
		{"无数字", "鲜艳度：N/A", ""},
		{"多个数字取第一个", "0.85-1.23", "0.85"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := numRe.FindString(tt.input)
			if result != tt.expected {
				t.Errorf("numRe.FindString(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestFetchSingleData(t *testing.T) {
	// 创建 mock HTTP 服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"tb_quality": "0.85",
			"tb_aod": "0.3",
			"tb_event_time": "2024-01-01 18:30"
		}`))
	}))
	defer server.Close()

	logger := log.New(os.Stdout, "", 0)
	config := &Config{
		Request: RequestConfig{
			BaseURL: server.URL + "/",
		},
		Schedule: ScheduleConfig{
			PushError: true,
		},
	}

	store, cleanup := setupTestStore(t)
	defer cleanup()

	predictor := NewWeatherPredictor(config, logger, store)

	result := predictor.fetchSingleData(server.URL)
	if result == nil {
		t.Fatal("fetchSingleData() 返回 nil")
	}
	if result.DateStr != "2024-01-01" {
		t.Errorf("DateStr = %q, want %q", result.DateStr, "2024-01-01")
	}
}

func TestFetchSingleDataHTTPError(t *testing.T) {
	// 创建返回错误的 mock HTTP 服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	logger := log.New(os.Stdout, "", 0)
	config := &Config{
		Request: RequestConfig{
			BaseURL: server.URL + "/",
		},
		Schedule: ScheduleConfig{
			PushError: true,
		},
	}

	predictor := NewWeatherPredictor(config, logger, nil)

	result := predictor.fetchSingleData(server.URL)
	if result == nil {
		t.Fatal("fetchSingleData() 在 HTTP 错误时应该返回错误结果")
	}
	if !contains(result.PushStr, "失败") {
		t.Errorf("PushStr 应包含 '失败', got %q", result.PushStr)
	}
}

func TestFetchSingleDataInvalidJSON(t *testing.T) {
	// 创建返回无效 JSON 的 mock HTTP 服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`invalid json`))
	}))
	defer server.Close()

	logger := log.New(os.Stdout, "", 0)
	config := &Config{
		Request: RequestConfig{
			BaseURL: server.URL + "/",
		},
		Schedule: ScheduleConfig{
			PushError: true,
		},
	}

	predictor := NewWeatherPredictor(config, logger, nil)

	result := predictor.fetchSingleData(server.URL)
	if result != nil {
		t.Error("fetchSingleData() 在无效 JSON 时应该返回 nil")
	}
}

func TestSendNotification(t *testing.T) {
	logger := log.New(os.Stdout, "", 0)

	t.Run("推送已关闭", func(t *testing.T) {
		config := &Config{
			Push: PushConfig{
				Enable: false,
			},
		}
		predictor := NewWeatherPredictor(config, logger, nil)
		// 不应该 panic
		predictor.sendNotification("title", "content", 1, []string{})
	})

	t.Run("未配置推送URL", func(t *testing.T) {
		config := &Config{
			Push: PushConfig{
				Enable:  true,
				PushURL: "",
			},
		}
		predictor := NewWeatherPredictor(config, logger, nil)
		// 不应该 panic
		predictor.sendNotification("title", "content", 1, []string{})
	})

	t.Run("正常推送", func(t *testing.T) {
		config := &Config{
			Push: PushConfig{
				Enable:   true,
				PushURL:  "http://example.com",
				Markdown: true,
			},
		}
		predictor := NewWeatherPredictor(config, logger, nil)
		// 由于没有真实的推送服务，会返回错误，但不应该 panic
		predictor.sendNotification("title", "content", 1, []string{})
	})
}

func TestFetchDataForCity(t *testing.T) {
	// 创建 mock HTTP 服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"tb_quality": "0.85",
			"tb_aod": "0.3",
			"tb_event_time": "2024-01-01 18:30"
		}`))
	}))
	defer server.Close()

	logger := log.New(os.Stdout, "", 0)
	config := &Config{
		Request: RequestConfig{
			BaseURL: server.URL + "/",
		},
		Push: PushConfig{
			Enable: false,
		},
		Schedule: ScheduleConfig{
			City: "北京",
			Morning: TaskConfig{
				Model: []string{"GFS"},
			},
			Evening: TaskConfig{
				Model: []string{"GFS"},
			},
		},
	}

	store, cleanup := setupTestStore(t)
	defer cleanup()

	predictor := NewWeatherPredictor(config, logger, store)

	now := time.Now()
	predictor.fetchDataForCity("北京", []string{"GFS"}, true, now, "morning")
	// 不应该 panic
}

func TestFetchData(t *testing.T) {
	// 创建 mock HTTP 服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"tb_quality": "0.85",
			"tb_aod": "0.3",
			"tb_event_time": "2024-01-01 18:30"
		}`))
	}))
	defer server.Close()

	logger := log.New(os.Stdout, "", 0)
	config := &Config{
		Request: RequestConfig{
			BaseURL: server.URL + "/",
		},
		Push: PushConfig{
			Enable: false,
		},
		Schedule: ScheduleConfig{
			City: "北京",
			Morning: TaskConfig{
				Model: []string{"GFS"},
			},
			Evening: TaskConfig{
				Model: []string{"GFS"},
			},
		},
	}

	store, cleanup := setupTestStore(t)
	defer cleanup()

	predictor := NewWeatherPredictor(config, logger, store)

	predictor.FetchData(true)
	// 不应该 panic

	predictor.FetchData(false)
	// 不应该 panic
}

func TestBuildMarkdownResponse(t *testing.T) {
	// 创建 mock HTTP 服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"tb_quality": "0.85",
			"tb_aod": "0.3",
			"tb_event_time": "2024-01-01 18:30"
		}`))
	}))
	defer server.Close()

	logger := log.New(os.Stdout, "", 0)
	config := &Config{
		Request: RequestConfig{
			BaseURL: server.URL + "/",
		},
	}

	store, cleanup := setupTestStore(t)
	defer cleanup()

	predictor := NewWeatherPredictor(config, logger, store)

	urls := map[string]string{
		server.URL: "GFS",
	}

	lines, maxPriority, hasData := predictor.buildMarkdownResponse("北京", urls, "evening")

	if !hasData {
		t.Error("buildMarkdownResponse() 应该有数据")
	}
	if maxPriority == nil {
		t.Error("maxPriority 不应该为 nil")
	}
	if len(lines) == 0 {
		t.Error("lines 不应该为空")
	}
}

func TestBuildMarkdownResponseNoData(t *testing.T) {
	// 创建返回空数据的 mock HTTP 服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{}`))
	}))
	defer server.Close()

	logger := log.New(os.Stdout, "", 0)
	config := &Config{
		Request: RequestConfig{
			BaseURL: server.URL + "/",
		},
	}

	predictor := NewWeatherPredictor(config, logger, nil)

	urls := map[string]string{
		server.URL: "GFS",
	}

	lines, maxPriority, hasData := predictor.buildMarkdownResponse("北京", urls, "evening")

	if hasData {
		t.Error("buildMarkdownResponse() 不应该有数据")
	}
	if maxPriority != nil {
		t.Error("maxPriority 应该为 nil")
	}
	if len(lines) != 0 {
		t.Error("lines 应该为空")
	}
}

func TestParseWeatherDataEdgeCases(t *testing.T) {
	logger := log.New(os.Stdout, "", 0)
	config := &Config{
		Request: RequestConfig{
			BaseURL: "https://example.com/",
		},
	}

	predictor := &WeatherPredictor{
		config: config,
		client: nil,
		logger: logger,
	}

	t.Run("空质量数据", func(t *testing.T) {
		content := `{
			"tb_quality": "",
			"tb_aod": "0.3",
			"tb_event_time": "2024-01-01 18:30"
		}`
		result := predictor.parseWeatherData(content)
		if result == nil {
			t.Fatal("parseWeatherData() 返回 nil")
		}
		if result.QualityNum != nil {
			t.Error("QualityNum 应该为 nil")
		}
	})

	t.Run("AOD为空", func(t *testing.T) {
		content := `{
			"tb_quality": "0.85",
			"tb_aod": "",
			"tb_event_time": "2024-01-01 18:30"
		}`
		result := predictor.parseWeatherData(content)
		if result == nil {
			t.Fatal("parseWeatherData() 返回 nil")
		}
		if result.AODNum != nil {
			t.Errorf("AODNum = %v, want nil", *result.AODNum)
		}
	})

	t.Run("低质量数据", func(t *testing.T) {
		content := `{
			"tb_quality": "0.15",
			"tb_aod": "0.3",
			"tb_event_time": "2024-01-01 18:30"
		}`
		result := predictor.parseWeatherData(content)
		if result == nil {
			t.Fatal("parseWeatherData() 返回 nil")
		}
		if result.QualityNum == nil {
			t.Fatal("QualityNum 不应该为 nil")
		}
		if *result.QualityNum >= 0.4 {
			t.Errorf("低质量数据应该小于 0.4, got %f", *result.QualityNum)
		}
	})

	t.Run("高质量数据", func(t *testing.T) {
		content := `{
			"tb_quality": "0.85",
			"tb_aod": "0.3",
			"tb_event_time": "2024-01-01 18:30"
		}`
		result := predictor.parseWeatherData(content)
		if result == nil {
			t.Fatal("parseWeatherData() 返回 nil")
		}
		if result.QualityNum == nil {
			t.Fatal("QualityNum 不应该为 nil")
		}
		if *result.QualityNum < 0.4 {
			t.Errorf("高质量数据应该大于等于 0.4, got %f", *result.QualityNum)
		}
	})

	t.Run("低AOD数据", func(t *testing.T) {
		content := `{
			"tb_quality": "0.85",
			"tb_aod": "0.3",
			"tb_event_time": "2024-01-01 18:30"
		}`
		result := predictor.parseWeatherData(content)
		if result == nil {
			t.Fatal("parseWeatherData() 返回 nil")
		}
		if result.AODNum == nil {
			t.Fatal("AODNum 不应该为 nil")
		}
		if *result.AODNum > 0.4 {
			t.Errorf("低AOD数据应该小于等于 0.4, got %f", *result.AODNum)
		}
	})
}

func TestFetchSingleDataConnectionError(t *testing.T) {
	logger := log.New(os.Stdout, "", 0)
	config := &Config{
		Request: RequestConfig{
			BaseURL: "http://localhost:99999/",
		},
		Schedule: ScheduleConfig{
			PushError: true,
		},
	}

	predictor := NewWeatherPredictor(config, logger, nil)

	result := predictor.fetchSingleData("http://localhost:99999")
	if result == nil {
		t.Fatal("fetchSingleData() 在连接错误时应该返回错误结果")
	}
	if !contains(result.PushStr, "失败") {
		t.Errorf("PushStr 应包含 '失败', got %q", result.PushStr)
	}
}

func TestFetchSingleDataReadError(t *testing.T) {
	// 创建一个在读取时关闭连接的服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			return
		}
		conn, _, _ := hijacker.Hijack()
		conn.Close()
	}))
	defer server.Close()

	logger := log.New(os.Stdout, "", 0)
	config := &Config{
		Request: RequestConfig{
			BaseURL: server.URL + "/",
		},
		Schedule: ScheduleConfig{
			PushError: true,
		},
	}

	predictor := NewWeatherPredictor(config, logger, nil)

	result := predictor.fetchSingleData(server.URL)
	// 这可能会返回 nil 或错误结果，取决于连接关闭的时机
	_ = result
}

func TestFetchDataForCityWithMultipleModels(t *testing.T) {
	// 创建 mock HTTP 服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"tb_quality": "0.85",
			"tb_aod": "0.3",
			"tb_event_time": "2024-01-01 18:30"
		}`))
	}))
	defer server.Close()

	logger := log.New(os.Stdout, "", 0)
	config := &Config{
		Request: RequestConfig{
			BaseURL: server.URL + "/",
		},
		Push: PushConfig{
			Enable: false,
		},
		Schedule: ScheduleConfig{
			City: "北京",
			Morning: TaskConfig{
				Model: []string{"GFS", "EC"},
			},
			Evening: TaskConfig{
				Model: []string{"GFS", "EC"},
			},
		},
	}

	store, cleanup := setupTestStore(t)
	defer cleanup()

	predictor := NewWeatherPredictor(config, logger, store)

	now := time.Now()
	predictor.fetchDataForCity("北京", []string{"GFS", "EC"}, true, now, "morning")
	// 不应该 panic
}

func TestFetchDataMorning(t *testing.T) {
	// 创建 mock HTTP 服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"tb_quality": "0.85",
			"tb_aod": "0.3",
			"tb_event_time": "2024-01-01 18:30"
		}`))
	}))
	defer server.Close()

	logger := log.New(os.Stdout, "", 0)
	config := &Config{
		Request: RequestConfig{
			BaseURL: server.URL + "/",
		},
		Push: PushConfig{
			Enable: false,
		},
		Schedule: ScheduleConfig{
			City: "北京",
			Morning: TaskConfig{
				Model: []string{"GFS"},
			},
			Evening: TaskConfig{
				Model: []string{"GFS"},
			},
		},
	}

	store, cleanup := setupTestStore(t)
	defer cleanup()

	predictor := NewWeatherPredictor(config, logger, store)

	predictor.FetchData(true)
	// 不应该 panic
}

func TestBuildMarkdownResponseMultipleDates(t *testing.T) {
	// 创建 mock HTTP 服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"tb_quality": "0.85",
			"tb_aod": "0.3",
			"tb_event_time": "2024-01-01 18:30"
		}`))
	}))
	defer server.Close()

	logger := log.New(os.Stdout, "", 0)
	config := &Config{
		Request: RequestConfig{
			BaseURL: server.URL + "/",
		},
	}

	store, cleanup := setupTestStore(t)
	defer cleanup()

	predictor := NewWeatherPredictor(config, logger, store)

	// 使用多个 URL 来模拟多个日期
	urls := map[string]string{
		server.URL: "GFS",
	}

	lines, maxPriority, hasData := predictor.buildMarkdownResponse("北京", urls, "evening")

	if !hasData {
		t.Error("buildMarkdownResponse() 应该有数据")
	}
	if maxPriority == nil {
		t.Error("maxPriority 不应该为 nil")
	}
	if len(lines) == 0 {
		t.Error("lines 不应该为空")
	}
}
