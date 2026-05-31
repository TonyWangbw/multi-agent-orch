package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.DBPath != "./data/orchestrator.db" {
		t.Errorf("expected default db_path, got %s", cfg.DBPath)
	}
	if !cfg.SummaryEnabled {
		t.Error("expected summary_enabled true")
	}
	if cfg.SummaryModel != "glm-5.1" {
		t.Errorf("expected glm-5.1, got %s", cfg.SummaryModel)
	}
	if cfg.SummaryMaxTokens != 512 {
		t.Errorf("expected 512, got %d", cfg.SummaryMaxTokens)
	}
	if cfg.MaxCLIInstances != 10 {
		t.Errorf("expected 10, got %d", cfg.MaxCLIInstances)
	}
	if cfg.CLIBinary != "codebuddy" {
		t.Errorf("expected codebuddy, got %s", cfg.CLIBinary)
	}
}

func TestLoadFromFile(t *testing.T) {
	content := `
db_path: /tmp/test.db
summary_enabled: false
summary_model: gpt-4
max_cli_instances: 5
log_level: debug
`
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	os.WriteFile(cfgPath, []byte(content), 0644)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.DBPath != "/tmp/test.db" {
		t.Errorf("expected /tmp/test.db, got %s", cfg.DBPath)
	}
	if cfg.SummaryEnabled {
		t.Error("expected summary_enabled false")
	}
	if cfg.SummaryModel != "gpt-4" {
		t.Errorf("expected gpt-4, got %s", cfg.SummaryModel)
	}
	if cfg.MaxCLIInstances != 5 {
		t.Errorf("expected 5, got %d", cfg.MaxCLIInstances)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("expected debug, got %s", cfg.LogLevel)
	}
	// 未覆盖的字段应保留默认值
	if cfg.SummaryMaxTokens != 512 {
		t.Errorf("expected default 512, got %d", cfg.SummaryMaxTokens)
	}
}

func TestLoadNonexistentFile(t *testing.T) {
	cfg, err := Load("/nonexistent/config.yaml")
	if err != nil {
		t.Fatalf("load nonexistent config should not error: %v", err)
	}

	// 应返回默认配置
	if cfg.SummaryModel != "glm-5.1" {
		t.Errorf("expected default glm-5.1, got %s", cfg.SummaryModel)
	}
}

func TestLoadPartialConfig(t *testing.T) {
	content := `summary_enabled: false`
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	os.WriteFile(cfgPath, []byte(content), 0644)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("load partial config: %v", err)
	}

	if cfg.SummaryEnabled {
		t.Error("expected summary_enabled false")
	}
	// 其他字段保持默认
	if cfg.SummaryModel != "glm-5.1" {
		t.Errorf("expected default glm-5.1, got %s", cfg.SummaryModel)
	}
}

func TestGetSummaryAPIKey(t *testing.T) {
	cfg := DefaultConfig()

	// 未设置环境变量
	if key := cfg.GetSummaryAPIKey(); key != "" {
		t.Errorf("expected empty key, got %s", key)
	}

	// 设置环境变量
	os.Setenv("SUMMARY_API_KEY", "test-key-123")
	defer os.Unsetenv("SUMMARY_API_KEY")

	if key := cfg.GetSummaryAPIKey(); key != "test-key-123" {
		t.Errorf("expected test-key-123, got %s", key)
	}
}

func TestGetSummaryAPIKeyCustomEnv(t *testing.T) {
	content := `summary_api_key_env: MY_CUSTOM_KEY`
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	os.WriteFile(cfgPath, []byte(content), 0644)

	cfg, _ := Load(cfgPath)

	os.Setenv("MY_CUSTOM_KEY", "custom-value")
	defer os.Unsetenv("MY_CUSTOM_KEY")

	if key := cfg.GetSummaryAPIKey(); key != "custom-value" {
		t.Errorf("expected custom-value, got %s", key)
	}
}
