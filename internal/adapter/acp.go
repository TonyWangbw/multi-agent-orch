// CLI 适配器：通过 --print --output-format stream-json 模式与 codebuddy-cli 通信
// 每轮调用独立进程，编排服务负责注入历史上下文摘要
package adapter

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"
)

// MessageType stream-json 输出消息类型
type MessageType string

const (
	TypeSystem   MessageType = "system"
	TypeAssistant MessageType = "assistant"
	TypeResult   MessageType = "result"
	TypeError    MessageType = "error"
)

// StreamMessage stream-json 格式的输出消息
type StreamMessage struct {
	Type      MessageType `json:"type"`
	Subtype   string      `json:"subtype,omitempty"`
	UUID      string      `json:"uuid,omitempty"`
	SessionID string      `json:"session_id,omitempty"`

	// assistant 类型消息
	Message *AssistantMessage `json:"message,omitempty"`

	// result 类型消息
	Result    interface{} `json:"result,omitempty"`
	IsError   bool        `json:"is_error,omitempty"`
	DurationMs int64      `json:"duration_ms,omitempty"`

	// usage
	Usage *UsageInfo `json:"usage,omitempty"`

	// 原始时间戳
	Timestamp string `json:"__timestamp,omitempty"`
}

// AssistantMessage 助手回复消息
type AssistantMessage struct {
	ID      string        `json:"id"`
	Content []ContentBlock `json:"content"`
	Model   string        `json:"model"`
	Role    string        `json:"role"`
}

// ContentBlock 消息内容块
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// UsageInfo token 用量信息
type UsageInfo struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// CLIClient 管理与 codebuddy-cli 的通信
type CLIClient struct {
	binary  string
	model   string
	workDir string
}

// NewCLIClient 创建 CLI 客户端
// binary 参数应为简单命令名（不含路径分隔符），防止命令注入
func NewCLIClient(binary string, model string, workDir string) *CLIClient {
	if strings.Contains(binary, "/") || strings.Contains(binary, "\\") {
		slog.Warn("cli binary should be a simple name without path separators", "binary", binary)
	}
	return &CLIClient{
		binary:  binary,
		model:   model,
		workDir: workDir,
	}
}

// Prompt 向 CLI 发送单轮请求，返回流式消息通道
// historySummary 为历史上下文摘要，由编排服务注入
func (c *CLIClient) Prompt(ctx context.Context, prompt string, historySummary string) (<-chan StreamMessage, error) {
	// 构建完整 prompt：注入历史上下文
	fullPrompt := c.buildPrompt(prompt, historySummary)

	cmd := exec.CommandContext(ctx, c.binary,
		"--print",
		"--output-format", "stream-json",
		"--model", c.model,
		"--dangerously-skip-permissions",
	)
	cmd.Dir = c.workDir
	cmd.Stdin = strings.NewReader(fullPrompt + "\n")

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("create stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start cli process: %w", err)
	}

	slog.Info("cli process started",
		"pid", cmd.Process.Pid,
		"model", c.model,
	)

	ch := make(chan StreamMessage, 64)

	// 后台读取 stderr 用于日志
	go c.readStderr(stderr, cmd.Process.Pid)

	// 后台读取 stdout 并解析 stream-json
	go func() {
		defer close(ch)
		defer func() {
			if err := cmd.Wait(); err != nil {
				slog.Error("cli process wait error", "pid", cmd.Process.Pid, "error", err)
			}
		}()

		scanner := bufio.NewScanner(stdout)
		// 增大缓冲区以支持大消息
		scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}

			var msg StreamMessage
			if err := json.Unmarshal([]byte(line), &msg); err != nil {
				slog.Warn("failed to parse stream-json line",
					"pid", cmd.Process.Pid,
					"error", err,
					"line_len", len(line),
				)
				continue
			}

			select {
			case ch <- msg:
			case <-ctx.Done():
				slog.Info("cli output context cancelled", "pid", cmd.Process.Pid)
				return
			}
		}

		if err := scanner.Err(); err != nil {
			slog.Error("cli stdout scanner error", "pid", cmd.Process.Pid, "error", err)
		}
	}()

	return ch, nil
}

// buildPrompt 构建包含历史上下文的完整 prompt
func (c *CLIClient) buildPrompt(prompt string, historySummary string) string {
	if historySummary == "" {
		return prompt
	}

	var sb strings.Builder
	sb.WriteString("--- 历史上下文摘要 ---\n")
	sb.WriteString(historySummary)
	sb.WriteString("\n--- 历史上下文结束 ---\n\n")
	sb.WriteString(prompt)
	return sb.String()
}

// readStderr 读取 stderr 并记录日志
func (c *CLIClient) readStderr(stderr io.ReadCloser, pid int) {
	scanner := bufio.NewScanner(stderr)
	for scanner.Scan() {
		slog.Debug("cli stderr", "pid", pid, "line", scanner.Text())
	}
}

// ExtractText 从 assistant 类型消息中提取文本内容
func ExtractText(msg StreamMessage) string {
	if msg.Type != TypeAssistant || msg.Message == nil {
		return ""
	}

	var texts []string
	for _, block := range msg.Message.Content {
		if block.Type == "text" {
			texts = append(texts, block.Text)
		}
	}
	return strings.Join(texts, "\n")
}

// ExtractResult 从 result 类型消息中提取结果文本
func ExtractResult(msg StreamMessage) string {
	if msg.Type != TypeResult {
		return ""
	}
	if str, ok := msg.Result.(string); ok {
		return str
	}
	return fmt.Sprintf("%v", msg.Result)
}
