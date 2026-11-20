package engine

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"log"

	"eamon-claw/internal/schema"
)

// 死循环探测与动态提醒生成器
// 我们需要在这里维护一个滑动窗口（Sliding Window）或哈希计数器，来监控最近几次的工具调用情况。

// ReminderInject 负责在运行时监控上下文，并在模型陷入执念时动态注入强力打断信息
type ReminderInject struct {
	// 用于记录连续失败的工具调用次数 map[hash(toolName+args)]num
	consecutiveFailureNum map[string]int
}

func NewReminderInject() *ReminderInject {
	return &ReminderInject{
		consecutiveFailureNum: make(map[string]int),
	}
}

// CheckAndInject 检查工具调用结果并注入提醒
func (r *ReminderInject) CheckAndInject(lastToolCall schema.ToolCall, lastResult schema.ToolResult) *schema.Message {
	toolCallHashCode := getToolCallKey(lastToolCall.Name, string(lastToolCall.Arguments))

	// 如果工具调用结果没有错误，说明这条路可以走通，清空失败计数器
	if !lastResult.IsError {
		r.consecutiveFailureNum = make(map[string]int)
		return nil
	}

	// 如果执行失败，累加该特征的失败次数
	r.consecutiveFailureNum[toolCallHashCode]++
	failureNum := r.consecutiveFailureNum[toolCallHashCode]

	log.Printf("[Reminder] 监控到工具 %s 执行失败，连续失败次数：%d\n", lastToolCall.Name, failureNum)

	// 触发死循环打断机制
	if failureNum >= 3 {
		log.Printf("[Reminder] ⚠️触发死循环干预！注入强力修改指令！")

		// 构造一条极其严厉的行动指南
		remindMessage := fmt.Sprintf(`
你尝试调用工具 %s 的连续失败次数达到 %d 次，停止这种无效的尝试，你的注意力被当前的报错过度吸引了。
你接下来需要：
1. 停止猜测参数，跳出局部思维；
2. 或者结束当前的任务并说明你需要哪些具体的信息和帮助。
		`, lastToolCall.Name, failureNum)

		return &schema.Message{
			// 必须是 RoleUser，以保证在下一次 API 请求时拥有最高的近因效应权重
			Role:    schema.RoleUser,
			Content: remindMessage,
		}
	}
	return nil
}

// getToolCallKey 获取工具调用的key
func getToolCallKey(toolName string, args string) string {
	hasher := md5.New()
	hasher.Write([]byte(toolName))
	hasher.Write([]byte(args))
	return hex.EncodeToString(hasher.Sum(nil))
}
