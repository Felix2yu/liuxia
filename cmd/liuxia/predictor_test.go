package main

import (
	"log"
	"os"
	"testing"
)

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

func TestMinValidDate(t *testing.T) {
	expected := "2020-01-01"
	if minValidDate.Format("2006-01-02") != expected {
		t.Errorf("minValidDate = %v, want %v", minValidDate.Format("2006-01-02"), expected)
	}
}

func TestNewWeatherPredictor(t *testing.T) {
	logger := log.New(os.Stdout, "", 0)
	config := &Config{
		Push: PushConfig{Enable: false},
		Schedule: ScheduleConfig{
			City:    "北京",
			Morning: TaskConfig{Model: []string{"GFS"}},
			Evening: TaskConfig{Model: []string{"GFS"}},
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

func TestErrorResult(t *testing.T) {
	logger := log.New(os.Stdout, "", 0)
	config := &Config{
		Schedule: ScheduleConfig{PushError: true},
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

func TestSendNotification(t *testing.T) {
	logger := log.New(os.Stdout, "", 0)

	t.Run("推送已关闭", func(t *testing.T) {
		config := &Config{Push: PushConfig{Enable: false}}
		predictor := NewWeatherPredictor(config, logger, nil)
		predictor.sendNotification("title", "content", 1, []string{})
	})

	t.Run("未配置推送URL", func(t *testing.T) {
		config := &Config{Push: PushConfig{Enable: true, PushURL: ""}}
		predictor := NewWeatherPredictor(config, logger, nil)
		predictor.sendNotification("title", "content", 1, []string{})
	})

	t.Run("正常推送", func(t *testing.T) {
		config := &Config{Push: PushConfig{Enable: true, PushURL: "http://example.com", Markdown: true}}
		predictor := NewWeatherPredictor(config, logger, nil)
		predictor.sendNotification("title", "content", 1, []string{})
	})
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
