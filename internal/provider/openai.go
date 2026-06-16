package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"eamon-claw/internal/schema"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"
)

type OpenAIProvider struct {
	model  string
	client openai.Client
}

func NewGeminiOpenAIProvider(model string) *OpenAIProvider {
	apiKey := os.Getenv("VECTAI_API_KEY")
	baseUrl := "https://api.vectorengine.ai/v1"
	return &OpenAIProvider{
		model:  model,
		client: openai.NewClient(option.WithAPIKey(apiKey), option.WithBaseURL(baseUrl)),
	}
}

// 此方法的核心功能就是将 上下文的 schema.Message 转成 openai 的 请求Message
// 再将 openai 的 响应message 转换成 schema.Message
func (p *OpenAIProvider) Generate(ctx context.Context, messages []schema.Message, avaliableTools []schema.ToolDefinition) (*schema.Message, error) {
	var openaiMessages []openai.ChatCompletionMessageParamUnion
	for _, message := range messages {
		switch message.Role {
		case schema.RoleSystem:
			openaiMessages = append(openaiMessages, openai.SystemMessage(message.Content))

		case schema.RoleUser:
			if len(message.ToolCallId) > 0 {
				openaiMessages = append(openaiMessages, openai.ToolMessage(message.Content, message.ToolCallId))
			} else {
				openaiMessages = append(openaiMessages, openai.UserMessage(message.Content))
			}

		case schema.RoleAssistant:
			assistMessageParam := openai.ChatCompletionAssistantMessageParam{}

			if len(message.Content) > 0 {
				assistMessageParam.Content = openai.ChatCompletionAssistantMessageParamContentUnion{
					OfString: openai.String(message.Content),
				}
			}

			if len(message.ToolCalls) > 0 {
				var toolCalls []openai.ChatCompletionMessageToolCallUnionParam
				// 【重要】如果历史包含 ToolCalls，必须原样放回，以维系大模型的逻辑链
				for _, tc := range message.ToolCalls {
					toolCalls = append(toolCalls, openai.ChatCompletionMessageToolCallUnionParam{
						OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
							ID:   tc.Id,
							Type: "function",
							Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{
								Name:      tc.Name,
								Arguments: string(tc.Arguments),
							},
						},
					})
				}
				assistMessageParam.ToolCalls = toolCalls
			}
			openaiMessages = append(openaiMessages, openai.ChatCompletionMessageParamUnion{
				OfAssistant: &assistMessageParam,
			})
		}
	}

	var openaiTools []openai.ChatCompletionToolUnionParam

	for _, toolDef := range avaliableTools {
		var params shared.FunctionParameters
		if m, ok := toolDef.InputSchema.(map[string]interface{}); ok {
			params = shared.FunctionParameters(m)
		} else {
			b, err := json.Marshal(toolDef.InputSchema)
			if err != nil {
				return nil, fmt.Errorf("工具数据定义格式错误，无法格式化%w", err)
			}
			if err := json.Unmarshal(b, &params); err != nil {
				return nil, fmt.Errorf("工具数据定义格式错误，解析失败%w", err)
			}
		}
		openaiTools = append(openaiTools, openai.ChatCompletionFunctionTool(
			shared.FunctionDefinitionParam{
				Name:        toolDef.Name,
				Description: openai.String(toolDef.Description),
				Parameters:  params,
			},
		))
	}

	// 构建请求并发送
	params := openai.ChatCompletionNewParams{
		Model:    p.model,
		Messages: openaiMessages,
	}

	// 慢思考机制支撑，仅当 availableTools 存在时才挂载 Tools
	if len(openaiTools) > 0 {
		params.Tools = openaiTools
	}

	resp, err := p.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("Openai Api 请求失败：%w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("Openai Api 返回了空")
	}

	choice := resp.Choices[0].Message
	resultMessage := &schema.Message{
		// question 这里为什么是 assistant？
		Role:    schema.RoleAssistant,
		Content: choice.Content,
	}

	// 提取 Usage 信息
	if resp.Usage.PromptTokens > 0 || resp.Usage.CompletionTokens > 0 {
		resultMessage.Usage = &schema.Usage{
			PromptTokenNum:     resp.Usage.PromptTokens,
			CompletionTokenNum: resp.Usage.CompletionTokens,
		}
	}

	for _, tc := range choice.ToolCalls {
		if tc.Type == "function" {
			resultMessage.ToolCalls = append(resultMessage.ToolCalls, schema.ToolCall{
				Id:        tc.ID,
				Name:      tc.Function.Name,
				Arguments: []byte(tc.Function.Arguments),
			})
		}
	}

	return resultMessage, nil
}
