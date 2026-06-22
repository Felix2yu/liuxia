package main

import (
	"log"
	"os"
	"os/signal"
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

	predictor := NewWeatherPredictor(cfg, logger)

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
			spec := buildCronSpec(rt)
			isMorning := true
			_, err := c.AddFunc(spec, func() {
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
			spec := buildCronSpec(rt)
			isMorning := false
			_, err := c.AddFunc(spec, func() {
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
		predictor.sendNtfyNotification("服务启动测试", "服务已启动，这是一条测试消息", 3, nil)
	}

	c.Start()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	c.Stop()
	logger.Println("[退出] 收到终止信号，程序退出")
}

func buildCronSpec(timeStr string) string {
	parts := strings.SplitN(timeStr, ":", 3)
	hour := "0"
	minute := "0"
	second := "0"
	if len(parts) > 0 {
		hour = parts[0]
	}
	if len(parts) > 1 {
		minute = parts[1]
	}
	if len(parts) > 2 {
		second = parts[2]
	}
	return second + " " + minute + " " + hour + " * * *"
}
