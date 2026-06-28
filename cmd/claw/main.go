package main

import (
	"context"
	"eamon-claw/internal/engine"
	"eamon-claw/internal/history"
	"eamon-claw/internal/observer"
	"eamon-claw/internal/provider"
	"eamon-claw/internal/schema"
	"eamon-claw/internal/tools"
	"flag"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// func main() {
// // LA-CPM Load-Aware Physical Cost Prediction Model
// // 环境负载感知的动态物理代价预测模型

// log.Println("🚀 AI Agent 启动：全架构闭环事件控制循环就绪...")

// // 1. 初始化只读分析库的连接池
// // 替换为你本地的测试数据库配置
// dsn := "root:rootisme@tcp(127.0.0.1:3302)/crm_data"
// db, err := sql.Open("mysql", dsn)
// if err != nil {
// 	log.Fatalf("❌ 无法建立数据库连接池: %v", err)
// }
// defer db.Close()

// // 2. 实例化 research 包下的三大硬核组件
// monitor := research.NewSlowLogMonitor("/Users/eamon/Programme/dk_container/mysql-agent/logs/mysql-slow.log")
// detector := research.NewLocalDetector()
// calculator := research.NewCostCalculator(db)

// // 3. 启动异步通道监听
// if err := monitor.StartWatch(); err != nil {
// 	log.Fatalf("❌ 慢日志监听器启动失败: %v", err)
// }

// // 4. 注册系统退出拦截信号
// sigChan := make(chan os.Signal, 1)
// signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

// log.Println("💡 消费者循环已挂起，开始流式消费 Channel 数据...")

// // 5. 【终极核心控制循环：Engine Loop】
// for {
// 	select {
// 	case payload := <-monitor.OutChannel:
// 		// 核心事件 A：通道收到慢查询数据

// 		// 第一步：瞬时捕获本地 OS 的 CPU / IO 负载因子
// 		factors := detector.Detect()

// 		// 第二步：将特征 payload 与环境因子送入计算器，联动读库算账
// 		_, _, _, _ = calculator.CalculateDynamicCost(payload, factors)

// 		// TODO: 第三步，根据 calculator 返回的 cost 结果，拼装高密 Prompt 扔给大模型大脑！

// 	case <-sigChan:
// 		// 核心事件 B：接收到系统退出中断
// 		log.Println("👋 接收到安全退出信号，Agent 引擎平滑关闭。")
// 		return
// 	}
// }
// }

func main() {
	promptPtr := flag.String("prompt", "", "要交给 Agent 执行的任务描述")
	flag.Parse()
	if *promptPtr == "" {
		fmt.Println("用法: go run cmd/claw/main.go -prompt \"你的任务指令\"")
		os.Exit(1)
	}

	workDir, _ := os.Getwd()

	// 测试 subagent
	// workDir += "/workspace"

	modelName := "gemini-3-pro-preview"

	sessionA := history.GlobalSessionManager.GetOrCreate("chat_01", workDir)

	sessionA.Append(schema.Message{
		Role:    schema.RoleUser,
		Content: *promptPtr,
	})

	modelProvider := provider.NewGeminiOpenAIProvider(modelName)
	trackerProvider := observer.NewCostTracker(modelProvider, modelName, sessionA)

	reporter := engine.NewTerminalReporter()

	// sub agent 工具注册
	// sub agent 只能使用只读的工具
	readOnlyRegistry := tools.NewRegistry()
	readOnlyRegistry.Register(tools.NewReadFileTool(workDir))
	// 只能执行 grep 等搜索命令
	readOnlyRegistry.Register(tools.NewBashTool(workDir))

	// main agent 工具注册
	// 工具注册
	mainToolRegistry := tools.NewRegistry()

	// 注册读取文件的工具
	mainToolRegistry.Register(tools.NewReadFileTool(workDir))

	// 注册写文件的工具
	mainToolRegistry.Register(tools.NewWriteFileTool(workDir))

	// 注册执行bash的工具
	mainToolRegistry.Register(tools.NewBashTool(workDir))

	// 注册执行lacpm-tool的工具
	mainToolRegistry.Register(tools.NewLacpmTool(workDir))

	// 挂载中间件
	mainToolRegistry.Use(func(ctx context.Context, call schema.ToolCall) (allowed bool, rejectReason string) {
		// todo 审核命令
		return true, ""
	})

	// 运行程序
	eng := engine.NewAgentEngine(trackerProvider, mainToolRegistry, false)

	// 将带有 Engine 引用和只读 Registry 的 Subagent 工具注册进主线
	mainToolRegistry.Register(tools.NewSubagentTool(eng, readOnlyRegistry, reporter))

	// // 初始化飞书调度器
	// bot := feishu.NewFeishu(eng)
	// handler := httpserverext.NewEventHandlerFunc(bot.GetEventDispatcher())
	// // 注册路由并启动 HTTP 服务
	// http.HandleFunc("/webhook/event", handler)
	// port := ":48080"
	// log.Printf("🚀 go-tiny-claw 飞书服务端已启动，正在监听 %s 端口\n", port)
	// err := http.ListenAndServe(port, nil)
	// if err != nil {
	// 	log.Fatalf("服务器启动失败: %v", err)
	// }

	terminalReporter := engine.NewTerminalReporter()

	// 计划模式开关
	planMode := false

	ctx := context.Background()

	if err := eng.Run(ctx, sessionA, terminalReporter, planMode); err != nil {
		log.Fatalf("引擎崩溃：%v\n", err)
	}

	log.Printf("\n================ 财务报表 ================\n")
	log.Printf("会话 ID: %s\n", sessionA.Id)
	log.Printf("总消耗 Input Tokens: %d\n", sessionA.TotalPromptTokenNum)
	log.Printf("总消耗 Output Tokens: %d\n", sessionA.TotalCompletionTokenNum)
	log.Printf("总计费用 (CNY): ¥%.6f\n", sessionA.TotalCost)
	log.Printf("==========================================\n")
}

func testSession() {
	workDir, _ := os.Getwd()

	modelProvider := provider.NewGeminiOpenAIProvider("gemini-2.5-flash")

	// 工具注册
	toolRegistry := tools.NewRegistry()

	// 注册读取文件的工具
	readFileTool := tools.NewReadFileTool(workDir)
	toolRegistry.Register(readFileTool)

	// 注册写文件的工具
	writeFileTool := tools.NewWriteFileTool(workDir)
	toolRegistry.Register(writeFileTool)

	// 注册执行bash的工具
	bashTool := tools.NewBashTool(workDir)
	toolRegistry.Register(bashTool)

	eng := engine.NewAgentEngine(modelProvider, toolRegistry, false)

	terminalReporter := engine.NewTerminalReporter()

	wg := &sync.WaitGroup{}

	wg.Add(1)
	go func() {
		defer wg.Done()

		sessionA := history.GlobalSessionManager.GetOrCreate("chat_01", workDir)

		log.Println("\n>>> 🙋‍♂️ [Session A / Turn 1]: 帮我看看 test.md 里记录了什么密钥？")
		sessionA.Append(schema.Message{
			Role:    schema.RoleUser,
			Content: "帮我看看 test.md 里记录了什么密钥？",
		})

		for i := 0; i < 6; i++ {
			sessionA.Append(schema.Message{
				Role:    schema.RoleUser,
				Content: "这只是一句闲聊占位符。",
			})
			sessionA.Append(schema.Message{
				Role:    schema.RoleAssistant,
				Content: "好的，收到闲聊消息。",
			})
		}

		// 回合 2：验证记忆截断 (此时第一轮的密钥已经被挤出 Working Memory 了！)
		log.Println("\n>>> 🙋‍♂️ [Session A / Turn 2]: 请直接告诉我，刚才第一轮你查到的那个密钥是什么？")
		sessionA.Append(schema.Message{Role: schema.RoleUser, Content: "请直接告诉我，刚才第一轮你查到的那个密钥是什么？不准调用工具！"})

		if err := eng.Run(context.Background(), sessionA, terminalReporter, false); err != nil {
			log.Fatalf("引擎崩溃：%v\n", err)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()

		time.Sleep(time.Second * 3)

		sessionB := history.GlobalSessionManager.GetOrCreate("chat_02", workDir)

		log.Println("\n>>> 🙋‍♂️ [Session B]: 别人查到了一个密钥，你这里能看到吗？")
		sessionB.Append(schema.Message{
			Role:    schema.RoleUser,
			Content: "你这里能看到别的会话查询到的密钥吗？不准调用工具！",
		})

		if err := eng.Run(context.Background(), sessionB, terminalReporter, false); err != nil {
			log.Fatalf("引擎崩溃：%v\n", err)
		}
	}()

	wg.Wait()
}
