package summary

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTruncateContent(t *testing.T) {
	tests := []struct {
		content  string
		maxChars int
		want     string
	}{
		{"hello", 10, "hello"},
		{"hello world this is a long message", 11, "hello world\n...[截断]"},
		{"short", 100, "short"},
	}

	for _, tt := range tests {
		got := truncateContent(tt.content, tt.maxChars)
		if got != tt.want {
			t.Errorf("truncateContent(%q, %d) = %q, want %q", tt.content, tt.maxChars, got, tt.want)
		}
	}
}

func TestSummarizeDisabled(t *testing.T) {
	svc := NewService(Config{
		Enabled:   false,
		MaxTokens: 128,
	})

	result, err := svc.Summarize(context.Background(), "long content that should be truncated because summary is disabled")
	if err != nil {
		t.Fatalf("summarize: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty result")
	}
}

func TestSummarizeNoAPIKey(t *testing.T) {
	svc := NewService(Config{
		Enabled:   true,
		APIKey:    "",
		APIBase:   "https://example.com",
		Model:     "glm-5.1",
		MaxTokens: 128,
	})

	result, err := svc.Summarize(context.Background(), "content without API key")
	if err != nil {
		t.Fatalf("summarize: %v", err)
	}
	// 无 API Key 时降级为截断
	if result == "" {
		t.Error("expected non-empty result from truncation fallback")
	}
}

func TestSummarizeWithMockServer(t *testing.T) {
	// 创建模拟 LLM API 服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 验证请求头
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("expected Bearer test-key, got %s", r.Header.Get("Authorization"))
		}

		// 返回模拟响应
		resp := `{
			"choices": [{
				"message": {
					"content": "这是摘要内容"
				}
			}],
			"usage": {
				"prompt_tokens": 100,
				"completion_tokens": 20
			}
		}`
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(resp))
	}))
	defer server.Close()

	svc := NewService(Config{
		Enabled:   true,
		APIKey:    "test-key",
		APIBase:   server.URL,
		Model:     "glm-5.1",
		MaxTokens: 512,
	})

	result, err := svc.Summarize(context.Background(), "这是一段需要摘要的长内容")
	if err != nil {
		t.Fatalf("summarize: %v", err)
	}
	if result != "这是摘要内容" {
		t.Errorf("expected '这是摘要内容', got %s", result)
	}
}

func TestSummarizeServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": {"message": "server error"}}`))
	}))
	defer server.Close()

	svc := NewService(Config{
		Enabled:   true,
		APIKey:    "test-key",
		APIBase:   server.URL,
		Model:     "glm-5.1",
		MaxTokens: 512,
	})

	result, err := svc.Summarize(context.Background(), "content when server errors")
	if err != nil {
		t.Fatalf("summarize should not error on server failure: %v", err)
	}
	// 服务器错误时降级为截断
	if result == "" {
		t.Error("expected non-empty result from truncation fallback")
	}
}

func TestSummarizeAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := `{
			"choices": [],
			"error": {"message": "rate limit exceeded"}
		}`
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(resp))
	}))
	defer server.Close()

	svc := NewService(Config{
		Enabled:   true,
		APIKey:    "test-key",
		APIBase:   server.URL,
		Model:     "glm-5.1",
		MaxTokens: 512,
	})

	result, err := svc.Summarize(context.Background(), "content when API errors")
	if err != nil {
		t.Fatalf("summarize should not error: %v", err)
	}
	// API 错误时降级为截断
	if result == "" {
		t.Error("expected non-empty result from truncation fallback")
	}
}

func TestIsEnabled(t *testing.T) {
	svc1 := NewService(Config{Enabled: true})
	if !svc1.IsEnabled() {
		t.Error("expected enabled")
	}

	svc2 := NewService(Config{Enabled: false})
	if svc2.IsEnabled() {
		t.Error("expected disabled")
	}
}
