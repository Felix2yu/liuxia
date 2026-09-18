package main

import (
	"fmt"
	"log"
	"strings"

	apprise "github.com/unraid/apprise-go"
)

// Notifier 通用通知接口
type Notifier interface {
	Send(title, body string, priority int, tags []string, markdown bool) error
	Name() string
}

// NewNotifier 根据配置创建对应的 Notifier
func NewNotifier(cfg *PushConfig, logger *log.Logger) Notifier {
	if cfg.PushURL == "" {
		return nil
	}
	return &AppriseNotifier{
		PushURL: cfg.PushURL,
		logger:  logger,
	}
}

// stripMarkdown 移除 Markdown 格式标记，返回纯文本
func stripMarkdown(s string) string {
	s = strings.ReplaceAll(s, "**", "")
	lines := strings.Split(s, "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			result = append(result, trimmed[3:])
		} else if strings.HasPrefix(trimmed, "### ") {
			result = append(result, trimmed[4:])
		} else if strings.HasPrefix(trimmed, "- ") {
			result = append(result, trimmed[2:])
		} else {
			result = append(result, line)
		}
	}
	return strings.Join(result, "\n")
}

// AppriseNotifier 基于 apprise-go 的通知实现
type AppriseNotifier struct {
	PushURL string
	logger  *log.Logger
}

func (s *AppriseNotifier) Name() string { return "apprise" }

func (s *AppriseNotifier) Send(title, body string, priority int, tags []string, markdown bool) error {
	if s.PushURL == "" {
		return fmt.Errorf("未设置 PUSH_URL")
	}

	// apprise-go 使用逗号分隔多个 URL
	urls := strings.Split(s.PushURL, ",")

	// 构建完整消息
	message := fmt.Sprintf("%s\n\n%s", title, body)

	// 根据 markdown 参数决定输入格式
	var opts []apprise.Option
	opts = append(opts, apprise.WithTitle(title))
	if markdown {
		opts = append(opts, apprise.WithInputFormat("markdown"))
	} else {
		message = stripMarkdown(message)
	}

	// 发送通知
	if err := apprise.Send(urls, message, opts...); err != nil {
		return fmt.Errorf("推送失败: %w", err)
	}

	s.logger.Printf("[推送成功] 通知已发送, 优先级: %d", priority)
	return nil
}
