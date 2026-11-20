package main

import (
	"context"
	"eamon-claw/internal/config"
	"eamon-claw/internal/engine"
	"eamon-claw/internal/feishu"
	"eamon-claw/internal/history"
	"eamon-claw/internal/observer"
	"eamon-claw/internal/provider"
	"eamon-claw/internal/schema"
	"eamon-claw/internal/tools"
	"fmt"
	"log"
	"os"
	"time"
)

func main() {
	workDir, _ := os.Getwd()

	cfg := config.Load(workDir)
	if err := cfg.Validate(); err != nil {
		fmt.Println(err)
		fmt.Println("\n用法: go run cmd/claw/main.go -prompt \"你的任务指令\"")
		fmt.Println("可选参数:")
		fmt.Println("  -model string    模型名称（默认 gemini-3-pro-preview）")
		fmt.Println("  -session string  会话 ID（默认 chat_01）")
		fmt.Println("  -plan            开启计划模式")
		fmt.Println("  -thinking        开启慢思考")
		os.Exit(1)
	}

	sessionA := history.GlobalSessionManager.GetOrCreate(cfg.SessionID, workDir)

	sessionA.Append(schema.Message{
		Role:    schema.RoleUser,
		Content: cfg.Prompt,
	})

	modelProvider := provider.NewGeminiOpenAIProvider(cfg.ModelName, cfg.APIKey, cfg.APIBaseURL)
	trackerProvider := observer.NewCostTracker(modelProvider, cfg.ModelName, sessionA)

	terminalReporter := engine.NewTerminalReporter()

	// sub agent 只能使用只读的工具
	readOnlyRegistry := tools.NewRegistry()
	readOnlyRegistry.Register(tools.NewReadFileTool(workDir, cfg.ReadFileMaxLen))
	readOnlyRegistry.Register(tools.NewBashTool(workDir, cfg.BashMaxLen, cfg.BashTimeoutSec))

	// main agent 工具注册
	mainToolRegistry := tools.NewRegistry()
	mainToolRegistry.Register(tools.NewReadFileTool(workDir, cfg.ReadFileMaxLen))
	mainToolRegistry.Register(tools.NewWriteFileTool(workDir))
	mainToolRegistry.Register(tools.NewEditFileTool(workDir))
	mainToolRegistry.Register(tools.NewBashTool(workDir, cfg.BashMaxLen, cfg.BashTimeoutSec))

	// 挂载高危命令拦截中间件
	mainToolRegistry.Use(func(ctx context.Context, call schema.ToolCall) (allowed bool, rejectReason string) {
		if feishu.IsDangerousCommand(call.Name, string(call.Arguments)) {
			allowed, reason := feishu.GlobalApprovalManager.WaitForApproval(
				ctx,
				fmt.Sprintf("%s-%d", call.Name, time.Now().UnixNano()),
				call.Name,
				string(call.Arguments),
				nil,
			)
			if !allowed {
				return false, reason
			}
		}
		return true, ""
	})

	eng := engine.NewAgentEngine(trackerProvider, mainToolRegistry, cfg.EnableThinking, cfg.MaxTurnMain, cfg.CompactMaxChar, cfg.CompactRetainLast)

	mainToolRegistry.Register(tools.NewSubagentTool(eng, readOnlyRegistry, terminalReporter))

	// // 初始化飞书调度器（启用时取消注释并补充 net/http 导入）
	// bot := feishu.NewFeishu(eng, workDir)
	// handler := httpserverext.NewEventHandlerFunc(bot.GetEventDispatcher())
	// http.HandleFunc("/webhook/event", handler)
	// log.Printf("🚀 go-tiny-claw 飞书服务端已启动，正在监听 %s 端口\n", cfg.FeishuPort)
	// if err := http.ListenAndServe(cfg.FeishuPort, nil); err != nil {
	// 	log.Fatalf("服务器启动失败: %v", err)
	// }

	ctx := context.Background()

	if err := eng.Run(ctx, sessionA, terminalReporter, cfg.PlanMode); err != nil {
		log.Fatalf("引擎崩溃：%v\n", err)
	}

	log.Printf("\n================ 财务报表 ================\n")
	log.Printf("会话 ID: %s\n", sessionA.Id)
	log.Printf("总消耗 Input Tokens: %d\n", sessionA.TotalPromptTokenNum)
	log.Printf("总消耗 Output Tokens: %d\n", sessionA.TotalCompletionTokenNum)
	log.Printf("总计费用 (CNY): ¥%.6f\n", sessionA.TotalCost)
	log.Printf("==========================================\n")
}
