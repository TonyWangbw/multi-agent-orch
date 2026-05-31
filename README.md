# 多智能体协作编排平台

CLI 工具，用于创建和管理多个 codebuddy-cli 实例的编排流程。

## 构建与运行

```bash
make build
./bin/orch flow create --name demo --nodes "coder,reviewer" --edges "coder->reviewer"
```

## 测试

```bash
make test
```
