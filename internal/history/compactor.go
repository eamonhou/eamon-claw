package history

import (
	"fmt"
	"log"

	"eamon-claw/internal/schema"
)

type Compactor struct {
	// 触发压缩的最大字符数阈值，水位线，可参考使用的大模型的token窗口大小
	MaxCharNum int

	// Working Memory 保护区：最近的 N 条消息
	RetainLastMsgNum int
}

func NewCompactor(maxCharNum int, retainLastMsgNum int) *Compactor {
	return &Compactor{
		MaxCharNum:       maxCharNum,
		RetainLastMsgNum: retainLastMsgNum,
	}
}

// Compact 接收要发送给大模型的消息数组
// 如果总长度超标，对远期历史区进行全量掩码（Masking），对短期保护区进行超长局部截断（Truncation）
func (c *Compactor) Compact(messages []schema.Message) []schema.Message {
	curHistoryCharNum := c.estimateCharNum(messages)

	if curHistoryCharNum < c.MaxCharNum {
		// 总长度没有超过水位线，直接返回原始数组
		return messages
	}

	log.Printf("[Compactor] ⚠️ 内存告警：当前上下文长度（%d个字符）超过阈值（%d），触发压缩策略...\n", curHistoryCharNum, c.MaxCharNum)

	var compactMessages []schema.Message
	messageCount := len(messages)

	// 计算受保护的起始索引
	protectedMsgStartIdx := messageCount - c.RetainLastMsgNum
	if protectedMsgStartIdx < 0 {
		protectedMsgStartIdx = 0
	}

	for idx, message := range messages {
		// 系统提示词不动
		if message.Role == schema.RoleSystem {
			compactMessages = append(compactMessages, message)
			continue
		}

		// 必须拷贝一份新消息，因为在并发环境中直接修改原引用可能导致底层数据结构被污染
		newMessage := message

		isInWorkingMemory := (idx >= protectedMsgStartIdx)

		// 核心驾驭逻辑: 双重降级防线
		if message.Role == schema.RoleUser && message.ToolCallId != "" { // 处理工具的执行结果
			if !isInWorkingMemory {
				// 第一道防线：远期历史。直接省略。
				if len(message.Content) > 200 {
					newMessage.Content = fmt.Sprintf("...（为了节省内存，较早历史的工具输出已经被系统强制清理。原始长度：%d 字节）...", len(message.Content))
				}
			} else {
				// 第二道防线：短期记忆。
				// 即使处于近期保护区，只要单条内容过大，只要单条内容过大，也必须强制 OOM (Head-Tail Truncation)
				// 保留前 500 字符和后 500 字符（掐头去尾法，大模型通常只需要看开头报错和结尾总结）
				const maxKeepNum int = 1000
				size := len(message.Content)
				if size > maxKeepNum {
					head, tail := message.Content[:500], message.Content[len(message.Content)-500:]
					newMessage.Content = fmt.Sprintf("%s...（内容过长，中间 %d 个字符已省略）...%s", head, len(message.Content)-maxKeepNum, tail)
				}
			}
		} else if message.Role == schema.RoleAssistant && message.Content != "" {
			// 对于大模型的冗长推理废话 (Thinking Trace)
			if !isInWorkingMemory && len(message.Content) > 200 {
				newMessage.Content = "...（早期推理思考过程已折叠）..."
			}
		}
		// 注意：我们绝不会去动 msg.ToolCalls，因为这是模型行动的证据，是维系逻辑链的关键！
		compactMessages = append(compactMessages, newMessage)
	}

	compactHistoryNum := c.estimateCharNum(compactMessages)
	log.Printf("[Compactor] ✅ 压缩完成。上下文长度从 %d 降至 %d 字符。\n", curHistoryCharNum, compactHistoryNum)

	return compactMessages
}

// estimateCharNum 粗略计算当前上下文的总字符长度
func (c *Compactor) estimateCharNum(messages []schema.Message) int {
	length := 0
	for _, message := range messages {
		length += len(message.Content)
		for _, tc := range message.ToolCalls {
			length += len(tc.Name) + len(tc.Arguments)
		}
	}
	return length
}
