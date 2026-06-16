package engine

import (
	"context"
	"fmt"
	"strings"
)

// 专用于本地终端测试的 Reporter 实现

type TerminalReporter struct {
}

func NewTerminalReporter() *TerminalReporter {
	return &TerminalReporter{}
}

// OnThinking 当模型开始进行慢思考（Reasoning）时调用
func (r *TerminalReporter) OnThinking(ctx context.Context) {
	fmt.Printf("\n🤔模型正在思考中...\n")
}

// OnToolCall 当模型决定并发调用工具时调用
func (r *TerminalReporter) OnToolCall(ctx context.Context, toolName string, args string) {
	fmt.Printf("🔧调用工具：%s\n", toolName)
	displayArgs := strings.ReplaceAll(args, "\n", "\\n")
	displayArgs = strings.ReplaceAll(displayArgs, "\r", "\\r")
	if len(displayArgs) > 150 {
		displayArgs = displayArgs[:150] + "...（已截断）"
	}
	fmt.Printf("参数：%s\n", displayArgs)
}

// OnToolResult 当工具在底层执行完毕并返回结果时调用
func (r *TerminalReporter) OnToolResult(ctx context.Context, toolName string, result string, isError bool) {
	if isError {
		fmt.Printf("❌工具执行失败：%s，错误信息：%s\n", toolName, result)
	} else {
		fmt.Printf("✅工具执行成功：%s\n", toolName)
	}
}

// OnMessage 当模型宣告任务完成，向用户输出最终纯文本回答时调用
func (r *TerminalReporter) OnMessage(ctx context.Context, content string) {
	fmt.Printf("\n 模型回复：\n%s\n\n", content)
}
