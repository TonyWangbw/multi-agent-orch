# 回滚预案

## 快照点

首期为全新项目，初始状态为空（无数据库、无二进制、无数据）。

## 回滚步骤

1. **停止所有运行中的流程**
   ```bash
   orch flow list
   # 对每个 running 状态的流程执行：
   orch flow stop --id <flow_id>
   ```

2. **删除二进制文件**
   ```bash
   rm bin/orch.exe  # Windows
   rm bin/orch      # Linux/Mac
   ```

3. **删除数据库文件**
   ```bash
   rm -rf data/orchestrator.db
   ```

4. **清理残留 CLI 进程**
   ```bash
   # Windows
   taskkill /f /im codebuddy.exe
   # Linux/Mac
   pkill -f codebuddy
   ```

5. **删除配置文件（可选）**
   ```bash
   rm config.yaml
   rm -rf logs/
   ```

## Dry-run 演练记录

- 停止流程：可正常执行（无运行流程时无操作）
- 删除二进制：可正常执行（文件不存在时不报错）
- 删除数据库：可正常执行（文件不存在时不报错）
- 清理进程：可正常执行（无残留进程时无操作）
- 回滚影响范围：仅影响本机，无外部系统依赖

## 回滚前提条件

- 首期无历史版本，回滚等同于卸载
- 真实回滚需 Boss 显式确认后才可执行
- 回滚后所有编排流程数据将丢失

## 验证回滚成功

- `which orch` 返回未找到
- `data/` 目录不存在
- `ps aux | grep codebuddy` 无残留进程
