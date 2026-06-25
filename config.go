package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type RequestConfig struct {
	BaseURL string
}

type PushConfig struct {
	Enable     bool
	NtfyServer string
	NtfyTopic  string
	NtfyToken  string
}

type TaskConfig struct {
	Enable bool
	Time   []string
	Model  []string
}

type ScheduleConfig struct {
	City            string
	Cities          []string
	SendTestOnStart bool
	PushError       bool
	Morning         TaskConfig
	Evening         TaskConfig
	DataRetention   int
}

type Config struct {
	Request  RequestConfig
	Push     PushConfig
	Schedule ScheduleConfig
}

func getEnv(key, defaultValue string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return defaultValue
}

func getEnvList(key string, defaultValue []string) []string {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return defaultValue
	}
	parts := strings.Split(v, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	if len(result) == 0 {
		return defaultValue
	}
	return result
}

func LoadConfig() (*Config, error) {
	city := getEnv("CITY", "")
	if city == "" {
		return nil, fmt.Errorf("环境变量 CITY 未设置")
	}

	cities := getEnvList("CITY", []string{city})

	ntfyTopic := getEnv("NTFY_TOPIC", "")
	if ntfyTopic == "" {
		return nil, fmt.Errorf("环境变量 NTFY_TOPIC 未设置")
	}

	dataRetention := 365
	if v := os.Getenv("DATA_RETENTION_DAYS"); v != "" {
		if days, err := strconv.Atoi(v); err == nil {
			dataRetention = days
		}
	}

	cfg := &Config{
		Request: RequestConfig{
			BaseURL: getEnv("BASE_URL", "https://sunsetbot.top/"),
		},
		Push: PushConfig{
			Enable:     getEnvBool("PUSH_ENABLE", true),
			NtfyServer: getEnv("NTFY_SERVER", "https://ntfy.sh"),
			NtfyTopic:  ntfyTopic,
			NtfyToken:  getEnv("NTFY_TOKEN", ""),
		},
		Schedule: ScheduleConfig{
			City:            cities[0],
			Cities:          cities,
			SendTestOnStart: getEnvBool("SEND_TEST_ON_START", false),
			PushError:       getEnvBool("PUSH_ERROR", true),
			Morning: TaskConfig{
				Enable: getEnvBool("MORNING_ENABLE", true),
				Time:   getEnvList("MORNING_TIME", []string{"18:00", "00:00"}),
				Model:  getEnvList("MORNING_MODEL", []string{"GFS", "EC"}),
			},
			Evening: TaskConfig{
				Enable: getEnvBool("EVENING_ENABLE", true),
				Time:   getEnvList("EVENING_TIME", []string{"08:00", "11:30", "16:00"}),
				Model:  getEnvList("EVENING_MODEL", []string{"GFS", "EC"}),
			},
			DataRetention: dataRetention,
		},
	}

	return cfg, nil
}
