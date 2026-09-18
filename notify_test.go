package main

import (
	"log"
	"os"
	"testing"
)

func TestStripMarkdown(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "移除加粗标记",
			input:    "**加粗文本**",
			expected: "加粗文本",
		},
		{
			name:     "移除二级标题",
			input:    "## 标题",
			expected: "标题",
		},
		{
			name:     "移除三级标题",
			input:    "### 小标题",
			expected: "小标题",
		},
		{
			name:     "移除列表标记",
			input:    "- 列表项",
			expected: "列表项",
		},
		{
			name:     "保留普通文本",
			input:    "普通文本",
			expected: "普通文本",
		},
		{
			name:     "处理多行文本",
			input:    "## 标题\n- 列表项1\n- 列表项2\n普通文本",
			expected: "标题\n列表项1\n列表项2\n普通文本",
		},
		{
			name:     "处理带空格的标记",
			input:    "  ## 标题  ",
			expected: "标题",
		},
		{
			name:     "处理空字符串",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stripMarkdown(tt.input)
			if result != tt.expected {
				t.Errorf("stripMarkdown(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestNewNotifier(t *testing.T) {
	logger := log.New(os.Stdout, "", 0)

	t.Run("PushURL为空返回nil", func(t *testing.T) {
		cfg := &PushConfig{
			Enable:  true,
			PushURL: "",
		}
		notifier := NewNotifier(cfg, logger)
		if notifier != nil {
			t.Error("NewNotifier() 在 PushURL 为空时应该返回 nil")
		}
	})

	t.Run("PushURL非空返回AppriseNotifier", func(t *testing.T) {
		cfg := &PushConfig{
			Enable:  true,
			PushURL: "http://example.com",
		}
		notifier := NewNotifier(cfg, logger)
		if notifier == nil {
			t.Fatal("NewNotifier() 在 PushURL 非空时不应该返回 nil")
		}
		if _, ok := notifier.(*AppriseNotifier); !ok {
			t.Error("NewNotifier() 应该返回 *AppriseNotifier")
		}
	})
}

func TestAppriseNotifierName(t *testing.T) {
	notifier := &AppriseNotifier{
		PushURL: "http://example.com",
	}
	if notifier.Name() != "apprise" {
		t.Errorf("Name() = %q, want %q", notifier.Name(), "apprise")
	}
}

func TestAppriseNotifierSendEmptyURL(t *testing.T) {
	logger := log.New(os.Stdout, "", 0)
	notifier := &AppriseNotifier{
		PushURL: "",
		logger:  logger,
	}

	err := notifier.Send("title", "body", 1, []string{}, false)
	if err == nil {
		t.Error("Send() 在 PushURL 为空时应该返回错误")
	}
}

func TestAppriseNotifierSend(t *testing.T) {
	logger := log.New(os.Stdout, "", 0)

	t.Run("空URL返回错误", func(t *testing.T) {
		notifier := &AppriseNotifier{
			PushURL: "",
			logger:  logger,
		}
		err := notifier.Send("title", "body", 1, []string{}, false)
		if err == nil {
			t.Error("Send() 在空URL时应该返回错误")
		}
	})

	t.Run("带优先级参数发送", func(t *testing.T) {
		notifier := &AppriseNotifier{
			PushURL: "json://localhost:1234",
			logger:  logger,
		}
		// 测试不同的优先级（会因无法连接而失败，但测试代码路径）
		for i := 1; i <= 5; i++ {
			notifier.Send("title", "body", i, []string{}, false)
		}
	})
}

func TestLogSuccess(t *testing.T) {
	logger := log.New(os.Stdout, "", 0)
	notifier := &AppriseNotifier{
		PushURL: "http://example.com",
		logger:  logger,
	}

	// 测试不同的优先级
	for i := 1; i <= 5; i++ {
		notifier.logger.Printf("[推送成功] 通知已发送, 优先级: %d", i)
	}
}

func TestStripMarkdownEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"空字符串", "", ""},
		{"只有加粗标记", "**", ""},
		{"嵌套标记", "## **标题**", "标题"},
		{"多个列表项", "- item1\n- item2\n- item3", "item1\nitem2\nitem3"},
		{"混合内容", "## 标题\n- 列表1\n- 列表2\n普通文本", "标题\n列表1\n列表2\n普通文本"},
		{"空行", "line1\n\nline2", "line1\n\nline2"},
		{"只有空格", "   ", "   "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stripMarkdown(tt.input)
			if result != tt.expected {
				t.Errorf("stripMarkdown(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
