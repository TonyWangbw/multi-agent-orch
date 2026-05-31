// 多智能体协作编排平台 CLI 入口
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/boss/multi-agent-orch/internal/adapter"
	"github.com/boss/multi-agent-orch/internal/config"
	"github.com/boss/multi-agent-orch/internal/db"
	"github.com/boss/multi-agent-orch/internal/engine"
	"github.com/boss/multi-agent-orch/internal/pingpong"
	"github.com/boss/multi-agent-orch/internal/summary"
	"github.com/spf13/cobra"
)

var (
	cfgPath      string
	flowName     string
	flowNodes    string
	flowEdges    string
	flowID       string
	nodeName     string
	message      string
	followLogs   bool
	ppKeywords   string
	ppName       string
	ppMaxRounds  int
	ppID         string
)

var rootCmd = &cobra.Command{
	Use:   "orch",
	Short: "多智能体协作编排平台",
	Long:  "通过 CLI 命令创建和管理多个 codebuddy-cli 实例的编排流程",
}

// 全局引擎实例
var eng *engine.Engine
var ppEng *pingpong.PingPongEngine

func initCommands() {
	rootCmd.PersistentFlags().StringVarP(&cfgPath, "config", "c", "config.yaml", "配置文件路径")

	// flow create
	createCmd := &cobra.Command{
		Use:   "create",
		Short: "创建编排流程",
		RunE:  runCreate,
	}
	createCmd.Flags().StringVarP(&flowName, "name", "n", "", "流程名称（必填）")
	createCmd.Flags().StringVarP(&flowNodes, "nodes", "", "", "节点列表，逗号分隔（必填，如 coder,reviewer）")
	createCmd.Flags().StringVarP(&flowEdges, "edges", "", "", "边列表，逗号分隔（必填，如 coder->reviewer）")
	createCmd.MarkFlagRequired("name")
	createCmd.MarkFlagRequired("nodes")
	createCmd.MarkFlagRequired("edges")

	// flow list
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "列出所有流程",
		RunE:  runList,
	}

	// flow run
	runCmd := &cobra.Command{
		Use:   "run",
		Short: "启动流程运行",
		RunE:  runRun,
	}
	runCmd.Flags().StringVarP(&flowID, "id", "", "", "流程 ID（必填）")
	runCmd.MarkFlagRequired("id")

	// flow stop
	stopCmd := &cobra.Command{
		Use:   "stop",
		Short: "停止流程运行",
		RunE:  runStop,
	}
	stopCmd.Flags().StringVarP(&flowID, "id", "", "", "流程 ID（必填）")
	stopCmd.MarkFlagRequired("id")

	// flow status
	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "查看运行状态",
		RunE:  runStatus,
	}
	statusCmd.Flags().StringVarP(&flowID, "id", "", "", "流程 ID（必填）")
	statusCmd.MarkFlagRequired("id")

	// flow logs
	logsCmd := &cobra.Command{
		Use:   "logs",
		Short: "查看消息日志",
		RunE:  runLogs,
	}
	logsCmd.Flags().StringVarP(&flowID, "id", "", "", "流程 ID（必填）")
	logsCmd.Flags().StringVarP(&nodeName, "node", "", "", "节点名称（可选）")
	logsCmd.Flags().BoolVarP(&followLogs, "follow", "f", false, "持续跟踪日志")
	logsCmd.MarkFlagRequired("id")

	// flow send
	sendCmd := &cobra.Command{
		Use:   "send",
		Short: "手动发送消息",
		RunE:  runSend,
	}
	sendCmd.Flags().StringVarP(&flowID, "id", "", "", "流程 ID（必填）")
	sendCmd.Flags().StringVarP(&nodeName, "node", "", "", "目标节点名称（必填）")
	sendCmd.Flags().StringVarP(&message, "message", "m", "", "消息内容（必填）")
	sendCmd.MarkFlagRequired("id")
	sendCmd.MarkFlagRequired("node")
	sendCmd.MarkFlagRequired("message")

	// flow delete
	deleteCmd := &cobra.Command{
		Use:   "delete",
		Short: "删除流程",
		RunE:  runDelete,
	}
	deleteCmd.Flags().StringVarP(&flowID, "id", "", "", "流程 ID（必填）")
	deleteCmd.MarkFlagRequired("id")

	// flow 父命令
	flowCmd := &cobra.Command{
		Use:   "flow",
		Short: "编排流程管理",
		PersistentPreRunE: initEngine,
	}
	flowCmd.AddCommand(createCmd, listCmd, runCmd, stopCmd, statusCmd, logsCmd, sendCmd, deleteCmd)

	rootCmd.AddCommand(flowCmd)

	// --- pingpong 子命令 ---

	// pingpong run
	ppRunCmd := &cobra.Command{
		Use:   "run",
		Short: "运行乒乓循环编排（如专利挖掘）",
		Long:  "启动双 Agent 交替对话，直至终止关键词触发或达到最大轮次",
		RunE:  runPingPongRun,
	}
	ppRunCmd.Flags().StringVarP(&ppKeywords, "keywords", "k", "", "关键词列表，逗号分隔（必填，如 AI,机器学习）")
	ppRunCmd.Flags().StringVarP(&ppName, "name", "n", "", "运行名称（可选，默认自动生成）")
	ppRunCmd.Flags().IntVarP(&ppMaxRounds, "max-rounds", "", 0, "最大轮次（0 则使用配置文件默认值）")
	ppRunCmd.MarkFlagRequired("keywords")

	// pingpong list
	ppListCmd := &cobra.Command{
		Use:   "list",
		Short: "列出所有乒乓运行",
		RunE:  runPingPongList,
	}

	// pingpong status
	ppStatusCmd := &cobra.Command{
		Use:   "status",
		Short: "查看乒乓运行状态",
		RunE:  runPingPongStatus,
	}
	ppStatusCmd.Flags().StringVarP(&ppID, "id", "", "", "运行 ID（必填）")
	ppStatusCmd.MarkFlagRequired("id")

	// pingpong stop
	ppStopCmd := &cobra.Command{
		Use:   "stop",
		Short: "停止乒乓运行",
		RunE:  runPingPongStop,
	}
	ppStopCmd.Flags().StringVarP(&ppID, "id", "", "", "运行 ID（必填）")
	ppStopCmd.MarkFlagRequired("id")

	// pingpong delete
	ppDeleteCmd := &cobra.Command{
		Use:   "delete",
		Short: "删除乒乓运行记录",
		RunE:  runPingPongDelete,
	}
	ppDeleteCmd.Flags().StringVarP(&ppID, "id", "", "", "运行 ID（必填）")
	ppDeleteCmd.MarkFlagRequired("id")

	// pingpong 父命令
	ppCmd := &cobra.Command{
		Use:   "pingpong",
		Short: "乒乓循环编排（双 Agent 交替对话）",
		PersistentPreRunE: initPingPongEngine,
	}
	ppCmd.AddCommand(ppRunCmd, ppListCmd, ppStatusCmd, ppStopCmd, ppDeleteCmd)

	rootCmd.AddCommand(ppCmd)
}

// initEngine 初始化引擎（flow 命令的 PersistentPreRunE）
func initEngine(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	database, err := db.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}

	cliAdapter := adapter.NewCLIClient(cfg.CLIBinary, cfg.SummaryModel, ".")
	summarySvc := summary.NewService(summary.Config{
		Enabled:   cfg.SummaryEnabled,
		APIKey:    cfg.GetSummaryAPIKey(),
		APIBase:   cfg.SummaryAPIBase,
		Model:     cfg.SummaryModel,
		MaxTokens: cfg.SummaryMaxTokens,
	})

	eng = engine.New(database, cliAdapter, summarySvc)
	return nil
}

// initPingPongEngine 初始化乒乓引擎（pingpong 命令的 PersistentPreRunE）
func initPingPongEngine(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	database, err := db.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}

	cliAdapter := adapter.NewCLIClient(cfg.CLIBinary, cfg.SummaryModel, ".")
	summarySvc := summary.NewService(summary.Config{
		Enabled:   cfg.SummaryEnabled,
		APIKey:    cfg.GetSummaryAPIKey(),
		APIBase:   cfg.SummaryAPIBase,
		Model:     cfg.SummaryModel,
		MaxTokens: cfg.SummaryMaxTokens,
	})

	ppEng = pingpong.NewPingPongEngine(database, cliAdapter, summarySvc, cfg.PingPong)
	return nil
}

func runCreate(cmd *cobra.Command, args []string) error {
	nodes := parseList(flowNodes)
	edges := parseEdges(flowEdges)

	flow, err := eng.CreateFlow(flowName, nodes, edges)
	if err != nil {
		return err
	}

	fmt.Printf("流程创建成功\n  ID: %s\n  名称: %s\n  节点: %s\n  边: %s\n",
		flow.ID, flow.Name, flowNodes, flowEdges)
	return nil
}

func runList(cmd *cobra.Command, args []string) error {
	flows, err := eng.ListFlows()
	if err != nil {
		return err
	}

	if len(flows) == 0 {
		fmt.Println("暂无流程")
		return nil
	}

	fmt.Printf("%-20s %-15s %-10s %-8s\n", "ID", "名称", "状态", "边数")
	fmt.Println(strings.Repeat("-", 55))
	for _, f := range flows {
		fmt.Printf("%-20s %-15s %-10s %-8d\n", f.ID, f.Name, f.Status, len(f.Edges))
	}
	return nil
}

func runRun(cmd *cobra.Command, args []string) error {
	if err := eng.RunFlow(context.Background(), flowID); err != nil {
		return err
	}

	fmt.Printf("流程 %s 已启动\n", flowID)
	return nil
}

func runStop(cmd *cobra.Command, args []string) error {
	if err := eng.StopFlow(flowID); err != nil {
		return err
	}

	fmt.Printf("流程 %s 已停止\n", flowID)
	return nil
}

func runStatus(cmd *cobra.Command, args []string) error {
	status, err := eng.GetStatus(flowID)
	if err != nil {
		return err
	}

	fmt.Printf("流程: %s (%s)\n", status.Name, status.FlowID)
	fmt.Printf("状态: %s\n", status.Status)
	fmt.Printf("边数: %d\n\n", status.EdgeCount)

	fmt.Printf("%-20s %-10s %-8s %-8s\n", "节点", "状态", "PID", "角色")
	fmt.Println(strings.Repeat("-", 50))
	for _, n := range status.Nodes {
		pidStr := "-"
		if n.PID > 0 {
			pidStr = fmt.Sprintf("%d", n.PID)
		}
		fmt.Printf("%-20s %-10s %-8s %-8s\n", n.Name, n.Status, pidStr, n.Role)
	}
	return nil
}

func runLogs(cmd *cobra.Command, args []string) error {
	logs, err := eng.GetLogs(flowID, nodeName, 0)
	if err != nil {
		return err
	}

	for _, msg := range logs {
		direction := "→"
		if msg.Direction == "out" {
			direction = "←"
		}
		content := msg.RawContent
		if msg.Summary != "" {
			content = msg.Summary
		}
		fmt.Printf("[%s] %s %s\n", msg.NodeID, direction, content)
	}
	return nil
}

func runSend(cmd *cobra.Command, args []string) error {
	return eng.SendMessage(context.Background(), flowID, nodeName, message)
}

func runDelete(cmd *cobra.Command, args []string) error {
	if err := eng.DeleteFlow(flowID); err != nil {
		return err
	}

	fmt.Printf("流程 %s 已删除\n", flowID)
	return nil
}

// --- pingpong 命令处理 ---

func runPingPongRun(cmd *cobra.Command, args []string) error {
	keywords := parseList(ppKeywords)
	if len(keywords) == 0 {
		return fmt.Errorf("at least one keyword is required")
	}

	name := ppName
	if name == "" {
		name = fmt.Sprintf("patent-%s", strings.Join(keywords, "-"))
	}

	// 构建覆盖项
	var overrides *pingpong.PingPongOverrides
	if ppMaxRounds > 0 {
		overrides = &pingpong.PingPongOverrides{
			MaxRounds: ppMaxRounds,
		}
	}

	run, err := ppEng.CreateRun(name, keywords, overrides)
	if err != nil {
		return err
	}

	fmt.Printf("乒乓循环已创建\n  ID: %s\n  名称: %s\n  关键词: %s\n  最大轮次: %d\n",
		run.ID, run.Name, ppKeywords, run.MaxRounds)
	fmt.Println("\n开始运行...")

	if err := ppEng.Run(context.Background(), run.ID); err != nil {
		return err
	}

	// 运行结束后获取最终状态
	status, err := ppEng.GetRunStatus(run.ID)
	if err != nil {
		return err
	}

	fmt.Printf("\n\n========== 乒乓循环完成 ==========\n")
	fmt.Printf("运行 ID: %s\n", status.RunID)
	fmt.Printf("状态: %s\n", status.Status)
	fmt.Printf("总轮次: %d\n", status.CurrentRound)
	if status.FinalOutput != "" {
		fmt.Printf("\n--- 最终输出 ---\n%s\n", status.FinalOutput)
	}

	return nil
}

func runPingPongList(cmd *cobra.Command, args []string) error {
	runs, err := ppEng.ListRuns()
	if err != nil {
		return err
	}

	if len(runs) == 0 {
		fmt.Println("暂无乒乓运行记录")
		return nil
	}

	fmt.Printf("%-15s %-20s %-12s %-8s %-8s\n", "ID", "名称", "状态", "轮次", "最大轮次")
	fmt.Println(strings.Repeat("-", 65))
	for _, r := range runs {
		fmt.Printf("%-15s %-20s %-12s %-8d %-8d\n", r.ID, r.Name, r.Status, r.CurrentRound, r.MaxRounds)
	}
	return nil
}

func runPingPongStatus(cmd *cobra.Command, args []string) error {
	status, err := ppEng.GetRunStatus(ppID)
	if err != nil {
		return err
	}

	fmt.Printf("运行 ID: %s\n", status.RunID)
	fmt.Printf("名称: %s\n", status.Name)
	fmt.Printf("关键词: %s\n", strings.Join(status.Keywords, ", "))
	fmt.Printf("Agent A: %s\n", status.AgentAName)
	fmt.Printf("Agent B: %s\n", status.AgentBName)
	fmt.Printf("状态: %s\n", status.Status)
	fmt.Printf("当前轮次: %d / %d\n", status.CurrentRound, status.MaxRounds)
	if status.FinalOutput != "" {
		fmt.Printf("\n--- 最终输出 ---\n%s\n", status.FinalOutput)
	}
	return nil
}

func runPingPongStop(cmd *cobra.Command, args []string) error {
	if err := ppEng.StopRun(ppID); err != nil {
		return err
	}
	fmt.Printf("乒乓运行 %s 已停止\n", ppID)
	return nil
}

func runPingPongDelete(cmd *cobra.Command, args []string) error {
	if err := ppEng.DeleteRun(ppID); err != nil {
		return err
	}
	fmt.Printf("乒乓运行 %s 已删除\n", ppID)
	return nil
}

// parseList 解析逗号分隔列表
func parseList(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// parseEdges 解析边列表（如 "coder->reviewer,tester->coder"）
func parseEdges(s string) []db.Edge {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	var edges []db.Edge
	for _, p := range parts {
		p = strings.TrimSpace(p)
		kv := strings.SplitN(p, "->", 2)
		if len(kv) == 2 {
			edges = append(edges, db.Edge{From: strings.TrimSpace(kv[0]), To: strings.TrimSpace(kv[1])})
		}
	}
	return edges
}

func main() {
	initCommands()

	// 初始化日志
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
