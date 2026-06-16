package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"eamon-claw/internal/observer"
	"eamon-claw/internal/schema"
)

type MiddlewareFunc func(ctx context.Context, call schema.ToolCall) (allowed bool, rejectReason string)

// Registry 定义了工具的注册与分发执行接口
type Registry interface {
	// GetAvailableTools 返回当前系统挂载的所有可用工具的 Schema
	GetAvaliableTools() []schema.ToolDefinition

	// Execute 实际执行模型请求的工具，并返回结果
	Execute(ctx context.Context, call schema.ToolCall) schema.ToolResult

	// Register 挂载工具
	Register(tool BaseTool)

	// 中间件挂载
	Use(mw MiddlewareFunc)
}

type BaseTool interface {
	// Name 返回工具的全局唯一名称
	Name() string

	// Definition 返回用于提交给大模型的工具元信息和参数 json schema
	Definition() schema.ToolDefinition

	// Execute 接受大模型吐出的 json 参数，执行具体业务逻辑
	// 注意：参数是 json.RawMessage，反序列化由各个工具内部自行处理
	Execute(ctx context.Context, args json.RawMessage) (string, error)
}

type RegistryImpl struct {
	// map[tool_name]tool
	tools map[string]BaseTool

	middlewares []MiddlewareFunc
}

func NewRegistry() Registry {
	return &RegistryImpl{
		tools:       map[string]BaseTool{},
		middlewares: make([]MiddlewareFunc, 0),
	}
}

// 注册工具，就是将 tool 挂载到 RegistryImpl 的tools下
func (r *RegistryImpl) Register(tool BaseTool) {
	name := tool.Name()
	if _, ok := r.tools[name]; ok {
		log.Printf("[Warning] 工具 %s 已经被注册，将被覆盖\n", name)
	}
	r.tools[name] = tool
	log.Printf("[Registery] 工具注册成功：%s\n", name)
}

func (r *RegistryImpl) GetAvaliableTools() []schema.ToolDefinition {
	var toolDefs []schema.ToolDefinition
	for _, t := range r.tools {
		toolDefs = append(toolDefs, t.Definition())
	}
	return toolDefs
}

func (r *RegistryImpl) Use(mw MiddlewareFunc) {
	r.middlewares = append(r.middlewares, mw)
}

func (r *RegistryImpl) Execute(ctx context.Context, tc schema.ToolCall) schema.ToolResult {
	// 链路追踪
	ctx, span := observer.StartSpan(ctx, "Tool.Execute")
	span.AddAttribute("tool_name", tc.Name)
	span.AddAttribute("args", string(tc.Arguments))
	defer span.EndSpan()

	tool, ok := r.tools[tc.Name]
	if !ok {
		// 工具不存在
		errMsg := fmt.Sprintf("工具 %s 不存在", tc.Name)
		return schema.ToolResult{
			ToolCallId: tc.Id,
			Output:     errMsg,
			IsError:    true,
		}
	}

	// 在运行指令前先一次运行所有的 middleware
	for _, mw := range r.middlewares {
		allowed, reason := mw(ctx, tc)
		if !allowed {
			log.Printf("[Registry] ⚠️工具 %s 被 Middleware 拦截：%s\n", tc.Name, reason)
			return schema.ToolResult{
				ToolCallId: tc.Id,
				Output:     fmt.Sprintf("执行被系统拦截，原因：%s", reason),
				IsError:    true, //必须返回 Error，强制大模型阅读拒绝理由
			}
		}
	}

	// 执行命令
	result, err := tool.Execute(ctx, tc.Arguments)

	if err != nil {
		errMsg := fmt.Sprintf("执行工具 %s 发生错误：%v", tc.Name, err)
		return schema.ToolResult{
			ToolCallId: tc.Id,
			Output:     errMsg,
			IsError:    true,
		}
	}

	// 我们甚至可以只截取输出的前 100 字符放入 Trace，防止 Trace 文件过度膨胀
	span.AddAttribute("output_preview", truncate(result, 100))

	return schema.ToolResult{
		ToolCallId: tc.Id,
		Output:     result,
		IsError:    false,
	}
}

func truncate(s string, max int) string {
	if len(s) > max {
		return s[:max] + "..."
	}
	return s
}
