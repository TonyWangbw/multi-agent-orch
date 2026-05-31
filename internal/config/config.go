// 配置加载：从 config.yaml 读取编排服务配置
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config 编排服务全局配置
type Config struct {
	// 数据库
	DBPath string `yaml:"db_path"`

	// 摘要配置
	SummaryEnabled   bool   `yaml:"summary_enabled"`
	SummaryModel     string `yaml:"summary_model"`
	SummaryMaxTokens int    `yaml:"summary_max_tokens"`
	SummaryAPIKeyEnv string `yaml:"summary_api_key_env"`
	SummaryAPIBase   string `yaml:"summary_api_base"`

	// 进程管理
	MaxCLIInstances int    `yaml:"max_cli_instances"`
	CLIBinary       string `yaml:"cli_binary"`

	// 日志
	LogLevel string `yaml:"log_level"`
	LogFile  string `yaml:"log_file"`

	// 乒乓循环编排
	PingPong PingPongConfig `yaml:"pingpong"`
}

// PingPongConfig 乒乓循环编排配置
type PingPongConfig struct {
	MaxRounds    int      `yaml:"max_rounds"`
	StopKeywords []string `yaml:"stop_keywords"`
	AgentAName   string   `yaml:"agent_a_name"`
	AgentBName   string   `yaml:"agent_b_name"`
	AgentAPrompt string   `yaml:"agent_a_prompt"`
	AgentBPrompt string   `yaml:"agent_b_prompt"`
}

// DefaultConfig 返回默认配置
func DefaultConfig() *Config {
	return &Config{
		DBPath:           "./data/orchestrator.db",
		SummaryEnabled:   true,
		SummaryModel:     "glm-5.1",
		SummaryMaxTokens: 512,
		SummaryAPIKeyEnv: "SUMMARY_API_KEY",
		SummaryAPIBase:   "https://open.bigmodel.cn/api/paas/v4",
		MaxCLIInstances:  10,
		CLIBinary:        "codebuddy",
		LogLevel:         "info",
		LogFile:          "./logs/orch.log",
		PingPong: PingPongConfig{
			MaxRounds:    100,
			StopKeywords: []string{"专利书编写完成", "PATENT_WRITING_DONE"},
			AgentAName:   "reader",
			AgentBName:   "writer",
			AgentAPrompt: "", // 空则使用内置默认 prompt
			AgentBPrompt: "", // 空则使用内置默认 prompt
		},
	}
}

// Load 从 YAML 文件加载配置，缺失字段用默认值填充
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// 配置文件不存在时使用默认值
			return cfg, nil
		}
		return nil, fmt.Errorf("read config file %s: %w", path, err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config yaml: %w", err)
	}

	return cfg, nil
}

// GetSummaryAPIKey 从环境变量获取 LLM API Key
func (c *Config) GetSummaryAPIKey() string {
	return os.Getenv(c.SummaryAPIKeyEnv)
}
