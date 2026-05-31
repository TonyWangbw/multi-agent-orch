// LLM 摘要服务：调用 LLM API 对 CLI 输出做摘要，支持降级截断
package summary

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// Service 摘要服务
type Service struct {
	apiKey    string
	apiBase   string
	model     string
	maxTokens int
	client    *http.Client
	enabled   bool
}

// Config 摘要服务配置
type Config struct {
	Enabled   bool
	APIKey    string
	APIBase   string
	Model     string
	MaxTokens int
}

// NewService 创建摘要服务
func NewService(cfg Config) *Service {
	return &Service{
		apiKey:    cfg.APIKey,
		apiBase:   cfg.APIBase,
		model:     cfg.Model,
		maxTokens: cfg.MaxTokens,
		enabled:   cfg.Enabled,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// chatRequest LLM API 请求格式
type chatRequest struct {
	Model     string        `json:"model"`
	Messages  []chatMessage `json:"messages"`
	MaxTokens int           `json:"max_tokens,omitempty"`
}

// chatMessage LLM API 消息格式
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatResponse LLM API 响应格式
type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Summarize 对内容做摘要
// 若摘要禁用或 API 不可用，降级为截断原文
func (s *Service) Summarize(ctx context.Context, content string) (string, error) {
	if !s.enabled {
		slog.Debug("summary disabled, truncating content")
		return truncateContent(content, s.maxTokens*4), nil
	}

	if s.apiKey == "" {
		slog.Warn("no API key configured, falling back to truncation")
		return truncateContent(content, s.maxTokens*4), nil
	}

	summary, err := s.callLLM(ctx, content)
	if err != nil {
		slog.Error("LLM summary failed, falling back to truncation", "error", err)
		return truncateContent(content, s.maxTokens*4), nil
	}

	return summary, nil
}

// callLLM 调用 LLM API
func (s *Service) callLLM(ctx context.Context, content string) (string, error) {
	reqBody := chatRequest{
		Model: s.model,
		Messages: []chatMessage{
			{
				Role:    "system",
				Content: "你是一个消息摘要助手。请将以下 AI 智能体的对话输出压缩为简洁摘要，保留关键信息、决策和行动项。摘要应让另一个智能体能够理解上下文并继续工作。",
			},
			{
				Role:    "user",
				Content: fmt.Sprintf("请摘要以下内容：\n\n%s", content),
			},
		},
		MaxTokens: s.maxTokens,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	url := strings.TrimRight(s.apiBase, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.apiKey)

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var chatResp chatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	if chatResp.Error != nil {
		return "", fmt.Errorf("API error: %s", chatResp.Error.Message)
	}

	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}

	slog.Info("summary generated",
		"model", s.model,
		"prompt_tokens", chatResp.Usage.PromptTokens,
		"completion_tokens", chatResp.Usage.CompletionTokens,
	)

	return chatResp.Choices[0].Message.Content, nil
}

// truncateContent 截断内容到指定字符数
func truncateContent(content string, maxChars int) string {
	if len(content) <= maxChars {
		return content
	}
	return content[:maxChars] + "\n...[截断]"
}

// IsEnabled 返回摘要是否启用
func (s *Service) IsEnabled() bool {
	return s.enabled
}
