package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
)

// Notifier 通用通知接口
type Notifier interface {
	Send(title, body string, priority int, tags []string, markdown bool) error
	Name() string
}

// NewNotifier 根据配置创建对应的 Notifier
func NewNotifier(cfg *PushConfig, client *http.Client, logger *log.Logger) Notifier {
	if cfg.AppriseURL != "" {
		return &AppriseNotifier{
			AppriseURL:  cfg.AppriseURL,
			AppriseKey:  cfg.AppriseKey,
			Targets:     cfg.AppriseTargets,
			client:      client,
			logger:      logger,
		}
	}
	// 向后兼容：如果没有配置 apprise_url，使用 ntfy 直连
	return &NtfyNotifier{
		Server: cfg.NtfyServer,
		Topic:  cfg.NtfyTopic,
		Token:  cfg.NtfyToken,
		client: client,
		logger: logger,
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

// AppriseNotifier 基于 apprise-api 的通知实现
type AppriseNotifier struct {
	AppriseURL  string
	AppriseKey  string
	Targets     []string
	client      *http.Client
	logger      *log.Logger
}

func (a *AppriseNotifier) Name() string { return "apprise" }

func (a *AppriseNotifier) Send(title, body string, priority int, tags []string, markdown bool) error {
	if a.AppriseURL == "" {
		return fmt.Errorf("未设置 apprise_url")
	}

	// 映射优先级到 apprise type
	msgType := "info"
	switch {
	case priority >= 5:
		msgType = "failure"
	case priority >= 4:
		msgType = "warning"
	case priority <= 2:
		msgType = "success"
	}

	payload := map[string]interface{}{
		"title": title,
		"body":  body,
		"type":  msgType,
	}

	if len(tags) > 0 {
		payload["tag"] = strings.Join(tags, ",")
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("序列化失败: %w", err)
	}

	// 确定发送目标
	targets := a.Targets
	if len(targets) == 0 {
		targets = []string{""} // 空字符串表示发送到所有已配置的服务
	}

	appriseURL := strings.TrimRight(a.AppriseURL, "/")
	keyPart := ""
	if a.AppriseKey != "" {
		keyPart = a.AppriseKey + "/"
	}

	var lastErr error
	for _, target := range targets {
		url := fmt.Sprintf("%s/%snotify/", appriseURL, keyPart)
		if target != "" {
			url = fmt.Sprintf("%s/%snotify/%s/", appriseURL, keyPart, target)
		}

		req, err := http.NewRequest("POST", url, bytes.NewReader(jsonData))
		if err != nil {
			lastErr = fmt.Errorf("构建请求失败: %w", err)
			continue
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := a.client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("推送失败: %w", err)
			continue
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()

		if resp.StatusCode >= 400 {
			lastErr = fmt.Errorf("推送失败: HTTP %d", resp.StatusCode)
			continue
		}

		a.logger.Printf("[推送成功] apprise 通知已发送, 目标: %s, 优先级: %d", target, priority)
	}

	return lastErr
}

// NtfyNotifier ntfy 直连通知实现（向后兼容）
type NtfyNotifier struct {
	Server string
	Topic  string
	Token  string
	client *http.Client
	logger *log.Logger
}

func (n *NtfyNotifier) Name() string { return "ntfy" }

func (n *NtfyNotifier) Send(title, body string, priority int, tags []string, markdown bool) error {
	if n.Topic == "" {
		return fmt.Errorf("未设置 ntfy_topic")
	}

	server := strings.TrimRight(n.Server, "/")
	pushURL := fmt.Sprintf("%s/%s", server, n.Topic)
	message := fmt.Sprintf("%s\n\n%s", title, body)

	req, err := http.NewRequest("POST", pushURL, strings.NewReader(message))
	if err != nil {
		return fmt.Errorf("构建请求失败: %w", err)
	}

	if markdown {
		req.Header.Set("Markdown", "yes")
	}
	req.Header.Set("Priority", fmt.Sprintf("%d", priority))
	if len(tags) > 0 {
		req.Header.Set("Tags", strings.Join(tags, ","))
	}
	if n.Token != "" {
		req.Header.Set("Authorization", "Bearer "+n.Token)
	}

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("推送失败: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 400 {
		return fmt.Errorf("推送失败: HTTP %d", resp.StatusCode)
	}

	n.logger.Printf("[推送成功] ntfy 通知已发送到 %s, 优先级: %d", pushURL, priority)
	return nil
}
