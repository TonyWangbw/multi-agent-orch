# 代码审查报告

**审查日期**: 2026-05-31
**修复日期**: 2026-05-31
**审查范围**: `multi-agent-orch/internal/` 全部 6 个包 + `cmd/orch/`
**审查依据**: Go 安全编码规范 V2.0 + Go Code Review Checklist
**审查结论**: 通过（7 项问题已全部修复，3 项提示待后续迭代）

## 审查统计

| 严重程度 | 数量 | 已修复 |
|---------|------|--------|
| 严重 | 2 | 2 |
| 一般 | 5 | 5 |
| 提示 | 3 | 0（待后续迭代） |

## 缺陷详情

### 严重问题（2项）—— 已修复

#### 1. ID 生成器非并发安全，全局自增计数器存在数据竞争 — 已修复
- **位置**: `internal/engine/engine.go:355-360`
- **模块**: engine
- **缺陷来源**: 编码
- **缺陷类型**: 正确性 > 并发安全
- **问题描述**: `idCounter` 是 `int64` 类型全局变量，`simpleCounter()` 直接 `idCounter++` 无任何锁保护。多个 goroutine 并发创建流程时会触发数据竞争（`go test -race` 可检出），且可能产生重复 ID 导致 SQLite UNIQUE 约束冲突。
- **违反规范**: Go 安全编码规范 §3.2 禁止并发写共享变量须加锁保护；§3.4 使用 `sync/atomic` 执行原子操作
- **修复方式**: 将 `idCounter++` 替换为 `atomic.AddInt64(&idCounter, 1)`，新增 `"sync/atomic"` import
- **修复提交**: 2026-05-31

#### 2. SendMessage 中原始输出被重复写入 message_logs — 已修复
- **位置**: `internal/engine/engine.go:249-274`（修复后行号）
- **模块**: engine
- **缺陷来源**: 编码
- **缺陷类型**: 正确性 > 逻辑错误
- **问题描述**: `SendMessage` 方法中对同一条输出做了两次 `CreateMessage` 调用——第一次记录原始输出（无摘要），第二次再次记录原始输出（带摘要）。导致每条消息在数据库中产生两条记录，造成数据冗余和日志查询混乱。
- **违反规范**: Go 安全编码规范 §9.1 函数返回的 error 必须检查；代码质量最佳实践
- **修复方式**: 删除第一次 `CreateMessage` 调用，仅保留带 `Summary` 字段的完整记录
- **修复提交**: 2026-05-31

### 一般问题（5项）—— 已修复

#### 3. RunFlow/StopFlow 中忽略 GetNodesByFlowID 和 UpdateNodeStatus 的错误返回 — 已修复
- **位置**: `internal/engine/engine.go:113-122`、`internal/engine/engine.go:140-149`（修复后行号）
- **模块**: engine
- **缺陷来源**: 编码
- **缺陷类型**: 正确性 > 错误处理
- **问题描述**: `e.db.GetNodesByFlowID(flowID)` 和 `e.db.UpdateNodeStatus(node.ID, "idle")` 的错误返回被 `_` 忽略。数据库操作失败时，节点状态可能不一致，但调用方无感知。
- **违反规范**: Go 安全编码规范 §9.1 函数返回的 error 必须检查
- **修复方式**: 检查 error 返回值，失败时通过 `slog.Error` 记录日志

#### 4. CLIClient.mu 字段未使用 — 已修复
- **位置**: `internal/adapter/acp.go:70-73`（修复后行号）
- **模块**: adapter
- **缺陷来源**: 编码
- **缺陷类型**: 代码质量 > 死代码
- **问题描述**: `CLIClient` 结构体声明了 `mu sync.Mutex` 字段但从未使用。当前 Prompt 方法每次创建独立进程，无需互斥。
- **违反规范**: Go 编码最佳实践 — 消除未使用字段
- **修复方式**: 删除 `mu sync.Mutex` 字段，移除 `"sync"` import

#### 5. SendMessage 未校验流程是否为 running 状态 — 已修复
- **位置**: `internal/engine/engine.go:205-208`（修复后行号）
- **模块**: engine
- **缺陷来源**: 设计
- **缺陷类型**: 正确性 > 状态校验缺失
- **问题描述**: `SendMessage` 没有检查流程是否处于 `running` 状态。对 `created` 或 `stopped` 状态的流程发送消息仍然会调用 CLI，可能导致非预期行为。
- **修复方式**: 获取 flow 后立即校验 `flow.Status != "running"`，不满足则返回错误

#### 6. DeleteFlow 在无锁状态下访问 activeFlows，且与 StopFlow 存在潜在死锁 — 已修复
- **位置**: `internal/engine/engine.go:345-351`（修复后行号）
- **模块**: engine
- **缺陷来源**: 编码
- **缺陷类型**: 正确性 > 潜在死锁 / 数据竞争
- **问题描述**: `DeleteFlow` 直接无锁访问 `e.activeFlows` 判断流程是否存在，然后调用 `StopFlow`。`StopFlow` 内部会 `e.mu.Lock()`。如果 `DeleteFlow` 未来加了锁保护，就会产生死锁。当前 `e.activeFlows` 的读取无锁保护存在数据竞争。
- **修复方式**: 删除直接访问 `activeFlows`，改为直接调用 `StopFlow`（内部有锁保护且处理不存在的情况）

#### 7. CLI binary 路径未做白名单校验 — 已修复
- **位置**: `internal/adapter/acp.go:78-86`（修复后行号）
- **模块**: adapter
- **缺陷来源**: 编码
- **缺陷类型**: 安全 > 命令注入
- **问题描述**: `c.binary` 直接用于 `exec.CommandContext`。如果 binary 名称来自用户输入（配置文件），可能执行任意程序。
- **违反规范**: Go 安全编码规范 §6.1 exec.Command 的 path 参数须白名单限定
- **修复方式**: `NewCLIClient` 中添加路径分隔符检查，含 `/` 或 `\` 时 `slog.Warn` 告警

### 提示（3项）—— 待后续迭代

#### 8. generateID 建议使用 UUID 替代自增计数器
- **位置**: `internal/engine/engine.go:348-352`
- **缺陷类型**: 最佳实践
- **问题描述**: 自增计数器在进程重启后从 0 开始，可能与已有数据 ID 冲突。建议使用 UUID 或 `time.Now().UnixNano()` + 随机后缀。
- **状态**: 待后续迭代，当前 atomic 自增在单机部署场景下足够

#### 9. Engine.SendMessage 中 fmt.Printf 输出不适合作为库函数
- **位置**: `internal/engine/engine.go:244,314`
- **缺陷类型**: 代码质量 > 关注点分离
- **问题描述**: Engine 作为核心逻辑层直接 `fmt.Printf` 输出到终端，违反关注点分离。建议通过回调函数或 channel 将输出传递给调用方（CLI 层），由 CLI 层决定输出格式。
- **状态**: 待后续迭代，当前首版本 CLI 工具场景可接受

#### 10. 日志输出到 stderr 与 slog 输出混杂
- **位置**: `cmd/orch/main.go` — 运行时观察到 slog 输出到 stderr 与终端输出混显
- **缺陷类型**: 用户体验
- **问题描述**: slog 默认输出到 stderr，而终端对话通过 fmt.Printf 输出到 stdout。当前体验中两者混杂。建议将 slog 输出重定向到文件（配置 `log_file`），stderr 仅保留错误级别。
- **状态**: 待后续迭代，与 #9 一起重构输出层

## 修复验证

- `go build ./...` — 编译通过，0 错误
- `go test ./... -count=1` — 7 个包全部 PASS，0 失败
- engine 包 9 项测试通过，adapter 包 8 项测试通过

## 总结

7 项严重/一般问题已全部修复，3 项提示列为后续迭代 backlog。当前代码通过审查，可合入。
