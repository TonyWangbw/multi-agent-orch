// 进程管理器：管理 codebuddy-cli 子进程的生命周期
package process

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// ProcessStatus 进程状态
type ProcessStatus string

const (
	StatusIdle    ProcessStatus = "idle"
	StatusRunning ProcessStatus = "running"
	StatusError   ProcessStatus = "error"
	StatusStopped ProcessStatus = "stopped"
)

// ManagedProcess 管理中的子进程
type ManagedProcess struct {
	NodeID string
	Pid    int
	Cmd    *exec.Cmd
	Cancel context.CancelFunc
	Status ProcessStatus
}

// Manager 进程管理器
type Manager struct {
	mu       sync.RWMutex
	processes map[string]*ManagedProcess // nodeID -> ManagedProcess
	binary   string
	model    string
	workDir  string
}

// NewManager 创建进程管理器
func NewManager(binary string, model string, workDir string) *Manager {
	return &Manager{
		processes: make(map[string]*ManagedProcess),
		binary:    binary,
		model:     model,
		workDir:   workDir,
	}
}

// StartCLI 启动 codebuddy-cli 子进程
func (m *Manager) StartCLI(ctx context.Context, nodeID string, args []string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 检查是否已有运行中的进程
	if p, exists := m.processes[nodeID]; exists && p.Status == StatusRunning {
		return 0, fmt.Errorf("node %s already has a running process (pid %d)", nodeID, p.Pid)
	}

	ctx, cancel := context.WithCancel(ctx)

	cmd := exec.CommandContext(ctx, m.binary, args...)
	cmd.Dir = m.workDir
	cmd.Stdin = nil // 将在 adapter 层设置
	// stdout/stderr 由 adapter 层管理

	if err := cmd.Start(); err != nil {
		cancel()
		return 0, fmt.Errorf("start cli for node %s: %w", nodeID, err)
	}

	pid := cmd.Process.Pid
	m.processes[nodeID] = &ManagedProcess{
		NodeID: nodeID,
		Pid:    pid,
		Cmd:    cmd,
		Cancel: cancel,
		Status: StatusRunning,
	}

	slog.Info("cli process started", "node_id", nodeID, "pid", pid)

	// 后台监控进程退出
	go m.monitorProcess(nodeID)

	return pid, nil
}

// StopCLI 停止指定节点的 CLI 进程
func (m *Manager) StopCLI(nodeID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	p, exists := m.processes[nodeID]
	if !exists {
		return fmt.Errorf("node %s has no managed process", nodeID)
	}

	if p.Status != StatusRunning {
		return nil // 已经不是运行状态
	}

	return m.stopProcess(p)
}

// stopProcess 停止进程（内部方法，调用者需持有锁）
func (m *Manager) stopProcess(p *ManagedProcess) error {
	// 先尝试 SIGTERM
	if err := p.Cmd.Process.Signal(syscall.SIGTERM); err != nil {
		slog.Warn("failed to send SIGTERM", "node_id", p.NodeID, "pid", p.Pid, "error", err)
	}

	// 等待 5 秒
	done := make(chan error, 1)
	go func() {
		done <- p.Cmd.Wait()
	}()

	select {
	case <-time.After(5 * time.Second):
		// 超时，发送 SIGKILL
		slog.Warn("process did not exit in 5s, sending SIGKILL", "node_id", p.NodeID, "pid", p.Pid)
		if err := p.Cmd.Process.Kill(); err != nil {
			return fmt.Errorf("kill process %d: %w", p.Pid, err)
		}
	case err := <-done:
		if err != nil {
			slog.Debug("process exited with error", "node_id", p.NodeID, "pid", p.Pid, "error", err)
		}
	}

	p.Cancel()
	p.Status = StatusStopped
	slog.Info("cli process stopped", "node_id", p.NodeID, "pid", p.Pid)
	return nil
}

// StopAll 停止所有运行中的 CLI 进程
func (m *Manager) StopAll() []error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var errs []error
	for _, p := range m.processes {
		if p.Status == StatusRunning {
			if err := m.stopProcess(p); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errs
}

// Status 获取指定节点的进程状态
func (m *Manager) Status(nodeID string) ProcessStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if p, exists := m.processes[nodeID]; exists {
		return p.Status
	}
	return StatusIdle
}

// GetPID 获取指定节点的进程 PID
func (m *Manager) GetPID(nodeID string) (int, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if p, exists := m.processes[nodeID]; exists {
		return p.Pid, true
	}
	return 0, false
}

// ListRunning 列出所有运行中的节点
func (m *Manager) ListRunning() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var running []string
	for _, p := range m.processes {
		if p.Status == StatusRunning {
			running = append(running, p.NodeID)
		}
	}
	return running
}

// CleanupOrphans 清理残留的 codebuddy 进程
// 查找系统中不属于任何管理节点的 codebuddy 进程
func (m *Manager) CleanupOrphans() ([]int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 收集当前管理的 PID
	managedPids := make(map[int]bool)
	for _, p := range m.processes {
		managedPids[p.Pid] = true
	}

	// 查找所有 codebuddy 进程
	var orphanPids []int

	// 在 Windows 上用 tasklist，在 Unix 上用 pgrep
	cmd := exec.Command("pgrep", "-f", m.binary)
	output, err := cmd.Output()
	if err != nil {
		// pgrep 没找到进程时返回 exit code 1，不是错误
		slog.Debug("no orphan processes found", "error", err)
		return nil, nil
	}

	// 解析 PID
	var pid int
	for _, line := range splitLines(string(output)) {
		if _, err := fmt.Sscanf(line, "%d", &pid); err == nil {
			if !managedPids[pid] {
				orphanPids = append(orphanPids, pid)
			}
		}
	}

	// 清理孤儿进程
	for _, pid := range orphanPids {
		proc, err := os.FindProcess(pid)
		if err != nil {
			slog.Warn("failed to find orphan process", "pid", pid, "error", err)
			continue
		}
		if err := proc.Kill(); err != nil {
			slog.Warn("failed to kill orphan process", "pid", pid, "error", err)
			continue
		}
		slog.Info("cleaned up orphan process", "pid", pid)
	}

	return orphanPids, nil
}

// monitorProcess 监控进程退出
func (m *Manager) monitorProcess(nodeID string) {
	m.mu.RLock()
	p, exists := m.processes[nodeID]
	m.mu.RUnlock()

	if !exists {
		return
	}

	err := p.Cmd.Wait()

	m.mu.Lock()
	defer m.mu.Unlock()

	if p.Status == StatusRunning {
		if err != nil {
			p.Status = StatusError
			slog.Error("cli process exited with error", "node_id", nodeID, "pid", p.Pid, "error", err)
		} else {
			p.Status = StatusStopped
			slog.Info("cli process exited normally", "node_id", nodeID, "pid", p.Pid)
		}
	}
}

// splitLines 按换行符分割字符串
func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			line := s[start:i]
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			if line != "" {
				lines = append(lines, line)
			}
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
