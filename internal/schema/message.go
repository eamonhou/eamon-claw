package schema

import "encoding/json"

//
// 统一的消息与工具调用类型定义
//

// Role 定义消息的角色，这是与大模型沟通的基石
type Role string

type InputSchemaParams map[string]interface{}

const (
	RoleSystem    Role = "system"    // 系统提示词：确立 Agent 的性格与红线
	RoleUser      Role = "user"      // 用户输入 / 工具执行的返回结果 (Observation)
	RoleAssistant Role = "assistant" // 模型的输出：包含推理(Reasoning)或工具调用(ToolCall))
)

// Message 代表上下文中的单条消息
type Message struct {
	Role Role `json:"role"`

	// 存放纯文本内容
	Content string `json:"content"`

	// 如果模型决定调用工具，此字段将被填充 (支持并行调用多个工具)
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`

	// 如果这是对某个工具调用的响应，此字段必须填写，以告知模型上下文的关联性
	ToolCallId string `json:"tool_call_id,omitempty"`

	// 如果这是大模型 (Assistant) 的回复，此字段存放本次调用的 Token 消耗
	Usage *Usage `json:"usage,omitempty"`
}

// ToolCall 代表模型请求调用某个具体的工具
type ToolCall struct {
	// 工具调用id
	Id string `json:"id"`

	// 工具名称
	Name string `json:"name"`

	// Arguments 存放 JSON 参数。使用 RawMessage 是为了延迟解析，将解析责任交给具体的工具
	// 注意 使用 json.RawMessage 意味着 Main Loop 根本不关心具体的工具需要什么参数，实现了极致的解耦。
	Arguments json.RawMessage `json:"arguments"`
}

// ToolResult 本地调用工具执行完毕后返回的物理结果
type ToolResult struct {
	ToolCallId string `json:"tool_call_id"`

	// 工具执行的控制台输出或报错堆栈
	Output string `json:"output"`

	// 标记是否失败，供后续的驾驭工程进行错误自愈
	IsError bool `json:"is_error"`
}

// ToolDefinition 描述了一个大模型可以调用的工具元信息 (供模型理解工具有什么用)
type ToolDefinition struct {
	Name        string `json:"name"`
	Description string `json:"description"`

	// 对应 JSON Schema
	InputSchema interface{} `json:"input_schema"`
}

// Usage 记录了单次大模型 API 调用的 Token 消耗
type Usage struct {
	// 输入的 token 数量
	PromptTokenNum int64 `json:"prompt_token_num"`

	// 产生的 token 数量
	CompletionTokenNum int64 `json:"completion_token_num"`
}
