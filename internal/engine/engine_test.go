package engine

import (
	"context"
	"testing"

	"github.com/boss/multi-agent-orch/internal/adapter"
	"github.com/boss/multi-agent-orch/internal/db"
	"github.com/boss/multi-agent-orch/internal/summary"
)

func testEngine(t *testing.T) *Engine {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	cliAdapter := adapter.NewCLIClient("echo", "glm-5.1", ".")
	summarySvc := summary.NewService(summary.Config{
		Enabled:   false,
		MaxTokens: 512,
	})

	return New(database, cliAdapter, summarySvc)
}

func TestCreateFlow(t *testing.T) {
	e := testEngine(t)

	flow, err := e.CreateFlow("test-flow", []string{"coder", "reviewer"}, []db.Edge{
		{From: "coder", To: "reviewer"},
	})
	if err != nil {
		t.Fatalf("create flow: %v", err)
	}

	if flow.ID == "" {
		t.Error("expected non-empty flow ID")
	}
	if flow.Name != "test-flow" {
		t.Errorf("expected test-flow, got %s", flow.Name)
	}
	if flow.Status != "created" {
		t.Errorf("expected created, got %s", flow.Status)
	}

	// 验证节点已创建
	nodes, _ := e.db.GetNodesByFlowID(flow.ID)
	if len(nodes) != 2 {
		t.Errorf("expected 2 nodes, got %d", len(nodes))
	}
}

func TestListFlows(t *testing.T) {
	e := testEngine(t)

	e.CreateFlow("flow-1", []string{"a"}, nil)
	e.CreateFlow("flow-2", []string{"b"}, nil)

	flows, err := e.ListFlows()
	if err != nil {
		t.Fatalf("list flows: %v", err)
	}
	if len(flows) != 2 {
		t.Errorf("expected 2 flows, got %d", len(flows))
	}
}

func TestRunAndStopFlow(t *testing.T) {
	e := testEngine(t)

	flow, _ := e.CreateFlow("test", []string{"coder"}, nil)

	if err := e.RunFlow(context.Background(), flow.ID); err != nil {
		t.Fatalf("run flow: %v", err)
	}

	status, _ := e.GetStatus(flow.ID)
	if status.Status != "running" {
		t.Errorf("expected running, got %s", status.Status)
	}

	if err := e.StopFlow(flow.ID); err != nil {
		t.Fatalf("stop flow: %v", err)
	}

	status, _ = e.GetStatus(flow.ID)
	if status.Status != "stopped" {
		t.Errorf("expected stopped, got %s", status.Status)
	}
}

func TestRunAlreadyRunningFlow(t *testing.T) {
	e := testEngine(t)

	flow, _ := e.CreateFlow("test", []string{"coder"}, nil)
	e.RunFlow(context.Background(), flow.ID)

	err := e.RunFlow(context.Background(), flow.ID)
	if err == nil {
		t.Error("expected error for already running flow")
	}

	e.StopFlow(flow.ID)
}

func TestGetStatus(t *testing.T) {
	e := testEngine(t)

	flow, _ := e.CreateFlow("test", []string{"coder", "reviewer"}, []db.Edge{
		{From: "coder", To: "reviewer"},
	})

	status, err := e.GetStatus(flow.ID)
	if err != nil {
		t.Fatalf("get status: %v", err)
	}

	if status.FlowID != flow.ID {
		t.Errorf("expected flow ID %s, got %s", flow.ID, status.FlowID)
	}
	if len(status.Nodes) != 2 {
		t.Errorf("expected 2 nodes, got %d", len(status.Nodes))
	}
	if status.EdgeCount != 1 {
		t.Errorf("expected 1 edge, got %d", status.EdgeCount)
	}
}

func TestDeleteFlow(t *testing.T) {
	e := testEngine(t)

	flow, _ := e.CreateFlow("test", []string{"coder"}, nil)

	if err := e.DeleteFlow(flow.ID); err != nil {
		t.Fatalf("delete flow: %v", err)
	}

	_, err2 := e.GetFlow(flow.ID)
	if err2 == nil {
		t.Error("expected error after delete")
	}
}

func TestGetLogs(t *testing.T) {
	e := testEngine(t)

	flow, _ := e.CreateFlow("test", []string{"coder"}, nil)

	// 插入测试消息
	nodes, _ := e.db.GetNodesByFlowID(flow.ID)
	e.db.CreateMessage(&db.MessageLog{
		FlowID:     flow.ID,
		NodeID:     nodes[0].ID,
		Direction:  "out",
		RawContent: "test message",
	})

	logs, err := e.GetLogs(flow.ID, "", 0)
	if err != nil {
		t.Fatalf("get logs: %v", err)
	}
	if len(logs) != 1 {
		t.Errorf("expected 1 log, got %d", len(logs))
	}

	// 按节点名查询
	logsByNode, err := e.GetLogs(flow.ID, "coder", 0)
	if err != nil {
		t.Fatalf("get logs by node: %v", err)
	}
	if len(logsByNode) != 1 {
		t.Errorf("expected 1 log for node, got %d", len(logsByNode))
	}
}

func TestGetLogsNonExistentNode(t *testing.T) {
	e := testEngine(t)

	flow, _ := e.CreateFlow("test", []string{"coder"}, nil)

	_, err := e.GetLogs(flow.ID, "nonexistent", 0)
	if err == nil {
		t.Error("expected error for nonexistent node")
	}
}

func TestGenerateID(t *testing.T) {
	id1 := generateID("flow")
	id2 := generateID("flow")

	if id1 == id2 {
		t.Error("expected different IDs")
	}
	if !startsWith(id1, "flow-") {
		t.Errorf("expected prefix 'flow-', got %s", id1)
	}
}

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
