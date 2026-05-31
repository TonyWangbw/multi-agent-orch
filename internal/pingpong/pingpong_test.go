// 乒乓循环引擎单元测试
package pingpong

import (
	"strings"
	"testing"
)

// --- Prompt 相关测试 ---

func TestGetReaderPromptDefault(t *testing.T) {
	prompt := GetReaderPrompt("")
	if prompt == "" {
		t.Fatal("default reader prompt should not be empty")
	}
	if !strings.Contains(prompt, "专利点挖掘") {
		t.Error("default reader prompt should contain '专利点挖掘'")
	}
}

func TestGetReaderPromptCustom(t *testing.T) {
	custom := "自定义 reader prompt"
	prompt := GetReaderPrompt(custom)
	if prompt != custom {
		t.Errorf("expected custom prompt, got: %s", prompt)
	}
}

func TestGetWriterPromptDefault(t *testing.T) {
	prompt := GetWriterPrompt("")
	if prompt == "" {
		t.Fatal("default writer prompt should not be empty")
	}
	if !strings.Contains(prompt, "专利评审") {
		t.Error("default writer prompt should contain '专利评审'")
	}
	// 验证终止关键词提示存在
	if !strings.Contains(prompt, "专利书编写完成") {
		t.Error("default writer prompt should contain stop keyword instruction")
	}
}

func TestGetWriterPromptCustom(t *testing.T) {
	custom := "自定义 writer prompt"
	prompt := GetWriterPrompt(custom)
	if prompt != custom {
		t.Errorf("expected custom prompt, got: %s", prompt)
	}
}

func TestBuildFirstPrompt(t *testing.T) {
	keywords := []string{"AI", "机器学习", "自然语言处理"}
	prompt := BuildFirstPrompt(keywords)
	if !strings.Contains(prompt, "AI") {
		t.Error("prompt should contain 'AI'")
	}
	if !strings.Contains(prompt, "机器学习") {
		t.Error("prompt should contain '机器学习'")
	}
	if !strings.Contains(prompt, "自然语言处理") {
		t.Error("prompt should contain '自然语言处理'")
	}
}

func TestBuildFirstPromptSingle(t *testing.T) {
	keywords := []string{"深度学习"}
	prompt := BuildFirstPrompt(keywords)
	if !strings.Contains(prompt, "深度学习") {
		t.Error("prompt should contain '深度学习'")
	}
}

func TestBuildFirstPromptEmpty(t *testing.T) {
	prompt := BuildFirstPrompt(nil)
	if prompt == "" {
		t.Error("should still generate prompt even with empty keywords")
	}
}

func TestContainsStopKeyword(t *testing.T) {
	stopKeywords := []string{"专利书编写完成", "PATENT_WRITING_DONE"}

	tests := []struct {
		name     string
		output   string
		expected bool
	}{
		{
			name:     "contains Chinese stop keyword",
			output:   "这是专利书内容\n专利书编写完成",
			expected: true,
		},
		{
			name:     "contains English stop keyword",
			output:   "Patent writing done\nPATENT_WRITING_DONE",
			expected: true,
		},
		{
			name:     "no stop keyword",
			output:   "需要补充更多专利点",
			expected: false,
		},
		{
			name:     "stop keyword in middle",
			output:   "前面内容 专利书编写完成 后面内容",
			expected: true,
		},
		{
			name:     "empty output",
			output:   "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ContainsStopKeyword(tt.output, stopKeywords)
			if result != tt.expected {
				t.Errorf("ContainsStopKeyword(%q) = %v, want %v", tt.output, result, tt.expected)
			}
		})
	}
}

func TestContainsStopKeywordEmptyList(t *testing.T) {
	result := ContainsStopKeyword("任意内容", nil)
	if result {
		t.Error("should return false for empty stop keywords list")
	}
}

// --- ID 生成测试 ---

func TestGeneratePingPongID(t *testing.T) {
	id1 := generatePingPongID()
	id2 := generatePingPongID()

	if id1 == id2 {
		t.Errorf("generated IDs should be unique: %s == %s", id1, id2)
	}
	if !strings.Contains(id1, "pp-") {
		t.Errorf("ID should have 'pp-' prefix: %s", id1)
	}
}

// --- 状态常量测试 ---

func TestStatusConstants(t *testing.T) {
	tests := []struct {
		name     string
		constant string
		value    string
	}{
		{"created", StatusCreated, "created"},
		{"running", StatusRunning, "running"},
		{"completed", StatusCompleted, "completed"},
		{"stopped", StatusStopped, "stopped"},
		{"error", StatusError, "error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.constant != tt.value {
				t.Errorf("expected %s, got %s", tt.value, tt.constant)
			}
		})
	}
}
