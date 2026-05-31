package adapter

import (
	"context"
	"testing"
	"time"
)

func TestBuildPromptNoHistory(t *testing.T) {
	client := NewCLIClient("echo", "glm-5.1", ".")
	result := client.buildPrompt("hello", "")
	if result != "hello" {
		t.Errorf("expected 'hello', got %s", result)
	}
}

func TestBuildPromptWithHistory(t *testing.T) {
	client := NewCLIClient("echo", "glm-5.1", ".")
	result := client.buildPrompt("write code", "previous discussion about API design")
	if !contains(result, "历史上下文摘要") {
		t.Error("expected history header in prompt")
	}
	if !contains(result, "previous discussion about API design") {
		t.Error("expected history content in prompt")
	}
	if !contains(result, "write code") {
		t.Error("expected original prompt in result")
	}
}

func TestExtractText(t *testing.T) {
	msg := StreamMessage{
		Type: TypeAssistant,
		Message: &AssistantMessage{
			Content: []ContentBlock{
				{Type: "text", Text: "hello"},
				{Type: "text", Text: "world"},
			},
		},
	}
	text := ExtractText(msg)
	if text != "hello\nworld" {
		t.Errorf("expected 'hello\\nworld', got %s", text)
	}
}

func TestExtractTextNonAssistant(t *testing.T) {
	msg := StreamMessage{Type: TypeSystem}
	text := ExtractText(msg)
	if text != "" {
		t.Errorf("expected empty string, got %s", text)
	}
}

func TestExtractResult(t *testing.T) {
	msg := StreamMessage{
		Type:   TypeResult,
		Result: "task completed",
	}
	result := ExtractResult(msg)
	if result != "task completed" {
		t.Errorf("expected 'task completed', got %s", result)
	}
}

func TestExtractResultNonResult(t *testing.T) {
	msg := StreamMessage{Type: TypeAssistant}
	result := ExtractResult(msg)
	if result != "" {
		t.Errorf("expected empty string, got %s", result)
	}
}

func TestPromptWithEchoMock(t *testing.T) {
	// 使用 echo 模拟 CLI 输出
	client := NewCLIClient("echo", "glm-5.1", ".")

	// echo 会原样输出，不是 stream-json，所以解析会失败
	// 这个测试验证流程不崩溃
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, err := client.Prompt(ctx, "test", "")
	if err != nil {
		t.Fatalf("prompt error: %v", err)
	}

	// 读取通道直到关闭
	for range ch {
		// 消耗所有消息
	}
}

func TestPromptCancellation(t *testing.T) {
	client := NewCLIClient("sleep", "glm-5.1", ".")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	_, err := client.Prompt(ctx, "test", "")
	// context 已取消应返回错误
	if err == nil {
		t.Error("expected error with cancelled context")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
