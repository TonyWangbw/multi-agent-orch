// 端到端集成测试：使用真实 codebuddy-cli 验证消息路由
// 运行条件：需要 codebuddy 已安装且可执行
package integration

import (
	"context"
	"testing"
	"time"

	"github.com/boss/multi-agent-orch/internal/adapter"
	"github.com/boss/multi-agent-orch/internal/db"
	"github.com/boss/multi-agent-orch/internal/engine"
	"github.com/boss/multi-agent-orch/internal/summary"
)

func TestCLIPrintMode(t *testing.T) {
	// 验证 codebuddy --print --output-format stream-json 可正常工作
	cli := adapter.NewCLIClient("codebuddy", "glm-5.1", ".")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ch, err := cli.Prompt(ctx, "say 'hello' and nothing else", "")
	if err != nil {
		t.Fatalf("prompt: %v", err)
	}

	var gotResponse bool
	for msg := range ch {
		if msg.Type == adapter.TypeResult {
			gotResponse = true
			t.Logf("result: %v", msg.Result)
		}
		if msg.Type == adapter.TypeAssistant && msg.Message != nil {
			t.Logf("assistant: %s", adapter.ExtractText(msg))
		}
	}

	if !gotResponse {
		t.Error("expected result message from CLI")
	}
}

func TestFlowCreateListRunStop(t *testing.T) {
	// 端到端测试：创建 → 列出 → 运行 → 状态 → 停止
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	cli := adapter.NewCLIClient("codebuddy", "glm-5.1", ".")
	summarySvc := summary.NewService(summary.Config{
		Enabled:   false,
		MaxTokens: 512,
	})
	eng := engine.New(database, cli, summarySvc)

	// 创建流程
	flow, err := eng.CreateFlow("e2e-test", []string{"coder", "reviewer"}, []db.Edge{
		{From: "coder", To: "reviewer"},
	})
	if err != nil {
		t.Fatalf("create flow: %v", err)
	}
	t.Logf("created flow: %s", flow.ID)

	// 列出流程
	flows, err := eng.ListFlows()
	if err != nil {
		t.Fatalf("list flows: %v", err)
	}
	if len(flows) != 1 {
		t.Errorf("expected 1 flow, got %d", len(flows))
	}

	// 运行流程
	if err := eng.RunFlow(context.Background(), flow.ID); err != nil {
		t.Fatalf("run flow: %v", err)
	}

	// 查看状态
	status, err := eng.GetStatus(flow.ID)
	if err != nil {
		t.Fatalf("get status: %v", err)
	}
	if status.Status != "running" {
		t.Errorf("expected running, got %s", status.Status)
	}
	t.Logf("status: %s, nodes: %d", status.Status, len(status.Nodes))

	// 停止流程
	if err := eng.StopFlow(flow.ID); err != nil {
		t.Fatalf("stop flow: %v", err)
	}

	// 验证已停止
	status, _ = eng.GetStatus(flow.ID)
	if status.Status != "stopped" {
		t.Errorf("expected stopped, got %s", status.Status)
	}
}

func TestSendMessagesWithHistory(t *testing.T) {
	// 端到端测试：发送消息 → CLI 响应 → 历史上下文注入
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	cli := adapter.NewCLIClient("codebuddy", "glm-5.1", ".")
	summarySvc := summary.NewService(summary.Config{
		Enabled:   false,
		MaxTokens: 512,
	})
	eng := engine.New(database, cli, summarySvc)

	flow, _ := eng.CreateFlow("msg-test", []string{"coder"}, nil)
	eng.RunFlow(context.Background(), flow.ID)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = eng.SendMessage(ctx, flow.ID, "coder", "say 'test ok' and nothing else")
	if err != nil {
		t.Fatalf("send message: %v", err)
	}

	// 验证日志已记录
	logs, err := eng.GetLogs(flow.ID, "", 0)
	if err != nil {
		t.Fatalf("get logs: %v", err)
	}
	if len(logs) == 0 {
		t.Error("expected at least 1 log entry")
	}

	eng.StopFlow(flow.ID)
}
