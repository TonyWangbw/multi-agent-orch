// 数据库层：SQLite 初始化、schema 迁移与 CRUD 操作
package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	_ "modernc.org/sqlite"
)

// FlowDefinition 编排流程定义
type FlowDefinition struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Edges     []Edge     `json:"edges"`
	Status    string     `json:"status"` // created, running, stopped, error
	Config    FlowConfig `json:"config"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// Edge 表示节点间的消息路由边
type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// FlowConfig 流程级别配置覆盖
type FlowConfig struct {
	SummaryEnabled *bool  `json:"summary_enabled,omitempty"`
	SummaryModel   string `json:"summary_model,omitempty"`
}

// FlowNode 流程中的节点
type FlowNode struct {
	ID       string    `json:"id"`
	FlowID   string    `json:"flow_id"`
	Name     string    `json:"name"`
	Role     string    `json:"role"`
	CLIPid   int       `json:"cli_pid"`
	Status   string    `json:"status"` // idle, running, error
	CreatedAt time.Time `json:"created_at"`
}

// MessageLog 消息日志
type MessageLog struct {
	ID          int64     `json:"id"`
	FlowID      string    `json:"flow_id"`
	NodeID      string    `json:"node_id"`
	Direction   string    `json:"direction"` // in, out
	RawContent  string    `json:"raw_content"`
	Summary     string    `json:"summary,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// DB 数据库访问对象
type DB struct {
	db *sql.DB
}

// Open 打开 SQLite 数据库并执行 schema 迁移
func Open(dbPath string) (*DB, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", dbPath, err)
	}

	// 启用 WAL 模式以支持并发读取
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable WAL mode: %w", err)
	}

	// 启用外键约束（SQLite 默认不启用）
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}

	d := &DB{db: db}
	if err := d.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("schema migration: %w", err)
	}

	slog.Info("database opened", "path", dbPath)
	return d, nil
}

// Close 关闭数据库连接
func (d *DB) Close() error {
	return d.db.Close()
}

// migrate 执行 schema 迁移
func (d *DB) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS flow_definitions (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		edges TEXT NOT NULL DEFAULT '[]',
		status TEXT NOT NULL DEFAULT 'created',
		config TEXT NOT NULL DEFAULT '{}',
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS flow_nodes (
		id TEXT PRIMARY KEY,
		flow_id TEXT NOT NULL REFERENCES flow_definitions(id) ON DELETE CASCADE,
		name TEXT NOT NULL,
		role TEXT NOT NULL DEFAULT '',
		cli_pid INTEGER,
		status TEXT NOT NULL DEFAULT 'idle',
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS message_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		flow_id TEXT NOT NULL REFERENCES flow_definitions(id) ON DELETE CASCADE,
		node_id TEXT NOT NULL REFERENCES flow_nodes(id) ON DELETE CASCADE,
		direction TEXT NOT NULL,
		raw_content TEXT NOT NULL,
		summary TEXT,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_flow_nodes_flow_id ON flow_nodes(flow_id);
	CREATE INDEX IF NOT EXISTS idx_message_logs_flow_id ON message_logs(flow_id);
	CREATE INDEX IF NOT EXISTS idx_message_logs_node_id ON message_logs(node_id);
	`
	_, err := d.db.Exec(schema)
	return err
}

// --- FlowDefinition CRUD ---

// CreateFlow 创建新的编排流程
func (d *DB) CreateFlow(flow *FlowDefinition) error {
	edgesJSON, err := json.Marshal(flow.Edges)
	if err != nil {
		return fmt.Errorf("marshal edges: %w", err)
	}
	configJSON, err := json.Marshal(flow.Config)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	_, err = d.db.Exec(
		"INSERT INTO flow_definitions (id, name, edges, status, config) VALUES (?, ?, ?, ?, ?)",
		flow.ID, flow.Name, string(edgesJSON), flow.Status, string(configJSON),
	)
	if err != nil {
		return fmt.Errorf("insert flow: %w", err)
	}
	slog.Info("flow created", "flow_id", flow.ID, "name", flow.Name)
	return nil
}

// GetFlow 根据 ID 获取流程定义
func (d *DB) GetFlow(id string) (*FlowDefinition, error) {
	row := d.db.QueryRow(
		"SELECT id, name, edges, status, config, created_at, updated_at FROM flow_definitions WHERE id = ?",
		id,
	)
	return d.scanFlow(row)
}

// ListFlows 列出所有流程定义
func (d *DB) ListFlows() ([]*FlowDefinition, error) {
	rows, err := d.db.Query(
		"SELECT id, name, edges, status, config, created_at, updated_at FROM flow_definitions ORDER BY created_at DESC",
	)
	if err != nil {
		return nil, fmt.Errorf("query flows: %w", err)
	}
	defer rows.Close()

	var flows []*FlowDefinition
	for rows.Next() {
		flow, err := d.scanFlowFromRows(rows)
		if err != nil {
			return nil, err
		}
		flows = append(flows, flow)
	}
	return flows, rows.Err()
}

// UpdateFlowStatus 更新流程状态
func (d *DB) UpdateFlowStatus(id string, status string) error {
	_, err := d.db.Exec(
		"UPDATE flow_definitions SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		status, id,
	)
	if err != nil {
		return fmt.Errorf("update flow status: %w", err)
	}
	slog.Info("flow status updated", "flow_id", id, "status", status)
	return nil
}

// DeleteFlow 删除流程定义（级联删除节点和消息日志）
func (d *DB) DeleteFlow(id string) error {
	_, err := d.db.Exec("DELETE FROM flow_definitions WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete flow: %w", err)
	}
	slog.Info("flow deleted", "flow_id", id)
	return nil
}

// scanFlow 从单行查询结果扫描 FlowDefinition
func (d *DB) scanFlow(row *sql.Row) (*FlowDefinition, error) {
	var flow FlowDefinition
	var edgesJSON, configJSON string
	err := row.Scan(
		&flow.ID, &flow.Name, &edgesJSON, &flow.Status, &configJSON,
		&flow.CreatedAt, &flow.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan flow: %w", err)
	}
	if err := json.Unmarshal([]byte(edgesJSON), &flow.Edges); err != nil {
		return nil, fmt.Errorf("unmarshal edges: %w", err)
	}
	if err := json.Unmarshal([]byte(configJSON), &flow.Config); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	return &flow, nil
}

// scanFlowFromRows 从多行查询结果扫描 FlowDefinition
func (d *DB) scanFlowFromRows(rows *sql.Rows) (*FlowDefinition, error) {
	var flow FlowDefinition
	var edgesJSON, configJSON string
	err := rows.Scan(
		&flow.ID, &flow.Name, &edgesJSON, &flow.Status, &configJSON,
		&flow.CreatedAt, &flow.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan flow row: %w", err)
	}
	if err := json.Unmarshal([]byte(edgesJSON), &flow.Edges); err != nil {
		return nil, fmt.Errorf("unmarshal edges: %w", err)
	}
	if err := json.Unmarshal([]byte(configJSON), &flow.Config); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	return &flow, nil
}

// --- FlowNode CRUD ---

// CreateNode 创建流程节点
func (d *DB) CreateNode(node *FlowNode) error {
	_, err := d.db.Exec(
		"INSERT INTO flow_nodes (id, flow_id, name, role, status) VALUES (?, ?, ?, ?, ?)",
		node.ID, node.FlowID, node.Name, node.Role, node.Status,
	)
	if err != nil {
		return fmt.Errorf("insert node: %w", err)
	}
	slog.Info("node created", "node_id", node.ID, "flow_id", node.FlowID, "name", node.Name)
	return nil
}

// GetNodesByFlowID 获取流程下所有节点
func (d *DB) GetNodesByFlowID(flowID string) ([]*FlowNode, error) {
	rows, err := d.db.Query(
		"SELECT id, flow_id, name, role, cli_pid, status, created_at FROM flow_nodes WHERE flow_id = ?",
		flowID,
	)
	if err != nil {
		return nil, fmt.Errorf("query nodes: %w", err)
	}
	defer rows.Close()

	var nodes []*FlowNode
	for rows.Next() {
		var node FlowNode
		var cliPid sql.NullInt64
		err := rows.Scan(
			&node.ID, &node.FlowID, &node.Name, &node.Role,
			&cliPid, &node.Status, &node.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan node row: %w", err)
		}
		if cliPid.Valid {
			node.CLIPid = int(cliPid.Int64)
		}
		nodes = append(nodes, &node)
	}
	return nodes, rows.Err()
}

// UpdateNodeStatus 更新节点状态
func (d *DB) UpdateNodeStatus(id string, status string) error {
	_, err := d.db.Exec("UPDATE flow_nodes SET status = ? WHERE id = ?", status, id)
	if err != nil {
		return fmt.Errorf("update node status: %w", err)
	}
	return nil
}

// UpdateNodePID 更新节点的 CLI 进程 PID
func (d *DB) UpdateNodePID(id string, pid int) error {
	_, err := d.db.Exec("UPDATE flow_nodes SET cli_pid = ?, status = 'running' WHERE id = ?", pid, id)
	if err != nil {
		return fmt.Errorf("update node pid: %w", err)
	}
	slog.Info("node pid updated", "node_id", id, "pid", pid)
	return nil
}

// ClearNodePID 清除节点的 CLI 进程 PID（停止时调用）
func (d *DB) ClearNodePID(id string, status string) error {
	_, err := d.db.Exec("UPDATE flow_nodes SET cli_pid = NULL, status = ? WHERE id = ?", status, id)
	if err != nil {
		return fmt.Errorf("clear node pid: %w", err)
	}
	return nil
}

// --- MessageLog CRUD ---

// CreateMessage 创建消息日志
func (d *DB) CreateMessage(msg *MessageLog) error {
	_, err := d.db.Exec(
		"INSERT INTO message_logs (flow_id, node_id, direction, raw_content, summary) VALUES (?, ?, ?, ?, ?)",
		msg.FlowID, msg.NodeID, msg.Direction, msg.RawContent, msg.Summary,
	)
	if err != nil {
		return fmt.Errorf("insert message: %w", err)
	}
	return nil
}

// GetMessagesByFlowID 获取流程的消息日志
func (d *DB) GetMessagesByFlowID(flowID string, limit int) ([]*MessageLog, error) {
	query := "SELECT id, flow_id, node_id, direction, raw_content, summary, created_at FROM message_logs WHERE flow_id = ? ORDER BY created_at ASC"
	args := []interface{}{flowID}
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query messages: %w", err)
	}
	defer rows.Close()

	var messages []*MessageLog
	for rows.Next() {
		var msg MessageLog
		var summary sql.NullString
		err := rows.Scan(
			&msg.ID, &msg.FlowID, &msg.NodeID, &msg.Direction,
			&msg.RawContent, &summary, &msg.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan message row: %w", err)
		}
		if summary.Valid {
			msg.Summary = summary.String
		}
		messages = append(messages, &msg)
	}
	return messages, rows.Err()
}

// GetMessagesByNodeID 获取节点的消息日志
func (d *DB) GetMessagesByNodeID(flowID string, nodeID string, limit int) ([]*MessageLog, error) {
	query := "SELECT id, flow_id, node_id, direction, raw_content, summary, created_at FROM message_logs WHERE flow_id = ? AND node_id = ? ORDER BY created_at ASC"
	args := []interface{}{flowID, nodeID}
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query messages by node: %w", err)
	}
	defer rows.Close()

	var messages []*MessageLog
	for rows.Next() {
		var msg MessageLog
		var summary sql.NullString
		err := rows.Scan(
			&msg.ID, &msg.FlowID, &msg.NodeID, &msg.Direction,
			&msg.RawContent, &summary, &msg.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan message row: %w", err)
		}
		if summary.Valid {
			msg.Summary = summary.String
		}
		messages = append(messages, &msg)
	}
	return messages, rows.Err()
}
