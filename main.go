package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/robfig/cron/v3"
)

func main() {
	logger := log.New(os.Stdout, "", log.LstdFlags)
	logger.Printf("[启动] %s", time.Now().Format("2006-01-02 15:04:05"))

	cfg, err := LoadConfig()
	if err != nil {
		logger.Fatalf("初始化失败: %v", err)
	}

	dbPath := getEnv("DB_PATH", "liuxia.db")
	store, err := InitStore(dbPath)
	if err != nil {
		logger.Fatalf("数据库初始化失败: %v", err)
	}
	defer store.Close()
	logger.Printf("[启动] 数据库已初始化: %s", dbPath)

	// 清理异常日期数据（早于 2020-01-01 的记录）
	if deleted, err := store.DeleteAbnormalDates(); err != nil {
		logger.Printf("[清理] 异常日期数据清理失败: %v", err)
	} else if deleted > 0 {
		logger.Printf("[清理] 已清理 %d 条异常日期数据（早于 2020-01-01）", deleted)
	}

	predictor := NewWeatherPredictor(cfg, logger, store)

	c := cron.New(
		cron.WithSeconds(),
		cron.WithLogger(cron.PrintfLogger(logger)),
	)

	morningEnable := cfg.Schedule.Morning.Enable
	eveningEnable := cfg.Schedule.Evening.Enable
	pushEnable := cfg.Push.Enable

	if morningEnable {
		for _, rt := range cfg.Schedule.Morning.Time {
			rt = strings.TrimSpace(rt)
			if rt == "" {
				continue
			}
		logger.Printf("[启动] 朝霞任务将每天 %s 执行", rt)
		spec, err := buildCronSpec(rt)
		if err != nil {
			logger.Fatalf("朝霞任务时间配置错误: %v", err)
		}
		isMorning := true
		_, err = c.AddFunc(spec, func() {
			predictor.FetchData(isMorning)
		})
		if err != nil {
			logger.Printf("朝霞任务 %s 调度错误: %v", rt, err)
		}
		}
	}

	if eveningEnable {
		for _, rt := range cfg.Schedule.Evening.Time {
			rt = strings.TrimSpace(rt)
			if rt == "" {
				continue
			}
		logger.Printf("[启动] 晚霞任务将每天 %s 执行", rt)
		spec, err := buildCronSpec(rt)
		if err != nil {
			logger.Fatalf("晚霞任务时间配置错误: %v", err)
		}
		isMorning := false
		_, err = c.AddFunc(spec, func() {
			predictor.FetchData(isMorning)
		})
		if err != nil {
			logger.Printf("晚霞任务 %s 调度错误: %v", rt, err)
		}
		}
	}

	logger.Printf(
		"[启动] 朝霞任务：%v 晚霞任务：%v 推送通知: %v 推送异常：%v",
		morningEnable,
		eveningEnable,
		pushEnable,
		cfg.Schedule.PushError,
	)

	if cfg.Schedule.SendTestOnStart {
		predictor.sendNotification("服务启动测试", "服务已启动，这是一条测试消息", 3, nil)
	}

	if cfg.Schedule.DataRetention > 0 {
		_, err := c.AddFunc("0 0 3 * * *", func() {
			logger.Printf("[清理] 开始清理 %d 天前的数据", cfg.Schedule.DataRetention)
			deleted, err := store.DeleteOldRecords(cfg.Schedule.DataRetention)
			if err != nil {
				logger.Printf("[清理] 清理失败: %v", err)
			} else {
				logger.Printf("[清理] 已清理 %d 条旧数据", deleted)
			}
		})
		if err != nil {
			logger.Printf("[启动] 数据清理任务调度错误: %v", err)
		} else {
			logger.Printf("[启动] 数据清理任务已启用，保留 %d 天数据", cfg.Schedule.DataRetention)
		}
	} else {
		logger.Printf("[启动] 数据清理任务已禁用（DATA_RETENTION_DAYS=0）")
	}

	webPort := getEnv("WEB_PORT", "8080")
	go func() {
		if err := StartWebServer(webPort, store, logger); err != nil {
			logger.Fatalf("[Web] 服务启动失败: %v", err)
		}
	}()

	c.Start()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	c.Stop()
	logger.Println("[退出] 收到终止信号，程序退出")
}

// buildCronSpec 将 "HH" / "HH:MM" / "HH:MM:SS" 转换为 6 段 cron 表达式。
// 时间格式非法时返回错误，避免配置写错导致任务静默丢失。
func buildCronSpec(timeStr string) (string, error) {
	parts := strings.Split(timeStr, ":")
	if len(parts) > 3 {
		return "", fmt.Errorf("时间格式应为 HH[:MM[:SS]]，实际: %q", timeStr)
	}

	hour, err := validateTimePart(parts[0], 23, "小时")
	if err != nil {
		return "", fmt.Errorf("时间 %q 非法: %w", timeStr, err)
	}
	minute := "0"
	second := "0"
	if len(parts) > 1 {
		if minute, err = validateTimePart(parts[1], 59, "分钟"); err != nil {
			return "", fmt.Errorf("时间 %q 非法: %w", timeStr, err)
		}
	}
	if len(parts) > 2 {
		if second, err = validateTimePart(parts[2], 59, "秒"); err != nil {
			return "", fmt.Errorf("时间 %q 非法: %w", timeStr, err)
		}
	}
	return second + " " + minute + " " + hour + " * * *", nil
}

// validateTimePart 校验单个时间字段为 [0, max] 范围内的整数，保留原始字符串（含前导零）。
func validateTimePart(part string, max int, name string) (string, error) {
	part = strings.TrimSpace(part)
	if part == "" {
		return "", fmt.Errorf("%s为空", name)
	}
	v, err := strconv.Atoi(part)
	if err != nil || v < 0 || v > max {
		return "", fmt.Errorf("%s %q 超出范围 [0, %d]", name, part, max)
	}
	return part, nil
}
