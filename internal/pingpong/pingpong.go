// 乒乓循环编排引擎：双 Agent 交替对话直至终止
// 通用能力：任意两个 Agent 交替运行，一个的输出作为另一个的输入
// 首个应用：专利挖掘（reader → writer 交替）
package pingpong

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/boss/multi-agent-orch/internal/adapter"
	"github.com/boss/multi-agent-orch/internal/config"
	"github.com/boss/multi-agent-orch/internal/db"
	"github.com/boss/multi-agent-orch/internal/summary"
)

// 状态常量，避免硬编码字符串拼写错误
const (
	StatusCreated   = "created"
	StatusRunning   = "running"
	StatusCompleted = "completed"
	StatusStopped   = "stopped"
	StatusError     = "error"
)

// PingPongEngine 乒乓循环编排引擎
type PingPongEngine struct {
	db      *db.DB
	adapter *adapter.CLIClient
	summary *summary.Service
	config  config.PingPongConfig
	mu      sync.RWMutex
	// 运行中的乒乓实例，用于支持手动停止
	activeRuns map[string]*activeRun
}

// activeRun 运行中的乒乓状态
type activeRun struct {
	cancel context.CancelFunc
}

// PingPongOverrides 创建运行时的自定义覆盖项
type PingPongOverrides struct {
	AgentAPrompt string
	AgentBPrompt string
	MaxRounds    int
	StopKeywords []string
}

// PingPongRunStatus 乒乓运行状态（对外展示）
type PingPongRunStatus struct {
	RunID        string   `json:"run_id"`
	Name         string   `json:"name"`
	Keywords     []string `json:"keywords"`
	AgentAName   string   `json:"agent_a_name"`
	AgentBName   string   `json:"agent_b_name"`
	Status       string   `json:"status"`
	CurrentRound int      `json:"current_round"`
	MaxRounds    int      `json:"max_rounds"`
	FinalOutput  string   `json:"final_output,omitempty"`
}

// NewPingPongEngine 创建乒乓循环引擎
func NewPingPongEngine(database *db.DB, cliAdapter *adapter.CLIClient, summarySvc *summary.Service, cfg config.PingPongConfig) *PingPongEngine {
	return &PingPongEngine{
		db:         database,
		adapter:    cliAdapter,
		summary:    summarySvc,
		config:     cfg,
		activeRuns: make(map[string]*activeRun),
	}
}

// CreateRun 创建乒乓循环运行实例
func (e *PingPongEngine) CreateRun(name string, keywords []string, overrides *PingPongOverrides) (*db.PingPongRun, error) {
	// 参数校验
	if len(keywords) == 0 {
		return nil, fmt.Errorf("at least one keyword is required")
	}

	runID := generatePingPongID()

	// 解析 prompt：自定义优先，否则用配置值，最终 fallback 到内置默认
	agentAPrompt := resolvePrompt(overrides, e.config)
	agentBPrompt := resolvePromptB(overrides, e.config)
	maxRounds := e.config.MaxRounds
	stopKeywords := e.config.StopKeywords

	if overrides != nil {
		if overrides.MaxRounds > 0 {
			maxRounds = overrides.MaxRounds
		}
		if len(overrides.StopKeywords) > 0 {
			stopKeywords = overrides.StopKeywords
		}
	}

	// name 为空时自动生成
	if name == "" {
		name = fmt.Sprintf("pingpong-%s", strings.Join(keywords, "-"))
	}

	run := &db.PingPongRun{
		ID:           runID,
		Name:         name,
		Keywords:     keywords,
		AgentAName:   e.config.AgentAName,
		AgentBName:   e.config.AgentBName,
		AgentAPrompt: agentAPrompt,
		AgentBPrompt: agentBPrompt,
		Status:       StatusCreated,
		MaxRounds:    maxRounds,
		StopKeywords: stopKeywords,
	}

	if err := e.db.CreatePingPongRun(run); err != nil {
		return nil, fmt.Errorf("save pingpong run: %w", err)
	}

	slog.Info("pingpong run created", "run_id", runID, "name", name, "keywords", keywords)
	return run, nil
}

// Run 执行乒乓循环：agentA → agentB → agentA → ... 直至终止
func (e *PingPongEngine) Run(ctx context.Context, runID string) error {
	// 原子性 CAS：只有 status != running 时才能抢到执行权，防止并发重复执行
	affected, err := e.db.CASPingPongStatus(runID, StatusRunning, 0)
	if err != nil {
		return fmt.Errorf("cas pingpong status: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("pingpong run %s is already running or does not exist", runID)
	}

	run, err := e.db.GetPingPongRun(runID)
	if err != nil {
		return fmt.Errorf("get pingpong run: %w", err)
	}

	// 注册 cancel 上下文
	ctx, cancel := context.WithCancel(ctx)
	e.mu.Lock()
	e.activeRuns[runID] = &activeRun{cancel: cancel}
	e.mu.Unlock()

	// 确保退出时清理
	defer func() {
		e.mu.Lock()
		delete(e.activeRuns, runID)
		e.mu.Unlock()
		cancel()
	}()

	// 执行乒乓循环
	return e.runLoop(ctx, run)
}

// runLoop 乒乓循环核心逻辑
func (e *PingPongEngine) runLoop(ctx context.Context, run *db.PingPongRun) error {
	// 第一轮：将关键词构建 prompt 发给 agentA
	firstPrompt := BuildFirstPrompt(run.Keywords)
	agentAPrompt := run.AgentAPrompt
	agentBPrompt := run.AgentBPrompt

	// agentA 的首次输入
	currentInput := firstPrompt
	currentRound := 0
	var finalOutput string

	for {
		// 检查上下文取消
		select {
		case <-ctx.Done():
			if err := e.db.UpdatePingPongStatus(run.ID, StatusStopped, currentRound); err != nil {
				slog.Error("failed to update status on cancel", "run_id", run.ID, "error", err)
			}
			slog.Info("pingpong run cancelled", "run_id", run.ID, "round", currentRound)
			return nil
		default:
		}

		// 检查最大轮次
		if currentRound >= run.MaxRounds {
			if err := e.db.UpdatePingPongStatus(run.ID, StatusCompleted, currentRound); err != nil {
				slog.Error("failed to update status on max rounds", "run_id", run.ID, "error", err)
			}
			slog.Info("pingpong run reached max rounds", "run_id", run.ID, "round", currentRound)
			return nil
		}

		// --- AgentA 轮次 ---
		currentRound++
		slog.Info("pingpong round agent_a", "run_id", run.ID, "round", currentRound, "agent", run.AgentAName)

		rawA, passA, err := e.callAgent(ctx, run.AgentAName, agentAPrompt, currentInput)
		if err != nil {
			if dbErr := e.db.UpdatePingPongStatus(run.ID, StatusError, currentRound); dbErr != nil {
				slog.Error("failed to update error status", "run_id", run.ID, "error", dbErr)
			}
			return fmt.Errorf("agent %s round %d: %w", run.AgentAName, currentRound, err)
		}

		slog.Info("pingpong agent_a output", "run_id", run.ID, "round", currentRound, "raw_len", len(rawA), "pass_len", len(passA))
		fmt.Printf("\n=== 第 %d 轮 [%s] ===\n%s\n", currentRound, run.AgentAName, passA)

		// 检查 agentA 输出是否包含终止关键词
		if ContainsStopKeyword(rawA, run.StopKeywords) {
			finalOutput = rawA
			break
		}

		// --- AgentB 轮次 ---
		slog.Info("pingpong round agent_b", "run_id", run.ID, "round", currentRound, "agent", run.AgentBName)

		rawB, passB, err := e.callAgent(ctx, run.AgentBName, agentBPrompt, passA)
		if err != nil {
			if dbErr := e.db.UpdatePingPongStatus(run.ID, StatusError, currentRound); dbErr != nil {
				slog.Error("failed to update error status", "run_id", run.ID, "error", dbErr)
			}
			return fmt.Errorf("agent %s round %d: %w", run.AgentBName, currentRound, err)
		}

		slog.Info("pingpong agent_b output", "run_id", run.ID, "round", currentRound, "raw_len", len(rawB), "pass_len", len(passB))
		fmt.Printf("\n=== 第 %d 轮 [%s] ===\n%s\n", currentRound, run.AgentBName, passB)

		// 检查 agentB 输出是否包含终止关键词
		if ContainsStopKeyword(rawB, run.StopKeywords) {
			finalOutput = rawB
			break
		}

		// 更新轮次到 DB
		if err := e.db.UpdatePingPongStatus(run.ID, StatusRunning, currentRound); err != nil {
			slog.Error("failed to update running status", "run_id", run.ID, "error", err)
		}

		// 下一轮：agentB 的摘要输出作为 agentA 的输入
		currentInput = passB
	}

	// 循环结束，保存最终输出和状态（使用事务保证原子性）
	if err := e.db.CompletePingPongRun(run.ID, finalOutput, currentRound); err != nil {
		slog.Error("failed to complete pingpong run", "run_id", run.ID, "error", err)
	}

	slog.Info("pingpong run completed", "run_id", run.ID, "round", currentRound)
	return nil
}

// callAgent 调用单个 Agent（CLI 进程）获取完整响应
// 返回 (原文, 传递版本, error)：原文用于最终输出保存，传递版本（可能摘要后）用于下一轮输入
func (e *PingPongEngine) callAgent(ctx context.Context, agentName string, systemPrompt string, userPrompt string) (raw string, pass string, err error) {
	// 将 system prompt 作为历史上下文注入
	ch, promptErr := e.adapter.Prompt(ctx, userPrompt, systemPrompt)
	if promptErr != nil {
		return "", "", fmt.Errorf("prompt agent %s: %w", agentName, promptErr)
	}

	// 收集完整响应，同时检查 context 取消
	var fullResponse strings.Builder
	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				// channel 关闭，响应收集完毕
				goto done
			}
			if msg.Type == adapter.TypeAssistant {
				text := adapter.ExtractText(msg)
				if text != "" {
					fullResponse.WriteString(text)
				}
			}
		case <-ctx.Done():
			slog.Info("agent call cancelled", "agent", agentName)
			return "", "", ctx.Err()
		}
	}

done:
	output := fullResponse.String()
	if output == "" {
		return "", "", fmt.Errorf("agent %s returned empty response", agentName)
	}

	// 摘要：如果输出过长且摘要服务可用，生成摘要版本用于传递
	// 原文始终保留，用于最终输出保存
	if e.summary.IsEnabled() && len(output) > 4096 {
		summaryText, summaryErr := e.summary.Summarize(ctx, output)
		if summaryErr != nil {
			slog.Error("summary failed, using original", "error", summaryErr)
			return output, output, nil
		}
		// 原文用于保存，摘要用于传递
		return output, summaryText, nil
	}

	return output, output, nil
}

// GetRunStatus 获取乒乓运行状态
func (e *PingPongEngine) GetRunStatus(runID string) (*PingPongRunStatus, error) {
	run, err := e.db.GetPingPongRun(runID)
	if err != nil {
		return nil, fmt.Errorf("get pingpong run: %w", err)
	}
	return &PingPongRunStatus{
		RunID:        run.ID,
		Name:         run.Name,
		Keywords:     run.Keywords,
		AgentAName:   run.AgentAName,
		AgentBName:   run.AgentBName,
		Status:       run.Status,
		CurrentRound: run.CurrentRound,
		MaxRounds:    run.MaxRounds,
		FinalOutput:  run.FinalOutput,
	}, nil
}

// ListRuns 列出所有乒乓运行
func (e *PingPongEngine) ListRuns() ([]*db.PingPongRun, error) {
	return e.db.ListPingPongRuns()
}

// StopRun 停止乒乓运行
func (e *PingPongEngine) StopRun(runID string) error {
	e.mu.Lock()
	if active, exists := e.activeRuns[runID]; exists {
		active.cancel()
	}
	e.mu.Unlock()

	// 获取当前轮次以保留进度
	run, err := e.db.GetPingPongRun(runID)
	round := 0
	if err == nil {
		round = run.CurrentRound
	}

	if err := e.db.UpdatePingPongStatus(runID, StatusStopped, round); err != nil {
		slog.Error("failed to update pingpong status on stop", "run_id", runID, "error", err)
	}

	slog.Info("pingpong run stopped", "run_id", runID)
	return nil
}

// DeleteRun 删除乒乓运行记录
func (e *PingPongEngine) DeleteRun(runID string) error {
	// 先停止运行中的实例
	e.StopRun(runID)
	return e.db.DeletePingPongRun(runID)
}

// resolvePrompt 解析 agentA 的 prompt：自定义 > 配置 > 默认
func resolvePrompt(overrides *PingPongOverrides, cfg config.PingPongConfig) string {
	if overrides != nil && overrides.AgentAPrompt != "" {
		return overrides.AgentAPrompt
	}
	return GetReaderPrompt(cfg.AgentAPrompt)
}

// resolvePromptB 解析 agentB 的 prompt：自定义 > 配置 > 默认
func resolvePromptB(overrides *PingPongOverrides, cfg config.PingPongConfig) string {
	if overrides != nil && overrides.AgentBPrompt != "" {
		return overrides.AgentBPrompt
	}
	return GetWriterPrompt(cfg.AgentBPrompt)
}

var ppIDCounter int64

// generatePingPongID 生成乒乓运行 ID（时间戳 + 原子计数器，进程重启不冲突）
func generatePingPongID() string {
	ts := time.Now().UnixMilli() % 100000000 // 取后8位
	id := atomic.AddInt64(&ppIDCounter, 1)
	return fmt.Sprintf("pp-%d-%d", ts, id)
}
