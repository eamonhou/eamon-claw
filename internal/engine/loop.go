package engine

import (
	"context"
	"fmt"
	"log"
	"sync"

	"eamon-claw/internal/history"
	"eamon-claw/internal/observer"
	"eamon-claw/internal/provider"
	"eamon-claw/internal/schema"
	"eamon-claw/internal/tools"
)

const (
	maxTurnCountMainAgent int = 20
	maxTurnCountSubAgent  int = 15
)

type AgentEngine struct {
	provider provider.LLMProvider
	registry tools.Registry

	// 慢思考模式开关
	EnableThinking bool

	// 引擎持有的组装器
	// composer *history.PromptComposer

	// 上下文压缩器
	compactor *history.Compactor

	// 自愈管理器
	recovery *history.RecoveryManager

	// 死循环提醒器
	reminder *ReminderInject
}

func NewAgentEngine(p provider.LLMProvider, r tools.Registry, enableThinking bool) *AgentEngine {
	return &AgentEngine{
		provider:       p,
		registry:       r,
		EnableThinking: enableThinking,

		// 【初始化压缩器】：为了便于今天的极端测试，我们将水位线阈值设积极（例如 3000 字符），
		// 并保护最近的 6 条消息（大约两轮 Turn 的交互）
		compactor: history.NewCompactor(20000, 30),

		recovery: history.NewRecoveryManager(),

		reminder: NewReminderInject(),
	}
}

func (e *AgentEngine) Run(ctx context.Context, session *history.Session, reporter Reporter, planMode bool) error {
	log.Printf("[Engine] 引擎启动，锁定工作区：%s\n", session.WorkDir)
	log.Printf("[Engine] 慢思考模式（Thniking phase）：%v\n", e.EnableThinking)

	// 开启 Root Span，记录整个任务的生命周期
	ctx, rootSpan := observer.StartSpan(ctx, "Agent.Run")
	rootSpan.AddAttribute("session_id", session.Id)
	rootSpan.AddAttribute("work_dir", session.WorkDir)

	// defer 保证在引擎退出时，无论成功失败，都能结束根 Span 并导出 Trace 报告
	defer func() {
		rootSpan.EndSpan()
		_ = observer.ExportTraceToFile(rootSpan, session.WorkDir, session.Id)
		log.Printf("📊 [Tracing] 本次任务的执行回放链路已保存至工作区的 .claw/traces 目录下\n")
	}()

	// 根据当前 Session 的工作区，动态组装最新的 System Prompt
	composer := history.NewPromptComposer(session.WorkDir, planMode)
	systemMessage := composer.Build()

	turnCount := 0

	for {
		turnCount++

		// 记录单次 turn 的 span
		turnCtx, turnSpan := observer.StartSpan(ctx, fmt.Sprintf("Turn_%d", turnCount))
		defer turnSpan.EndSpan() // 利用 defer，哪怕遇到了 break 或 error 也会计算耗时

		// 上下文组装 System Prompt + 截取最近的 6 条消息作为 Working Memory
		// 在实际业务中，由于工具返回结果可能很长，短期工作记忆往往设为 6-10 条足以维系连贯对话
		workingMemory := session.GetWorkingMemory(20)

		// 当前对话的上下文窗口
		history := make([]schema.Message, 0)
		history = append(history, systemMessage)
		history = append(history, workingMemory...)

		// 核心注入点：在向 Provider 发起推理前，过一遍内存压缩器！
		// 无论你带出了多少上下文，如果字符总数超标，早期日志将被掩码化，超大日志将被掐头去尾
		history = e.compactor.Compact(history)

		// ================= Phase 1: Thinking =================

		if e.EnableThinking {

			// 记录 Thinking 调用
			thinkCtx, thinkSpan := observer.StartSpan(turnCtx, "LLM.Thinking")
			thinkResp, err := e.provider.Generate(thinkCtx, history, nil)
			thinkSpan.EndSpan()

			// 慢思考阶段：剥夺工具，强制规划
			// 模型如果拿到工具注册信息很大概率会放弃思考规划，直接执行工具
			// 所以在慢思考阶段不给模型工具注册信息，强迫让其思考规划

			if reporter != nil {
				// 触发 Reporter：开始慢慢思考
				reporter.OnThinking(ctx)
			}

			thinkResp, err = e.provider.Generate(ctx, history, nil)
			if err != nil {
				return fmt.Errorf("Thinking 生成失败：%w", err)
			}
			if len(thinkResp.Content) > 0 {
				// 将思考过程持久化到 Session 中
				session.Append(*thinkResp)
				// 加入到本轮上下文中
				history = append(history, *thinkResp)
			}
		}

		// ================= Phase 2: Action =================

		// 获取当前挂载的所有工具
		avaliableTools := e.registry.GetAvaliableTools()

		// 记录 Action 调用
		actCtx, actSpan := observer.StartSpan(turnCtx, "LLM.Action")
		actionResp, err := e.provider.Generate(actCtx, history, avaliableTools)
		actSpan.EndSpan() // 结束行动跨度

		// 向大模型发起推理请求
		// history 包含了模型慢思考与规划的上下文
		// 模型会顺着自己的逻辑，结合恢复的 availableTools 发起精准的工具调用
		actionResp, err = e.provider.Generate(ctx, history, avaliableTools)
		if err != nil {
			return fmt.Errorf("Action 生成失败：%w", err)
		}

		// 将大模型的行动响应持久化到 Session 中并放入本轮上下文中
		session.Append(*actionResp)
		history = append(history, *actionResp)

		// 如果模型回复了纯文本，打印出来 (这通常是它的思考过程，或是最终结果)
		if len(actionResp.Content) > 0 && reporter != nil {
			reporter.OnMessage(ctx, actionResp.Content)
		}

		// 退出条件判断
		// 如果模型没有请求任何工具调用，说明它认为任务已经完成，跳出循环。
		if len(actionResp.ToolCalls) == 0 {
			break
		}

		// // 执行行动 (Action) 与 获取观察结果 (Observation)
		// log.Printf("[Engine] 模型请求调用 %d 个工具...\n", len(actionResp.ToolCalls))

		for _, toolCall := range actionResp.ToolCalls {
			if reporter != nil {
				reporter.OnToolCall(ctx, toolCall.Name, string(toolCall.Arguments))
			}

			result := e.registry.Execute(ctx, toolCall)

			// 自愈
			toolExecuteResult := result.Output
			if result.IsError {
				toolExecuteResult = e.recovery.AnalyzeAndInject(toolCall.Name, result.Output)
				log.Printf("❌ 执行工具 %s 发生错误并注入救援指南: %s\n", toolCall.Name, toolExecuteResult)
			}

			if reporter != nil {
				displayOutput := toolExecuteResult
				if len(displayOutput) > 200 {
					displayOutput = displayOutput[:200] + "...（已截断）"
				}
				reporter.OnToolResult(ctx, toolCall.Name, displayOutput, result.IsError)
			}

			// 将工具执行的观察结果 (Observation) 封装为 User Message 追加到上下文中
			// 注意：ToolCallID 必须携带！这是维系大模型推理链条的关键
			obserResult := schema.Message{
				Role:       schema.RoleUser,
				Content:    result.Output,
				ToolCallId: toolCall.Id,
			}
			session.Append(obserResult)
			history = append(history, obserResult)

			reminderMessage := e.reminder.CheckAndInject(toolCall, result)
			if reminderMessage != nil {
				log.Printf("[Engine] 发生了死循环并注入提醒：%s %s\n", reminderMessage.Role, reminderMessage.Content)
				// 如果触发了干预规则，将这条严厉的提醒作为 User 消息，强制追加到 Session 的最末尾！
				// 大模型在下一轮被唤醒时，第一眼就会看到这句话，从而打破局部执念。
				session.Append(*reminderMessage)
			}

		}
		// fmt.Printf("%v", history)
		// 循环回到开头，模型将带着新加入的 Observation 继续它的下一轮思考...

		// 结束本轮 Turn 的 Span
		turnSpan.EndSpan()
	}

	return nil
}

func (e *AgentEngine) RunSub(ctx context.Context, taskPrompt string, readOnlyRegistry tools.Registry, reporter interface{}) (string, error) {
	history := []schema.Message{
		{
			Role: schema.RoleSystem,
			Content: `你是一个专门负责深度探索的探路者
你的任务是根据主架构师的指令，在当前工作区内仔细阅读代码、查阅日志，搜集足够的信息。

【核心纪律】
1. 你必须、且只能依靠内置工具（如 bash 的 find/grep，或 read_file）去寻找答案。绝对不允许凭空捏造或猜测！
2. 如果你没有找到确切的答案，你必须继续使用工具深入搜索。
3. 当且仅当你找到了确切的线索后，停止调用工具，直接输出一段纯文本作为你的终极汇报。主架构师会根据你的汇报来做下一步决策。`,
		}, {
			Role:    schema.RoleUser,
			Content: taskPrompt,
		},
	}

	// 限制 subagent 数量上限
	const maxSubagent int = 10
	turnCount := 0

	for {
		turnCount++
		if turnCount > maxTurnCountSubAgent {
			return "", fmt.Errorf("子 Agent 探索过深，超过 %d 轮被强制召回，请主 Agent 给它更明确的指令", maxTurnCountSubAgent)
		}

		// 注册可用工具
		avaliableTools := readOnlyRegistry.GetAvaliableTools()

		compactedContext := e.compactor.Compact(history)

		// 子任务要求快速响应，关闭慢思考，直接预测 action
		actionResp, err := e.provider.Generate(ctx, compactedContext, avaliableTools)
		if err != nil {
			return "", fmt.Errorf("子 agent 思考失败：%w", err)
		}

		compactedContext = append(compactedContext, *actionResp)

		// 子智能体一旦不调用工具了，说明它做好了总结汇报
		if len(actionResp.ToolCalls) == 0 {
			// 直接将它的这段汇报内容剥离出来返回给上层
			return actionResp.Content, nil
		}

		// 执行只读工具的并发循环
		observationMessages := make([]schema.Message, len(actionResp.ToolCalls))

		wg := &sync.WaitGroup{}

		for i, tc := range actionResp.ToolCalls {
			wg.Add(1)
			go func(idx int, toolcall schema.ToolCall) {
				defer wg.Done()

				var subReporter Reporter
				if reporter != nil {
					subReporter = reporter.(Reporter)
					subReporter.OnToolCall(ctx, fmt.Sprintf("[Subagent] %s", toolcall.Name), string(tc.Arguments))
				}

				result := e.registry.Execute(ctx, toolcall)

				finalOutput := result.Output
				if result.IsError {
					finalOutput = e.recovery.AnalyzeAndInject(toolcall.Name, result.Output)
				}

				if subReporter != nil {
					displayOutput := finalOutput
					if len(displayOutput) > 200 {
						displayOutput = fmt.Sprintf("%s...（已截断）", displayOutput[:200])
					}
					subReporter.OnToolResult(ctx, toolcall.Name, displayOutput, result.IsError)
				}

				observationMessages[idx] = schema.Message{
					Role:       schema.RoleUser,
					Content:    finalOutput,
					ToolCallId: toolcall.Id,
				}
			}(i, tc)
		}
	}

	return "", nil
}
