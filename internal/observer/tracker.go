package observer

import (
	"context"
	"log"
	"time"

	"eamon-claw/internal/history"
	"eamon-claw/internal/provider"
	"eamon-claw/internal/schema"
)

var PricingModel = map[string]struct {
	InputPrice  float64
	OutputPrice float64
}{
	"gemini-3-pro-preview": {InputPrice: 0.15, OutputPrice: 0.15}, // 这里假定的大模型价格(每百万Token，tk)
}

type CostTracker struct {
	nextProvider provider.LLMProvider
	modelName    string
	session      *history.Session
}

func NewCostTracker(next provider.LLMProvider, modelName string, session *history.Session) *CostTracker {
	return &CostTracker{
		nextProvider: next,
		modelName:    modelName,
		session:      session,
	}
}

func (t *CostTracker) Generate(ctx context.Context, messages []schema.Message, avaliableTools []schema.ToolDefinition) (*schema.Message, error) {
	startTime := time.Now

	respMessage, err := t.nextProvider.Generate(ctx, messages, avaliableTools)

	// 计算耗时
	latency := time.Since(startTime())

	if err != nil {
		log.Printf("[Tracker] ❌ API 调用失败，耗时: %v\n", latency)
		return respMessage, err
	}

	if respMessage.Usage != nil {
		promptNum := respMessage.Usage.PromptTokenNum
		completionNum := respMessage.Usage.CompletionTokenNum

		var cost float64
		if price, ok := PricingModel[t.modelName]; ok {
			cost = float64(float64(promptNum)*price.InputPrice+float64(completionNum)*price.OutputPrice) / 1000000.0
		}

		log.Printf("[Tracker] 📈 API调用完成 | 耗时：%v | 输入：%d | 输出：%d | 花费：%.6f\n", latency, promptNum, completionNum, cost)

		if t.session != nil {
			t.session.TotalPromptTokenNum = promptNum
			t.session.TotalCompletionTokenNum = completionNum
			t.session.TotalCost = cost
		}

	} else {
		log.Printf("[Tracker] ⚠️ API 调用完成，但未返回 Usage 数据｜耗时：%v\n", latency)
	}

	return respMessage, nil
}
