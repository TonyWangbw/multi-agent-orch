// 流程引擎：编排流程的核心逻辑
// 负责流程创建、节点管理、消息路由、状态机和终端流式输出
package engine

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/boss/multi-agent-orch/internal/adapter"
	"github.com/boss/multi-agent-orch/internal/db"
	"github.com/boss/multi-agent-orch/internal/summary"
)

// Engine 编排流程引擎
type Engine struct {
	db      *db.DB
	adapter *adapter.CLIClient
	summary *summary.Service
	mu      sync.RWMutex
	// 运行中的流程状态
	activeFlows map[string]*ActiveFlow
}

// ActiveFlow 运行中的流程状态
type ActiveFlow struct {
	FlowID    string
	Cancel    context.CancelFunc
	NodeMsgs  map[string]string // nodeID -> 最近一次历史摘要
}

// New 创建流程引擎
func New(database *db.DB, cliAdapter *adapter.CLIClient, summarySvc *summary.Service) *Engine {
	return &Engine{
		db:          database,
		adapter:     cliAdapter,
		summary:     summarySvc,
		activeFlows: make(map[string]*ActiveFlow),
	}
}

// CreateFlow 创建编排流程
func (e *Engine) CreateFlow(name string, nodes []string, edges []db.Edge) (*db.FlowDefinition, error) {
	flowID := generateID("flow")

	flow := &db.FlowDefinition{
		ID:     flowID,
		Name:   name,
		Edges:  edges,
		Status: "created",
	}

	if err := e.db.CreateFlow(flow); err != nil {
		return nil, fmt.Errorf("save flow: %w", err)
	}

	// 创建节点
	for _, nodeName := range nodes {
		nodeID := generateID("node")
		node := &db.FlowNode{
			ID:     nodeID,
			FlowID: flowID,
			Name:   nodeName,
			Status: "idle",
		}
		if err := e.db.CreateNode(node); err != nil {
			return nil, fmt.Errorf("save node %s: %w", nodeName, err)
		}
	}

	slog.Info("flow created", "flow_id", flowID, "name", name, "nodes", len(nodes), "edges", len(edges))
	return flow, nil
}

// ListFlows 列出所有流程
func (e *Engine) ListFlows() ([]*db.FlowDefinition, error) {
	return e.db.ListFlows()
}

// GetFlow 获取流程详情
func (e *Engine) GetFlow(id string) (*db.FlowDefinition, error) {
	return e.db.GetFlow(id)
}

// RunFlow 启动流程运行
func (e *Engine) RunFlow(ctx context.Context, flowID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	flow, err := e.db.GetFlow(flowID)
	if err != nil {
		return fmt.Errorf("get flow: %w", err)
	}

	if flow.Status == "running" {
		return fmt.Errorf("flow %s is already running", flowID)
	}

	if err := e.db.UpdateFlowStatus(flowID, "running"); err != nil {
		return fmt.Errorf("update flow status: %w", err)
	}

	ctx, cancel := context.WithCancel(ctx)
	e.activeFlows[flowID] = &ActiveFlow{
		FlowID:   flowID,
		Cancel:   cancel,
		NodeMsgs: make(map[string]string),
	}

	// 更新所有节点为 idle（等待首次消息）
	nodes, err := e.db.GetNodesByFlowID(flowID)
	if err != nil {
		slog.Error("failed to get nodes after run", "flow_id", flowID, "error", err)
	} else {
		for _, node := range nodes {
			if err := e.db.UpdateNodeStatus(node.ID, "idle"); err != nil {
				slog.Error("failed to update node status", "node_id", node.ID, "error", err)
			}
		}
	}

	slog.Info("flow running", "flow_id", flowID)
	return nil
}

// StopFlow 停止流程运行
func (e *Engine) StopFlow(flowID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if active, exists := e.activeFlows[flowID]; exists {
		active.Cancel()
		delete(e.activeFlows, flowID)
	}

	if err := e.db.UpdateFlowStatus(flowID, "stopped"); err != nil {
		return fmt.Errorf("update flow status: %w", err)
	}

	// 更新所有节点状态
	nodes, err := e.db.GetNodesByFlowID(flowID)
	if err != nil {
		slog.Error("failed to get nodes after stop", "flow_id", flowID, "error", err)
	} else {
		for _, node := range nodes {
			if err := e.db.ClearNodePID(node.ID, "stopped"); err != nil {
				slog.Error("failed to clear node pid", "node_id", node.ID, "error", err)
			}
		}
	}

	slog.Info("flow stopped", "flow_id", flowID)
	return nil
}

// GetStatus 获取流程运行状态
func (e *Engine) GetStatus(flowID string) (*FlowStatus, error) {
	flow, err := e.db.GetFlow(flowID)
	if err != nil {
		return nil, fmt.Errorf("get flow: %w", err)
	}

	nodes, err := e.db.GetNodesByFlowID(flowID)
	if err != nil {
		return nil, fmt.Errorf("get nodes: %w", err)
	}

	nodeStatuses := make([]NodeStatus, len(nodes))
	for i, node := range nodes {
		nodeStatuses[i] = NodeStatus{
			ID:     node.ID,
			Name:   node.Name,
			Role:   node.Role,
			Status: node.Status,
			PID:    node.CLIPid,
		}
	}

	return &FlowStatus{
		FlowID:   flowID,
		Name:     flow.Name,
		Status:   flow.Status,
		Nodes:    nodeStatuses,
		EdgeCount: len(flow.Edges),
	}, nil
}

// FlowStatus 流程状态
type FlowStatus struct {
	FlowID    string       `json:"flow_id"`
	Name      string       `json:"name"`
	Status    string       `json:"status"`
	Nodes     []NodeStatus `json:"nodes"`
	EdgeCount int          `json:"edge_count"`
}

// NodeStatus 节点状态
type NodeStatus struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Role   string `json:"role"`
	Status string `json:"status"`
	PID    int    `json:"pid"`
}

// SendMessage 向指定节点发送消息并处理响应
// 这是核心消息路由逻辑：发送消息 → 接收输出 → 摘要 → 投递给下游
func (e *Engine) SendMessage(ctx context.Context, flowID string, nodeName string, message string) error {
	flow, err := e.db.GetFlow(flowID)
	if err != nil {
		return fmt.Errorf("get flow: %w", err)
	}

	// 校验流程必须处于 running 状态
	if flow.Status != "running" {
		return fmt.Errorf("flow %s is not running (status: %s)", flowID, flow.Status)
	}

	nodes, err := e.db.GetNodesByFlowID(flowID)
	if err != nil {
		return fmt.Errorf("get nodes: %w", err)
	}

	// 查找目标节点
	var targetNode *db.FlowNode
	for _, n := range nodes {
		if n.Name == nodeName {
			targetNode = n
			break
		}
	}
	if targetNode == nil {
		return fmt.Errorf("node %s not found in flow %s", nodeName, flowID)
	}

	// 获取该节点的历史摘要
	e.mu.RLock()
	historySummary := ""
	if active, exists := e.activeFlows[flowID]; exists {
		historySummary = active.NodeMsgs[nodeName]
	}
	e.mu.RUnlock()

	// 调用 CLI 获取响应
	ch, err := e.adapter.Prompt(ctx, message, historySummary)
	if err != nil {
		return fmt.Errorf("prompt cli: %w", err)
	}

	// 收集完整响应
	var fullResponse strings.Builder
	for msg := range ch {
		if msg.Type == adapter.TypeAssistant {
			text := adapter.ExtractText(msg)
			if text != "" {
				fullResponse.WriteString(text)
				// 输出到终端
				fmt.Printf("[%s] %s\n", nodeName, text)
			}
		}
	}

	output := fullResponse.String()
	if output == "" {
		slog.Warn("empty response from CLI", "flow_id", flowID, "node", nodeName)
		return nil
	}

	// 生成摘要
	var summaryText string
	if e.summary.IsEnabled() {
		summaryText, err = e.summary.Summarize(ctx, output)
		if err != nil {
			slog.Error("summary failed, using original", "error", err)
			summaryText = output
		}
	} else {
		summaryText = output
	}

	// 记录摘要
	e.db.CreateMessage(&db.MessageLog{
		FlowID:     flowID,
		NodeID:     targetNode.ID,
		Direction:  "out",
		RawContent: output,
		Summary:    summaryText,
	})

	// 更新节点历史
	e.mu.Lock()
	if active, exists := e.activeFlows[flowID]; exists {
		active.NodeMsgs[nodeName] = summaryText
	}
	e.mu.Unlock()

	// 投递给下游节点
	for _, edge := range flow.Edges {
		if edge.From == nodeName {
			slog.Info("routing message", "from", nodeName, "to", edge.To, "summary_len", len(summaryText))

			// 更新下游节点历史（摘要作为输入）
			e.mu.Lock()
			if active, exists := e.activeFlows[flowID]; exists {
				existing := active.NodeMsgs[edge.To]
				if existing != "" {
					active.NodeMsgs[edge.To] = existing + "\n\n" + summaryText
				} else {
					active.NodeMsgs[edge.To] = summaryText
				}
			}
			e.mu.Unlock()

			// 记录输入消息
			for _, n := range nodes {
				if n.Name == edge.To {
					e.db.CreateMessage(&db.MessageLog{
						FlowID:     flowID,
						NodeID:     n.ID,
						Direction:  "in",
						RawContent: summaryText,
					})
					break
				}
			}

			// 打印投递信息
			fmt.Printf("[%s] (摘要自 %s) %s\n", edge.To, nodeName, summaryText)
		}
	}

	return nil
}

// GetLogs 获取流程消息日志
func (e *Engine) GetLogs(flowID string, nodeName string, limit int) ([]*db.MessageLog, error) {
	if nodeName != "" {
		// 查找节点 ID
		nodes, err := e.db.GetNodesByFlowID(flowID)
		if err != nil {
			return nil, fmt.Errorf("get nodes: %w", err)
		}
		for _, n := range nodes {
			if n.Name == nodeName {
				return e.db.GetMessagesByNodeID(flowID, n.ID, limit)
			}
		}
		return nil, fmt.Errorf("node %s not found", nodeName)
	}
	return e.db.GetMessagesByFlowID(flowID, limit)
}

// DeleteFlow 删除流程
// 先调用 StopFlow 安全停止（StopFlow 内部处理锁和不存在的流程）
func (e *Engine) DeleteFlow(flowID string) error {
	// StopFlow 已处理"流程不存在"的情况，且内部有锁保护
	e.StopFlow(flowID)
	return e.db.DeleteFlow(flowID)
}

// generateID 生成简单 ID
func generateID(prefix string) string {
	// 简单实现：prefix + 时间戳后6位
	// 生产环境应使用 UUID
	return fmt.Sprintf("%s-%d", prefix, simpleCounter())
}

var idCounter int64

// simpleCounter 原子递增计数器，并发安全
func simpleCounter() int64 {
	return atomic.AddInt64(&idCounter, 1)
}
