package main

import (
	"fmt"
	"log"
	"strings"

	"github.com/containrrr/shoutrrr"
	"github.com/containrrr/shoutrrr/pkg/types"
)

// Notifier 通用通知接口
type Notifier interface {
	Send(title, body string, priority int, tags []string, markdown bool) error
	Name() string
}

// NewNotifier 根据配置创建对应的 Notifier
func NewNotifier(cfg *PushConfig, logger *log.Logger) Notifier {
	if cfg.PushURL != "" {
		return &ShoutrrrNotifier{
			PushURL: cfg.PushURL,
			logger:  logger,
		}
	}
	// 向后兼容：使用 ntfy 直连
	if cfg.NtfyTopic != "" {
		return &NtfyNotifier{
			Server: cfg.NtfyServer,
			Topic:  cfg.NtfyTopic,
			Token:  cfg.NtfyToken,
			logger: logger,
		}
	}
	return nil
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

// ShoutrrrNotifier 基于 shoutrrr 的通知实现
type ShoutrrrNotifier struct {
	PushURL string
	logger  *log.Logger
}

func (s *ShoutrrrNotifier) Name() string { return "shoutrrr" }

func (s *ShoutrrrNotifier) Send(title, body string, priority int, tags []string, markdown bool) error {
	if s.PushURL == "" {
		return fmt.Errorf("未设置 PUSH_URL")
	}

	// shoutrrr 使用逗号分隔多个 URL
	urls := strings.Split(s.PushURL, ",")

	sender, err := shoutrrr.CreateSender(urls...)
	if err != nil {
		return fmt.Errorf("创建通知器失败: %w", err)
	}

	// 构建完整消息
	message := fmt.Sprintf("%s\n\n%s", title, body)

	// 构建参数
	params := &types.Params{
		"title": title,
	}

	// 优先级映射
	switch {
	case priority >= 5:
		(*params)["priority"] = "5"
	case priority >= 4:
		(*params)["priority"] = "4"
	case priority >= 3:
		(*params)["priority"] = "3"
	case priority >= 2:
		(*params)["priority"] = "2"
	default:
		(*params)["priority"] = "1"
	}

	if len(tags) > 0 {
		(*params)["tags"] = strings.Join(tags, ",")
	}

	// sender.Send 返回 []error
	errs := sender.Send(message, params)
	if len(errs) > 0 {
		// 收集所有错误
		var errMessages []string
		for _, e := range errs {
			if e != nil {
				errMessages = append(errMessages, e.Error())
			}
		}
		if len(errMessages) > 0 {
			return fmt.Errorf("推送失败: %s", strings.Join(errMessages, "; "))
		}
	}

	s.logger.Printf("[推送成功] 通知已发送, 优先级: %d", priority)
	return nil
}

// NtfyNotifier ntfy 直连通知实现（向后兼容）
type NtfyNotifier struct {
	Server string
	Topic  string
	Token  string
	logger *log.Logger
}

func (n *NtfyNotifier) Name() string { return "ntfy" }

func (n *NtfyNotifier) Send(title, body string, priority int, tags []string, markdown bool) error {
	if n.Topic == "" {
		return fmt.Errorf("未设置 ntfy_topic")
	}

	// 构建 ntfy URL
	server := strings.TrimRight(n.Server, "/")
	pushURL := fmt.Sprintf("%s/%s", server, n.Topic)
	if n.Token != "" {
		// 如果有 token，使用 token 格式
		pushURL = fmt.Sprintf("%s?auth=%s", pushURL, n.Token)
	}

	sender, err := shoutrrr.CreateSender(pushURL)
	if err != nil {
		return fmt.Errorf("创建通知器失败: %w", err)
	}

	message := fmt.Sprintf("%s\n\n%s", title, body)

	params := &types.Params{
		"title":    title,
		"priority": fmt.Sprintf("%d", priority),
		"markdown": fmt.Sprintf("%v", markdown),
	}
	if len(tags) > 0 {
		(*params)["tags"] = strings.Join(tags, ",")
	}

	errs := sender.Send(message, params)
	if len(errs) > 0 {
		var errMessages []string
		for _, e := range errs {
			if e != nil {
				errMessages = append(errMessages, e.Error())
			}
		}
		if len(errMessages) > 0 {
			return fmt.Errorf("推送失败: %s", strings.Join(errMessages, "; "))
		}
	}

	n.logger.Printf("[推送成功] ntfy 通知已发送到 %s, 优先级: %d", pushURL, priority)
	return nil
}
