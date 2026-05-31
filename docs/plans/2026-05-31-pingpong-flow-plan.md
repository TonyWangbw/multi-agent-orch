# 乒乓循环编排实施计划

## 元数据

```yaml
specPath: docs/specs/2026-05-31-pingpong-flow-spec-lite.md
taskType: new-feature
finalTier: M
gateStatus: pass
```

## 合同摘要

- 目标：为编排引擎新增通用乒乓循环编排能力，首个应用为专利挖掘双 Agent 对话
- 允许修改：`internal/pingpong/`（新增）、`internal/db/db.go`、`internal/config/config.go`、`cmd/orch/main.go`、`config.yaml`
- 禁止修改：`internal/engine/engine.go`、`internal/adapter/acp.go`
- 验证命令：`go build ./...`、`go test ./internal/pingpong/ -v`、`go test ./... -count=1`
- 交付证据：单元测试通过、全量测试通过
- 人工确认点：预设 prompt 内容评审
- owner：Boss

## 风险缓解

| 风险 | 缓解措施 |
|---|---|
| 循环不终止 | 最大轮次硬限制 + 关键词检测双保险 |
| writer 未输出终止关键词 | 默认 prompt 明确指示输出格式；最大轮次兜底 |
| 乒乓状态异常中断 | 每轮状态持久化到 DB，支持 status 查看 |

---

## Batch 1：共享边界（DB + Config）

### Task 1: 新增 pingpong_runs 表及 CRUD

- **目标**：DB 层新增 `pingpong_runs` 表和对应 CRUD 方法
- **允许修改**：`internal/db/db.go`
- **禁止修改**：其他文件
- **验证命令**：`go test ./internal/db/ -v -run PingPong`
- **交付证据**：新增 CRUD 测试全部 PASS
- **依赖项**：无

数据模型：

```sql
CREATE TABLE IF NOT EXISTS pingpong_runs (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    keywords TEXT NOT NULL DEFAULT '[]',      -- 用户输入的关键词列表 JSON
    agent_a_name TEXT NOT NULL,               -- 第一个 agent 名称（如 reader）
    agent_b_name TEXT NOT NULL,               -- 第二个 agent 名称（如 writer）
    agent_a_prompt TEXT NOT NULL DEFAULT '',  -- agent_a 的 system prompt
    agent_b_prompt TEXT NOT NULL DEFAULT '',  -- agent_b 的 system prompt
    status TEXT NOT NULL DEFAULT 'created',   -- created, running, completed, stopped, error
    current_round INTEGER NOT NULL DEFAULT 0, -- 当前轮次
    max_rounds INTEGER NOT NULL DEFAULT 100,  -- 最大轮次
    stop_keywords TEXT NOT NULL DEFAULT '[]', -- 终止关键词列表 JSON
    final_output TEXT NOT NULL DEFAULT '',    -- 最终输出（如专利书全文）
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

CRUD 方法：
- `CreatePingPongRun(run *PingPongRun) error`
- `GetPingPongRun(id string) (*PingPongRun, error)`
- `ListPingPongRuns() ([]*PingPongRun, error)`
- `UpdatePingPongStatus(id string, status string, currentRound int) error`
- `UpdatePingPongFinalOutput(id string, output string) error`
- `DeletePingPongRun(id string) error`

### Task 2: Config 新增 PingPong 配置段

- **目标**：Config 结构体新增 `PingPong` 嵌套配置，config.yaml 同步更新
- **允许修改**：`internal/config/config.go`、`config.yaml`
- **禁止修改**：其他文件
- **验证命令**：`go test ./internal/config/ -v`
- **交付证据**：现有测试仍 PASS，新配置可加载
- **依赖项**：无

新增配置：

```go
type PingPongConfig struct {
    MaxRounds     int      `yaml:"max_rounds"`
    StopKeywords  []string `yaml:"stop_keywords"`
    AgentAPrompt  string   `yaml:"agent_a_prompt"`
    AgentBPrompt  string   `yaml:"agent_b_prompt"`
    AgentAName    string   `yaml:"agent_a_name"`
    AgentBName    string   `yaml:"agent_b_name"`
}
```

config.yaml 新增：

```yaml
pingpong:
  max_rounds: 100
  stop_keywords:
    - "专利书编写完成"
    - "PATENT_WRITING_DONE"
  agent_a_name: "reader"
  agent_b_name: "writer"
  agent_a_prompt: ""  # 空则使用内置默认 prompt
  agent_b_prompt: ""  # 空则使用内置默认 prompt
```

---

## Batch 2：核心实现（乒乓引擎 + Prompt）

### Task 3: 预设 Prompt 模板

- **目标**：内置专利挖掘场景的 reader 和 writer 默认 system prompt
- **允许修改**：`internal/pingpong/prompt.go`（新增）
- **禁止修改**：其他文件
- **验证命令**：`go test ./internal/pingpong/ -v -run Prompt`
- **交付证据**：prompt 模板变量可正确渲染，测试 PASS
- **依赖项**：无

Reader prompt 核心要点：
- 角色：专利点挖掘专家
- 任务：根据用户关键词和技术领域，挖掘可申请专利的技术创新点
- 输出格式：每个专利点包含「名称 / 技术领域 / 核心创新 / 技术方案简述」

Writer prompt 核心要点：
- 角色：专利评审与编写专家
- 任务：评判 reader 输出的专利点，评判不够则指出不足要求补充，评判足够则编写专利书
- 输出格式：评判结果（通过/需补充）+ 专利书正文（当评判通过时）
- 终止关键词：`专利书编写完成` 或 `PATENT_WRITING_DONE`

### Task 4: 乒乓循环引擎

- **目标**：实现 `PingPong` 引擎核心逻辑——状态机、循环控制、终止检测
- **允许修改**：`internal/pingpong/pingpong.go`（新增）
- **禁止修改**：`internal/engine/engine.go`、`internal/adapter/acp.go`
- **验证命令**：`go test ./internal/pingpong/ -v -run PingPong`
- **交付证据**：单元测试覆盖状态机转换、终止条件、轮次计数
- **依赖项**：Task 1, Task 2, Task 3

核心结构：

```go
type PingPongEngine struct {
    db      *db.DB
    adapter *adapter.CLIClient
    summary *summary.Service
    config  config.PingPongConfig
}

// Run 执行乒乓循环：agentA → agentB → agentA → ... 直至终止
func (e *PingPongEngine) Run(ctx context.Context, runID string) error

// CreateRun 创建乒乓运行实例
func (e *PingPongEngine) CreateRun(name string, keywords []string, overrides *PingPongOverrides) (*db.PingPongRun, error)

// GetRunStatus 获取运行状态
func (e *PingPongEngine) GetRunStatus(runID string) (*PingPongRunStatus, error)

// ListRuns 列出所有运行
func (e *PingPongEngine) ListRuns() ([]*db.PingPongRun, error)

// StopRun 停止运行
func (e *PingPongEngine) StopRun(runID string) error

// DeleteRun 删除运行
func (e *PingPongEngine) DeleteRun(runID string) error
```

状态机：`created → running → completed|stopped|error`

循环逻辑：
1. 第一轮：将 keywords 构建 prompt 发给 agentA（reader）
2. agentA 输出 → 摘要 → 发给 agentB（writer）
3. agentB 输出 → 检查终止关键词 → 若包含则终止并保存 final_output
4. 若未终止 → agentB 输出摘要 → 发回 agentA → 回到步骤 2
5. 每轮递增 currentRound，达到 maxRounds 时终止

### Task 5: 乒乓引擎单元测试

- **目标**：为乒乓引擎编写完整单元测试
- **允许修改**：`internal/pingpong/pingpong_test.go`（新增）
- **禁止修改**：其他文件
- **验证命令**：`go test ./internal/pingpong/ -v`
- **交付证据**：覆盖率 ≥ 80%
- **依赖项**：Task 4

测试用例：
- TestCreateRun — 创建运行实例
- TestCreateRunWithOverrides — 自定义 prompt 覆盖默认值
- TestRunStopKeywords — 终止关键词检测
- TestRunMaxRounds — 最大轮次保护
- TestStopRun — 手动停止
- TestDeleteRun — 删除运行记录
- TestListRuns — 列出运行
- TestGetRunStatus — 获取状态
- TestPingPongStateTransition — 状态机转换

---

## Batch 3：集成与收尾

### Task 6: CLI pingpong 子命令

- **目标**：新增 `orch pingpong run/list/status/stop/delete` 子命令
- **允许修改**：`cmd/orch/main.go`
- **禁止修改**：其他文件
- **验证命令**：`go build ./...`、`./bin/orch pingpong --help`
- **交付证据**：编译通过，help 输出正确
- **依赖项**：Task 4

命令：
- `orch pingpong run --keywords "关键词1,关键词2" [--name "名称"] [--max-rounds 100]`
- `orch pingpong list`
- `orch pingpong status --id <id>`
- `orch pingpong stop --id <id>`
- `orch pingpong delete --id <id>`

### Task 7: 集成测试 + 全量回归

- **目标**：端到端集成测试 + 现有测试回归验证
- **允许修改**：`tests/integration/e2e_test.go`
- **禁止修改**：其他文件
- **验证命令**：`go test ./... -count=1`
- **交付证据**：全量测试通过
- **依赖项**：Task 6

集成测试用例：
- TestPingPongPatentRun — 真实 CLI 乒乓对话，验证循环执行和终止

### Task 8: 代码审查

- **目标**：对本次所有新增代码进行审查
- **允许修改**：按审查意见修改
- **禁止修改**：无限制（审查发现的问题需要修改）
- **验证命令**：`go test ./... -count=1`
- **交付证据**：审查报告，所有严重/一般问题已修复
- **依赖项**：Task 7
