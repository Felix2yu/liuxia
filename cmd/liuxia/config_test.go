package main

import (
	"os"
	"testing"
)

func TestGetEnv(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		defaultValue string
		envValue     string
		setEnv       bool
		expected     string
	}{
		{
			name:         "环境变量存在时返回环境变量值",
			key:          "TEST_KEY",
			defaultValue: "default",
			envValue:     "custom",
			setEnv:       true,
			expected:     "custom",
		},
		{
			name:         "环境变量不存在时返回默认值",
			key:          "TEST_KEY",
			defaultValue: "default",
			setEnv:       false,
			expected:     "default",
		},
		{
			name:         "环境变量为空字符串时返回默认值",
			key:          "TEST_KEY",
			defaultValue: "default",
			envValue:     "",
			setEnv:       true,
			expected:     "default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setEnv {
				os.Setenv(tt.key, tt.envValue)
				defer os.Unsetenv(tt.key)
			} else {
				os.Unsetenv(tt.key)
			}

			result := getEnv(tt.key, tt.defaultValue)
			if result != tt.expected {
				t.Errorf("getEnv(%q, %q) = %q, want %q", tt.key, tt.defaultValue, result, tt.expected)
			}
		})
	}
}

func TestGetEnvBool(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		defaultValue bool
		envValue     string
		setEnv       bool
		expected     bool
	}{
		{
			name:         "环境变量为true时返回true",
			key:          "TEST_BOOL_KEY",
			defaultValue: false,
			envValue:     "true",
			setEnv:       true,
			expected:     true,
		},
		{
			name:         "环境变量为false时返回false",
			key:          "TEST_BOOL_KEY",
			defaultValue: true,
			envValue:     "false",
			setEnv:       true,
			expected:     false,
		},
		{
			name:         "环境变量不存在时返回默认值",
			key:          "TEST_BOOL_KEY",
			defaultValue: true,
			setEnv:       false,
			expected:     true,
		},
		{
			name:         "环境变量无效时返回默认值",
			key:          "TEST_BOOL_KEY",
			defaultValue: false,
			envValue:     "invalid",
			setEnv:       true,
			expected:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setEnv {
				os.Setenv(tt.key, tt.envValue)
				defer os.Unsetenv(tt.key)
			} else {
				os.Unsetenv(tt.key)
			}

			result := getEnvBool(tt.key, tt.defaultValue)
			if result != tt.expected {
				t.Errorf("getEnvBool(%q, %v) = %v, want %v", tt.key, tt.defaultValue, result, tt.expected)
			}
		})
	}
}

func TestGetEnvList(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		defaultValue []string
		envValue     string
		setEnv       bool
		expected     []string
	}{
		{
			name:         "单个值",
			key:          "TEST_LIST_KEY",
			defaultValue: []string{"default"},
			envValue:     "value1",
			setEnv:       true,
			expected:     []string{"value1"},
		},
		{
			name:         "多个逗号分隔的值",
			key:          "TEST_LIST_KEY",
			defaultValue: []string{"default"},
			envValue:     "value1,value2,value3",
			setEnv:       true,
			expected:     []string{"value1", "value2", "value3"},
		},
		{
			name:         "带空格的多个值",
			key:          "TEST_LIST_KEY",
			defaultValue: []string{"default"},
			envValue:     "  value1 , value2 , value3  ",
			setEnv:       true,
			expected:     []string{"value1", "value2", "value3"},
		},
		{
			name:         "环境变量不存在时返回默认值",
			key:          "TEST_LIST_KEY",
			defaultValue: []string{"default"},
			setEnv:       false,
			expected:     []string{"default"},
		},
		{
			name:         "空字符串返回默认值",
			key:          "TEST_LIST_KEY",
			defaultValue: []string{"default"},
			envValue:     "",
			setEnv:       true,
			expected:     []string{"default"},
		},
		{
			name:         "只有空格的值返回默认值",
			key:          "TEST_LIST_KEY",
			defaultValue: []string{"default"},
			envValue:     "  , , ",
			setEnv:       true,
			expected:     []string{"default"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setEnv {
				os.Setenv(tt.key, tt.envValue)
				defer os.Unsetenv(tt.key)
			} else {
				os.Unsetenv(tt.key)
			}

			result := getEnvList(tt.key, tt.defaultValue)
			if len(result) != len(tt.expected) {
				t.Errorf("getEnvList(%q, %v) 长度 = %d, want %d", tt.key, tt.defaultValue, len(result), len(tt.expected))
				return
			}
			for i, v := range result {
				if v != tt.expected[i] {
					t.Errorf("getEnvList(%q, %v)[%d] = %q, want %q", tt.key, tt.defaultValue, i, v, tt.expected[i])
				}
			}
		})
	}
}

func TestLoadConfig(t *testing.T) {
	t.Run("缺少CITY环境变量返回错误", func(t *testing.T) {
		os.Unsetenv("CITY")
		_, err := LoadConfig()
		if err == nil {
			t.Error("LoadConfig() 在缺少 CITY 环境变量时应返回错误")
		}
	})

	t.Run("基本配置加载", func(t *testing.T) {
		os.Setenv("CITY", "北京")
		os.Setenv("PUSH_ENABLE", "false")
		defer func() {
			os.Unsetenv("CITY")
			os.Unsetenv("PUSH_ENABLE")
		}()

		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("LoadConfig() 意外错误: %v", err)
		}

		if cfg.Schedule.City != "北京" {
			t.Errorf("Schedule.City = %q, want %q", cfg.Schedule.City, "北京")
		}
		if cfg.Request.BaseURL != "https://sunsetbot.top/" {
			t.Errorf("Request.BaseURL = %q, want %q", cfg.Request.BaseURL, "https://sunsetbot.top/")
		}
	})

	t.Run("多城市配置", func(t *testing.T) {
		os.Setenv("CITY", "北京,上海,广州")
		os.Setenv("PUSH_ENABLE", "false")
		defer func() {
			os.Unsetenv("CITY")
			os.Unsetenv("PUSH_ENABLE")
		}()

		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("LoadConfig() 意外错误: %v", err)
		}

		if len(cfg.Schedule.Cities) != 3 {
			t.Errorf("Schedule.Cities 长度 = %d, want 3", len(cfg.Schedule.Cities))
		}
		if cfg.Schedule.City != "北京" {
			t.Errorf("Schedule.City = %q, want %q", cfg.Schedule.City, "北京")
		}
	})

	t.Run("推送配置验证", func(t *testing.T) {
		os.Setenv("CITY", "北京")
		os.Setenv("PUSH_ENABLE", "true")
		os.Setenv("PUSH_URL", "")
		defer func() {
			os.Unsetenv("CITY")
			os.Unsetenv("PUSH_ENABLE")
			os.Unsetenv("PUSH_URL")
		}()

		_, err := LoadConfig()
		if err == nil {
			t.Error("LoadConfig() 在推送已启用但未配置 PUSH_URL 时应返回错误")
		}
	})

	t.Run("DATA_RETENTION_DAYS为负数返回错误", func(t *testing.T) {
		os.Setenv("CITY", "北京")
		os.Setenv("PUSH_ENABLE", "false")
		os.Setenv("DATA_RETENTION_DAYS", "-30")
		defer func() {
			os.Unsetenv("CITY")
			os.Unsetenv("PUSH_ENABLE")
			os.Unsetenv("DATA_RETENTION_DAYS")
		}()

		_, err := LoadConfig()
		if err == nil {
			t.Error("LoadConfig() 在 DATA_RETENTION_DAYS 为负数时应返回错误")
		}
	})

	t.Run("DATA_RETENTION_DAYS为非数字返回错误", func(t *testing.T) {
		os.Setenv("CITY", "北京")
		os.Setenv("PUSH_ENABLE", "false")
		os.Setenv("DATA_RETENTION_DAYS", "abc")
		defer func() {
			os.Unsetenv("CITY")
			os.Unsetenv("PUSH_ENABLE")
			os.Unsetenv("DATA_RETENTION_DAYS")
		}()

		_, err := LoadConfig()
		if err == nil {
			t.Error("LoadConfig() 在 DATA_RETENTION_DAYS 非数字时应返回错误")
		}
	})

	t.Run("DATA_RETENTION_DAYS为0表示禁用清理", func(t *testing.T) {
		os.Setenv("CITY", "北京")
		os.Setenv("PUSH_ENABLE", "false")
		os.Setenv("DATA_RETENTION_DAYS", "0")
		defer func() {
			os.Unsetenv("CITY")
			os.Unsetenv("PUSH_ENABLE")
			os.Unsetenv("DATA_RETENTION_DAYS")
		}()

		cfg, err := LoadConfig()
		if err != nil {
			t.Fatalf("LoadConfig() 意外错误: %v", err)
		}
		if cfg.Schedule.DataRetention != 0 {
			t.Errorf("Schedule.DataRetention = %d, want 0", cfg.Schedule.DataRetention)
		}
	})
}
