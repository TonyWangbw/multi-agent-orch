package db

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// testDB 创建内存测试数据库
func testDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestOpenAndMigrate(t *testing.T) {
	db := testDB(t)

	// 验证表已创建
	var count int
	err := db.db.QueryRow("SELECT count(*) FROM flow_definitions").Scan(&count)
	if err != nil {
		t.Fatalf("query flow_definitions: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 rows, got %d", count)
	}
}

func TestOpenWithFile(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open file db: %v", err)
	}
	db.Close()

	// 验证文件已创建
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatalf("db file not created: %s", dbPath)
	}
}

// --- FlowDefinition CRUD 测试 ---

func TestCreateAndGetFlow(t *testing.T) {
	db := testDB(t)

	flow := &FlowDefinition{
		ID:     "flow-1",
		Name:   "test-flow",
		Edges:  []Edge{{From: "coder", To: "reviewer"}},
		Status: "created",
		Config: FlowConfig{},
	}

	if err := db.CreateFlow(flow); err != nil {
		t.Fatalf("create flow: %v", err)
	}

	got, err := db.GetFlow("flow-1")
	if err != nil {
		t.Fatalf("get flow: %v", err)
	}

	if got.ID != "flow-1" {
		t.Errorf("expected ID flow-1, got %s", got.ID)
	}
	if got.Name != "test-flow" {
		t.Errorf("expected Name test-flow, got %s", got.Name)
	}
	if len(got.Edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(got.Edges))
	}
	if got.Edges[0].From != "coder" || got.Edges[0].To != "reviewer" {
		t.Errorf("unexpected edge: %+v", got.Edges[0])
	}
	if got.Status != "created" {
		t.Errorf("expected status created, got %s", got.Status)
	}
}

func TestListFlows(t *testing.T) {
	db := testDB(t)

	for i := 0; i < 3; i++ {
		flow := &FlowDefinition{
			ID:     fmt.Sprintf("flow-%d", i),
			Name:   fmt.Sprintf("test-flow-%d", i),
			Edges:  []Edge{},
			Status: "created",
		}
		if err := db.CreateFlow(flow); err != nil {
			t.Fatalf("create flow %d: %v", i, err)
		}
	}

	flows, err := db.ListFlows()
	if err != nil {
		t.Fatalf("list flows: %v", err)
	}
	if len(flows) != 3 {
		t.Errorf("expected 3 flows, got %d", len(flows))
	}
}

func TestUpdateFlowStatus(t *testing.T) {
	db := testDB(t)

	flow := &FlowDefinition{ID: "flow-1", Name: "test", Edges: []Edge{}, Status: "created"}
	if err := db.CreateFlow(flow); err != nil {
		t.Fatalf("create flow: %v", err)
	}

	if err := db.UpdateFlowStatus("flow-1", "running"); err != nil {
		t.Fatalf("update status: %v", err)
	}

	got, _ := db.GetFlow("flow-1")
	if got.Status != "running" {
		t.Errorf("expected status running, got %s", got.Status)
	}
}

func TestDeleteFlow(t *testing.T) {
	db := testDB(t)

	flow := &FlowDefinition{ID: "flow-1", Name: "test", Edges: []Edge{}, Status: "created"}
	db.CreateFlow(flow)

	if err := db.DeleteFlow("flow-1"); err != nil {
		t.Fatalf("delete flow: %v", err)
	}

	_, err := db.GetFlow("flow-1")
	if err == nil {
		t.Error("expected error after delete, got nil")
	}
}

// --- FlowNode CRUD 测试 ---

func TestCreateAndGetNodes(t *testing.T) {
	db := testDB(t)

	flow := &FlowDefinition{ID: "flow-1", Name: "test", Edges: []Edge{}, Status: "created"}
	db.CreateFlow(flow)

	nodes := []*FlowNode{
		{ID: "node-1", FlowID: "flow-1", Name: "coder", Role: "代码编写", Status: "idle"},
		{ID: "node-2", FlowID: "flow-1", Name: "reviewer", Role: "代码审查", Status: "idle"},
	}

	for _, n := range nodes {
		if err := db.CreateNode(n); err != nil {
			t.Fatalf("create node: %v", err)
		}
	}

	got, err := db.GetNodesByFlowID("flow-1")
	if err != nil {
		t.Fatalf("get nodes: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 nodes, got %d", len(got))
	}
}

func TestUpdateNodeStatus(t *testing.T) {
	db := testDB(t)

	flow := &FlowDefinition{ID: "flow-1", Name: "test", Edges: []Edge{}, Status: "created"}
	db.CreateFlow(flow)

	node := &FlowNode{ID: "node-1", FlowID: "flow-1", Name: "coder", Status: "idle"}
	db.CreateNode(node)

	if err := db.UpdateNodeStatus("node-1", "running"); err != nil {
		t.Fatalf("update node status: %v", err)
	}

	nodes, _ := db.GetNodesByFlowID("flow-1")
	if nodes[0].Status != "running" {
		t.Errorf("expected running, got %s", nodes[0].Status)
	}
}

func TestUpdateNodePID(t *testing.T) {
	db := testDB(t)

	flow := &FlowDefinition{ID: "flow-1", Name: "test", Edges: []Edge{}, Status: "created"}
	db.CreateFlow(flow)

	node := &FlowNode{ID: "node-1", FlowID: "flow-1", Name: "coder", Status: "idle"}
	db.CreateNode(node)

	if err := db.UpdateNodePID("node-1", 12345); err != nil {
		t.Fatalf("update node pid: %v", err)
	}

	nodes, _ := db.GetNodesByFlowID("flow-1")
	if nodes[0].CLIPid != 12345 {
		t.Errorf("expected pid 12345, got %d", nodes[0].CLIPid)
	}
	if nodes[0].Status != "running" {
		t.Errorf("expected status running, got %s", nodes[0].Status)
	}
}

func TestClearNodePID(t *testing.T) {
	db := testDB(t)

	flow := &FlowDefinition{ID: "flow-1", Name: "test", Edges: []Edge{}, Status: "created"}
	db.CreateFlow(flow)

	node := &FlowNode{ID: "node-1", FlowID: "flow-1", Name: "coder", Status: "idle"}
	db.CreateNode(node)
	db.UpdateNodePID("node-1", 12345)

	if err := db.ClearNodePID("node-1", "stopped"); err != nil {
		t.Fatalf("clear node pid: %v", err)
	}

	nodes, _ := db.GetNodesByFlowID("flow-1")
	if nodes[0].CLIPid != 0 {
		t.Errorf("expected pid 0, got %d", nodes[0].CLIPid)
	}
	if nodes[0].Status != "stopped" {
		t.Errorf("expected status stopped, got %s", nodes[0].Status)
	}
}

// --- MessageLog CRUD 测试 ---

func TestCreateAndGetMessages(t *testing.T) {
	db := testDB(t)

	flow := &FlowDefinition{ID: "flow-1", Name: "test", Edges: []Edge{}, Status: "created"}
	db.CreateFlow(flow)
	node := &FlowNode{ID: "node-1", FlowID: "flow-1", Name: "coder", Status: "idle"}
	db.CreateNode(node)

	msg := &MessageLog{
		FlowID:     "flow-1",
		NodeID:     "node-1",
		Direction:  "out",
		RawContent: "hello world",
		Summary:    "greeting",
	}
	if err := db.CreateMessage(msg); err != nil {
		t.Fatalf("create message: %v", err)
	}

	messages, err := db.GetMessagesByFlowID("flow-1", 0)
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	if messages[0].RawContent != "hello world" {
		t.Errorf("expected 'hello world', got %s", messages[0].RawContent)
	}
	if messages[0].Summary != "greeting" {
		t.Errorf("expected 'greeting', got %s", messages[0].Summary)
	}
}

func TestGetMessagesByNodeID(t *testing.T) {
	db := testDB(t)

	flow := &FlowDefinition{ID: "flow-1", Name: "test", Edges: []Edge{}, Status: "created"}
	db.CreateFlow(flow)
	db.CreateNode(&FlowNode{ID: "node-1", FlowID: "flow-1", Name: "coder", Status: "idle"})
	db.CreateNode(&FlowNode{ID: "node-2", FlowID: "flow-1", Name: "reviewer", Status: "idle"})

	db.CreateMessage(&MessageLog{FlowID: "flow-1", NodeID: "node-1", Direction: "out", RawContent: "msg1"})
	db.CreateMessage(&MessageLog{FlowID: "flow-1", NodeID: "node-2", Direction: "out", RawContent: "msg2"})

	messages, err := db.GetMessagesByNodeID("flow-1", "node-1", 0)
	if err != nil {
		t.Fatalf("get messages by node: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	if messages[0].RawContent != "msg1" {
		t.Errorf("expected 'msg1', got %s", messages[0].RawContent)
	}
}

func TestGetMessagesWithLimit(t *testing.T) {
	db := testDB(t)

	flow := &FlowDefinition{ID: "flow-1", Name: "test", Edges: []Edge{}, Status: "created"}
	db.CreateFlow(flow)
	db.CreateNode(&FlowNode{ID: "node-1", FlowID: "flow-1", Name: "coder", Status: "idle"})

	for i := 0; i < 5; i++ {
		db.CreateMessage(&MessageLog{FlowID: "flow-1", NodeID: "node-1", Direction: "out", RawContent: "msg"})
	}

	messages, err := db.GetMessagesByFlowID("flow-1", 3)
	if err != nil {
		t.Fatalf("get messages with limit: %v", err)
	}
	if len(messages) != 3 {
		t.Errorf("expected 3 messages, got %d", len(messages))
	}
}

func TestDeleteFlowCascades(t *testing.T) {
	db := testDB(t)

	flow := &FlowDefinition{ID: "flow-1", Name: "test", Edges: []Edge{}, Status: "created"}
	db.CreateFlow(flow)
	db.CreateNode(&FlowNode{ID: "node-1", FlowID: "flow-1", Name: "coder", Status: "idle"})
	db.CreateMessage(&MessageLog{FlowID: "flow-1", NodeID: "node-1", Direction: "out", RawContent: "msg"})

	// 删除流程应级联删除节点和消息
	if err := db.DeleteFlow("flow-1"); err != nil {
		t.Fatalf("delete flow: %v", err)
	}

	nodes, _ := db.GetNodesByFlowID("flow-1")
	if len(nodes) != 0 {
		t.Errorf("expected 0 nodes after cascade delete, got %d", len(nodes))
	}

	msgs, _ := db.GetMessagesByFlowID("flow-1", 0)
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages after cascade delete, got %d", len(msgs))
	}
}
