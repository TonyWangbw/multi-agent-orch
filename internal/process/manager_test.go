package process

import (
	"context"
	"testing"
	"time"
)

func TestManagerStartAndStop(t *testing.T) {
	mgr := NewManager("sleep", "glm-5.1", ".")

	pid, err := mgr.StartCLI(context.Background(), "node-1", []string{"10"})
	if err != nil {
		t.Fatalf("start cli: %v", err)
	}
	if pid <= 0 {
		t.Errorf("expected positive pid, got %d", pid)
	}

	status := mgr.Status("node-1")
	if status != StatusRunning {
		t.Errorf("expected running, got %s", status)
	}

	if err := mgr.StopCLI("node-1"); err != nil {
		t.Fatalf("stop cli: %v", err)
	}

	// 等待进程状态更新
	time.Sleep(100 * time.Millisecond)

	status = mgr.Status("node-1")
	if status != StatusStopped {
		t.Errorf("expected stopped, got %s", status)
	}
}

func TestManagerStopNonExistent(t *testing.T) {
	mgr := NewManager("echo", "glm-5.1", ".")

	err := mgr.StopCLI("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent node")
	}
}

func TestManagerDoubleStart(t *testing.T) {
	mgr := NewManager("sleep", "glm-5.1", ".")

	pid1, err := mgr.StartCLI(context.Background(), "node-1", []string{"10"})
	if err != nil {
		t.Fatalf("first start: %v", err)
	}

	// 第二次启动同一节点应失败
	_, err = mgr.StartCLI(context.Background(), "node-1", []string{"10"})
	if err == nil {
		t.Error("expected error for double start")
	}

	// 清理
	mgr.StopCLI("node-1")
	t.Logf("started pid %d", pid1)
}

func TestManagerStopAll(t *testing.T) {
	mgr := NewManager("sleep", "glm-5.1", ".")

	mgr.StartCLI(context.Background(), "node-1", []string{"10"})
	mgr.StartCLI(context.Background(), "node-2", []string{"10"})

	running := mgr.ListRunning()
	if len(running) != 2 {
		t.Errorf("expected 2 running, got %d", len(running))
	}

	errs := mgr.StopAll()
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}

	time.Sleep(100 * time.Millisecond)

	running = mgr.ListRunning()
	if len(running) != 0 {
		t.Errorf("expected 0 running after stop all, got %d", len(running))
	}
}

func TestManagerStatusNonExistent(t *testing.T) {
	mgr := NewManager("echo", "glm-5.1", ".")
	status := mgr.Status("nonexistent")
	if status != StatusIdle {
		t.Errorf("expected idle, got %s", status)
	}
}

func TestManagerGetPID(t *testing.T) {
	mgr := NewManager("sleep", "glm-5.1", ".")

	pid, _ := mgr.GetPID("node-1")
	if pid != 0 {
		t.Errorf("expected 0 for nonexistent node, got %d", pid)
	}

	mgr.StartCLI(context.Background(), "node-1", []string{"10"})

	pid, ok := mgr.GetPID("node-1")
	if !ok {
		t.Error("expected to find pid")
	}
	if pid <= 0 {
		t.Errorf("expected positive pid, got %d", pid)
	}

	mgr.StopCLI("node-1")
}

func TestManagerContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	mgr := NewManager("sleep", "glm-5.1", ".")
	pid, err := mgr.StartCLI(ctx, "node-1", []string{"60"})
	if err != nil {
		t.Fatalf("start cli: %v", err)
	}
	t.Logf("started pid %d", pid)

	// 取消上下文
	cancel()
	time.Sleep(200 * time.Millisecond)

	// 进程应该退出
	status := mgr.Status("node-1")
	if status == StatusRunning {
		t.Error("expected process to stop after context cancellation")
		mgr.StopCLI("node-1")
	}
}

func TestSplitLines(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"line1\nline2\n", 2},
		{"single", 1},
		{"", 0},
		{"a\nb\nc\n", 3},
	}

	for _, tt := range tests {
		lines := splitLines(tt.input)
		if len(lines) != tt.want {
			t.Errorf("splitLines(%q): expected %d lines, got %d", tt.input, tt.want, len(lines))
		}
	}
}
