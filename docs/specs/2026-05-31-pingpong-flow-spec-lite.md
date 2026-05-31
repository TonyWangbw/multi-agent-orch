# 乒乓循环编排（PingPong Flow）Spec-Lite

## 元信息

- taskId: 20260531-pingpong-flow
- createdAt: 2026-05-31 17:30
- sourceCommand: /spec-lite
- explore: false

## 1. 目标

为编排引擎新增通用"乒乓循环"编排能力：两个 Agent 交替运行，一个的输出作为另一个的输入，直到满足终止条件（关键词触发或达到最大轮次）。首个应用场景为专利挖掘（reader 挖掘专利点 → writer 评判/编写专利书）。

## 2. 范围外

- 不做多于两个 Agent 的循环编排（N 方乒乓留后续迭代）
- 不做 Web UI，仅 CLI 交互
- 不做对话过程的实时流式输出到终端（每轮完成后输出）
- 不做专利书格式校验或法律合规审查
- 不修改现有 `flow create/run/send` 命令的行为

## 3. 接口与数据影响

- API/CLI: 新增 `orch pingpong run` 子命令
- DB: 新增 `pingpong_runs` 表记录运行历史；`message_logs` 复用现有表
- Config: 新增 `pingpong` 配置段（max_rounds, stop_keywords, agent_a/b system_prompt）
- 外部契约是否变化: 否（现有 flow 命令不受影响）

## 4. 需求澄清结论（必填）

- 业务目标与成功标准: 用户输入关键词，自动运行 reader→writer 交替对话，最终输出专利书。成功标准：能跑完整个循环并产出最终专利书文本
- 用户/调用方与使用场景: 专利工程师/研发人员，输入技术关键词后自动挖掘专利点并编写专利书
- 触发入口与交互路径: CLI 命令 `orch pingpong run --keywords "关键词1,关键词2"`
- 交付形态: CLI 命令 + 乒乓循环引擎 + 专利挖掘预设 prompt
- 关键数据对象与范围边界:
  - 新增: PingPongRun 记录、乒乓循环状态机、预设 prompt 模板
  - 修改: config.yaml 新增 pingpong 段、engine 包新增方法
  - 不改: 现有 flow 相关代码
- 外部契约与兼容影响: 无破坏性变更
- 非功能约束（性能/安全/稳定性/合规）: 最大轮次保护防死循环；单机单进程
- 观测与运维要求（日志/监控/告警）: 沿用 slog 结构化日志，记录每轮交互
- 日志实现策略: 沿用项目现有 slog 结构
- 日志字段规范: traceId/module/action/result/errorCode/durationMs
- 日志语言约束: English only
- 控制台输出策略: 每轮完成后输出摘要到终端，最终输出完整专利书

## 5. 方案方向确认（必填）

### 5.1 候选方向

| 方向 | 核心思路 | 优点 | 风险/代价 | 适用前提 |
|---|---|---|---|---|
| A | 新增 patent 子命令 + 循环引擎，循环逻辑专用化 | 简单直接，开发快 | 循环逻辑耦合，未来新增其他乒乓场景需重复开发 | 只有一种乒乓场景 |
| B | 通用乒乓循环编排引擎，patent 为预设模板 | 扩展性好，新增乒乓场景只需配模板 | 抽象层增加少量复杂度 | 预期有多种双 Agent 乒乓场景 |

### 5.2 用户确认结果

- selectedDirection: B
- rejectedDirections: A
- rejectionReason: 此特性是项目自身应具备的通用能力，不应做成专用逻辑
- userHardConstraints: 乒乓循环必须是引擎通用能力，专利挖掘只是预设应用
- alternativeDirection: 无
- unresolvedItems: 无

## 6. 风险与缓解

| 风险 | 等级 | 缓解措施 |
|---|---|---|
| 循环不终止 | low | 最大轮次硬限制（默认 100）+ 关键词检测 |
| writer 未输出终止关键词 | med | 默认 prompt 明确指示输出格式；最大轮次兜底 |
| CLI 进程输出过长 | low | 每轮摘要后传递，非全文传递 |
| 乒乓状态机异常中断 | low | 每轮状态持久化到 DB，支持查看/恢复 |

## 7. 验收标准

- [ ] 功能验收：`orch pingpong run --keywords "AI,机器学习"` 能跑完 reader→writer 循环并输出专利书
- [ ] 功能验收：达到最大轮次自动终止并输出当前进度
- [ ] 功能验收：writer 输出包含终止关键词时自动停止循环
- [ ] 功能验收：`orch pingpong list` 列出历史运行
- [ ] 功能验收：`orch pingpong status --id <id>` 查看运行详情
- [ ] 测试验收：单元测试覆盖乒乓状态机转换、终止条件检测、轮次计数
- [ ] 测试验收：集成测试验证真实 CLI 交替对话
- [ ] 文档验收：config.yaml 中 pingpong 配置段有注释说明

## 8. 回滚方案

删除 `internal/pingpong/` 包、`cmd/orch/main.go` 中 pingpong 子命令注册、config.yaml 中 pingpong 段。现有 flow 命令不受影响。

## 9. 分级输入

- estimatedChangedFiles: 8
- impactedModules: 3（engine, config, cmd）
- hasContractChange: true（新增 pingpong_runs 表、新增 CLI 命令）
- hasSecurityOrPermissionImpact: false
- hasDataOrStateMigration: false
- hasCriticalPathPerformanceImpact: false
- isProductionIncidentFix: false

评分：变更文件 8:+2, 模块 3:+2, 契约变更:+3 = 7 → H

## 10. GateContext

```yaml
GateContext:
  taskId: "20260531-pingpong-flow"
  taskType: "new-feature"
  workflow: "spec-first"
  recommendedTier: "H"
  finalTier: "M"
  overrideReason: "虽有契约变更但为纯新增（不破坏现有接口），影响范围可控，3 个模块内部新增代码，无外部集成"
  specPath: "docs/specs/2026-05-31-pingpong-flow-spec-lite.md"
  planPath: ""
  requiredChecks:
    - spec_exists
    - clarification_defined
    - solution_direction_confirmed
    - acceptance_defined
    - risks_defined
  completedChecks:
    - spec_exists
    - clarification_defined
    - solution_direction_confirmed
    - acceptance_defined
    - risks_defined
  gateStatus: "pass"
```

## 11. GateResult

```yaml
GateResult:
  status: "pass"
  tier: "M"
  missing: []
  nextCommand: "/write-plan spec=docs/specs/2026-05-31-pingpong-flow-spec-lite.md tier=M"
  message: "乒乓循环编排特性规格通过，推荐 M 级流程"
```

## 12. TaskContract

```yaml
TaskContract:
  templatePath: ".codebuddy/templates/task-contracts/new-feature.md"
  taskType: "new-feature"
  objective: "为编排引擎新增通用乒乓循环编排能力，首个应用为专利挖掘双 Agent 对话"
  background: "当前引擎仅支持手动 send 触发单轮对话，缺少自动循环编排能力。专利挖掘场景需要 reader 和 writer 交替对话直至完成"
  editablePaths:
    - "internal/pingpong/"
    - "internal/db/db.go"
    - "internal/config/config.go"
    - "cmd/orch/main.go"
    - "config.yaml"
  forbiddenPaths:
    - "internal/engine/engine.go"（不修改现有 flow 引擎）
    - "internal/adapter/acp.go"（适配器不变）
  relatedFiles:
    - "internal/summary/service.go"
    - "internal/db/db.go"
  verificationCommands:
    - "go build ./..."
    - "go test ./internal/pingpong/ -v"
    - "go test ./... -count=1"
  deliverables:
    - "internal/pingpong/pingpong.go — 乒乓循环引擎"
    - "internal/pingpong/prompt.go — 预设 prompt 模板"
    - "internal/pingpong/pingpong_test.go — 单元测试"
    - "cmd/orch/main.go — pingpong 子命令注册"
    - "internal/db/db.go — 新增 pingpong_runs 表"
    - "internal/config/config.go — 新增 PingPong 配置"
    - "config.yaml — 新增 pingpong 段"
  evidence:
    - "go test ./internal/pingpong/ -v 输出"
    - "go test ./... -count=1 全量通过"
  humanCheckpoints:
    - "预设 prompt 内容评审"
    - "终止关键词列表确认"
  owner: "Boss"
  outOfScopeHandling: "超边界时先停止并回退上游规格/人工确认"
```

## 13. 追踪链接

- historicalSpecPath:
- requirementAnalysisPath:
- brainstormPath:
- researchPath:
- designPath:
- testStrategyPath:
- planPath:
- testcasePath:
- testcaseAnalysisPath:
- implementationProgressPath:
- implementationSummaryPath:
- reviewReportPath:
- coverageMatrixPath:
- coverageReportPath:
- securityReviewReportPath:
- perfBaselinePath:
- perfReportPath:
- systemTestReportPath:
- releaseNotesPath:
- rollbackPlaybookPath:

## 14. 需求覆盖矩阵

> 无上游需求分析文档，本节为空。
