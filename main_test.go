package main

import (
	"os"
	"syscall"
	"testing"
	"time"
)

func TestBuildCronSpec(t *testing.T) {
	tests := []struct {
		name     string
		timeStr  string
		expected string
	}{
		{"完整时间 HH:MM:SS", "18:30:00", "00 30 18 * * *"},
		{"只有小时和分钟 HH:MM", "18:30", "0 30 18 * * *"},
		{"只有小时 HH", "18", "0 0 18 * * *"},
		{"零时零分零秒", "00:00:00", "00 00 00 * * *"},
		{"单个数字", "8", "0 0 8 * * *"},
		{"带前导零", "08:05:09", "09 05 08 * * *"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := buildCronSpec(tt.timeStr)
			if err != nil {
				t.Fatalf("buildCronSpec(%q) 返回错误: %v", tt.timeStr, err)
			}
			if result != tt.expected {
				t.Errorf("buildCronSpec(%q) = %q, want %q", tt.timeStr, result, tt.expected)
			}
		})
	}
}

func TestBuildCronSpecRejectsInvalid(t *testing.T) {
	tests := []struct {
		name    string
		timeStr string
	}{
		{"空字符串", ""},
		{"只有冒号", ":"},
		{"两个冒号", "::"},
		{"无效格式", "abc"},
		{"小时越界", "25:00"},
		{"分钟越界", "18:60"},
		{"秒越界", "18:30:60"},
		{"负数", "-1:00"},
		{"过多段", "1:2:3:4"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := buildCronSpec(tt.timeStr); err == nil {
				t.Errorf("buildCronSpec(%q) 应该返回错误", tt.timeStr)
			}
		})
	}
}

func TestMainIntegration(t *testing.T) {
	// 设置环境变量
	os.Setenv("CITY", "北京")
	os.Setenv("PUSH_ENABLE", "false")
	os.Setenv("MORNING_ENABLE", "true")
	os.Setenv("EVENING_ENABLE", "true")
	os.Setenv("SEND_TEST_ON_START", "false")
	os.Setenv("DATA_RETENTION_DAYS", "0")
	os.Setenv("WEB_PORT", "18099")
	os.Setenv("DB_PATH", "/tmp/liuxia_test_main.db")
	defer func() {
		os.Unsetenv("CITY")
		os.Unsetenv("PUSH_ENABLE")
		os.Unsetenv("MORNING_ENABLE")
		os.Unsetenv("EVENING_ENABLE")
		os.Unsetenv("SEND_TEST_ON_START")
		os.Unsetenv("DATA_RETENTION_DAYS")
		os.Unsetenv("WEB_PORT")
		os.Unsetenv("DB_PATH")
		os.Remove("/tmp/liuxia_test_main.db")
	}()

	done := make(chan bool, 1)

	go func() {
		main()
		done <- true
	}()

	// 等待程序启动
	time.Sleep(500 * time.Millisecond)

	// 发送 SIGTERM 信号来停止程序
	p, _ := os.FindProcess(os.Getpid())
	p.Signal(syscall.SIGTERM)

	// 等待程序退出
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("程序未在5秒内退出")
	}
}

func TestMainIntegrationWithMorningOnly(t *testing.T) {
	os.Setenv("CITY", "北京")
	os.Setenv("PUSH_ENABLE", "false")
	os.Setenv("MORNING_ENABLE", "true")
	os.Setenv("EVENING_ENABLE", "false")
	os.Setenv("SEND_TEST_ON_START", "false")
	os.Setenv("DATA_RETENTION_DAYS", "365")
	os.Setenv("WEB_PORT", "18098")
	os.Setenv("DB_PATH", "/tmp/liuxia_test_main2.db")
	defer func() {
		os.Unsetenv("CITY")
		os.Unsetenv("PUSH_ENABLE")
		os.Unsetenv("MORNING_ENABLE")
		os.Unsetenv("EVENING_ENABLE")
		os.Unsetenv("SEND_TEST_ON_START")
		os.Unsetenv("DATA_RETENTION_DAYS")
		os.Unsetenv("WEB_PORT")
		os.Unsetenv("DB_PATH")
		os.Remove("/tmp/liuxia_test_main2.db")
	}()

	done := make(chan bool, 1)

	go func() {
		main()
		done <- true
	}()

	time.Sleep(500 * time.Millisecond)

	p, _ := os.FindProcess(os.Getpid())
	p.Signal(syscall.SIGTERM)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("程序未在5秒内退出")
	}
}

func TestMainIntegrationWithEveningOnly(t *testing.T) {
	os.Setenv("CITY", "北京")
	os.Setenv("PUSH_ENABLE", "false")
	os.Setenv("MORNING_ENABLE", "false")
	os.Setenv("EVENING_ENABLE", "true")
	os.Setenv("SEND_TEST_ON_START", "false")
	os.Setenv("DATA_RETENTION_DAYS", "0")
	os.Setenv("WEB_PORT", "18097")
	os.Setenv("DB_PATH", "/tmp/liuxia_test_main3.db")
	defer func() {
		os.Unsetenv("CITY")
		os.Unsetenv("PUSH_ENABLE")
		os.Unsetenv("MORNING_ENABLE")
		os.Unsetenv("EVENING_ENABLE")
		os.Unsetenv("SEND_TEST_ON_START")
		os.Unsetenv("DATA_RETENTION_DAYS")
		os.Unsetenv("WEB_PORT")
		os.Unsetenv("DB_PATH")
		os.Remove("/tmp/liuxia_test_main3.db")
	}()

	done := make(chan bool, 1)

	go func() {
		main()
		done <- true
	}()

	time.Sleep(500 * time.Millisecond)

	p, _ := os.FindProcess(os.Getpid())
	p.Signal(syscall.SIGTERM)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("程序未在5秒内退出")
	}
}

func TestMainIntegrationWithNoTask(t *testing.T) {
	os.Setenv("CITY", "北京")
	os.Setenv("PUSH_ENABLE", "false")
	os.Setenv("MORNING_ENABLE", "false")
	os.Setenv("EVENING_ENABLE", "false")
	os.Setenv("SEND_TEST_ON_START", "false")
	os.Setenv("DATA_RETENTION_DAYS", "0")
	os.Setenv("WEB_PORT", "18096")
	os.Setenv("DB_PATH", "/tmp/liuxia_test_main4.db")
	defer func() {
		os.Unsetenv("CITY")
		os.Unsetenv("PUSH_ENABLE")
		os.Unsetenv("MORNING_ENABLE")
		os.Unsetenv("EVENING_ENABLE")
		os.Unsetenv("SEND_TEST_ON_START")
		os.Unsetenv("DATA_RETENTION_DAYS")
		os.Unsetenv("WEB_PORT")
		os.Unsetenv("DB_PATH")
		os.Remove("/tmp/liuxia_test_main4.db")
	}()

	done := make(chan bool, 1)

	go func() {
		main()
		done <- true
	}()

	time.Sleep(500 * time.Millisecond)

	p, _ := os.FindProcess(os.Getpid())
	p.Signal(syscall.SIGTERM)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("程序未在5秒内退出")
	}
}
