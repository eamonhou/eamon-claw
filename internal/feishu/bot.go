package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"eamon-claw/internal/engine"
	"eamon-claw/internal/history"
	"eamon-claw/internal/schema"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

type FeishuBot struct {
	appId     string
	appSecret string
	client    *lark.Client

	// 持有核心引擎引用
	engine *engine.AgentEngine

	// session信息
	session *history.Session

	// 实现Reporter接口的FeishuReporter实例
	reporter *FeishuReporter
}

func NewFeishu(eg *engine.AgentEngine) *FeishuBot {
	appId := ""
	appSecret := ""

	if appId == "" || appSecret == "" {
		log.Fatal("请设置飞书 appId 和 app secret")
	}

	client := lark.NewClient(appId, appSecret)

	return &FeishuBot{
		appId:     appId,
		appSecret: appSecret,
		client:    client,
		engine:    eg,
	}
}

func (b *FeishuBot) GetEventDispatcher() *dispatcher.EventDispatcher {
	encryptKey := os.Getenv("FEISHU_ENCRYPT_KEY")
	verifyToken := os.Getenv("FEISHU_VERIFY_TOKEN")
	handler := dispatcher.NewEventDispatcher(encryptKey, verifyToken).OnP2MessageReceiveV1(func(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
		contentStr := *event.Event.Message.Content
		contentStr = strings.TrimPrefix(contentStr, `{"text":"`)
		contentStr = strings.TrimSuffix(contentStr, `"}`)

		chatId := *event.Event.Message.ChatId
		log.Printf("[Feishu] 收到会话 %s 消息：%s\n", chatId, contentStr)

		// 拦截人工审批的特殊口令
		if strings.HasPrefix(contentStr, "approve ") {
			taskID := strings.TrimPrefix(contentStr, "approve ")
			taskID = strings.TrimSpace(taskID)
			// 唤醒挂起的引擎协程！
			GlobalApprovalManager.ResolveApproval(taskID, true, "人类管理员已批准操作")
			log.Printf("[Feishu] 会话 %s: ✅ 已为您批准任务 %s", chatId, taskID)
			return nil
		}
		if strings.HasPrefix(contentStr, "reject ") {
			taskID := strings.TrimPrefix(contentStr, "reject ")
			taskID = strings.TrimSpace(taskID)
			// 唤醒挂起的引擎协程，并反馈拒绝理由！
			GlobalApprovalManager.ResolveApproval(taskID, false, "人类管理员认为该操作存在极高风险，已无情拒绝")
			log.Printf("[Feishu] 会话 %s: 🚫 已拒绝任务 %s", chatId, taskID)
			return nil
		}

		go b.handleAgentRun(chatId, contentStr)

		return nil
	}).OnP2MessageReadV1(func(ctx context.Context, event *larkim.P2MessageReadV1) error {
		// 消息已读事件，静默忽略
		return nil
	})
	return handler
}

func (b *FeishuBot) Reporter() *FeishuReporter {
	return b.reporter
}

func (b *FeishuBot) handleAgentRun(chatId string, prompt string) {
	reporter := &FeishuReporter{client: b.client, chatId: chatId}
	b.reporter = reporter
	b.session.Append(schema.Message{
		Role:    schema.RoleUser,
		Content: prompt,
	})
	// 将prompt加入会话中
	err := b.engine.Run(context.Background(), b.session, reporter, false)
	if err != nil {
		reporter.sendMsg(fmt.Sprintf("❌ Agent 运行崩溃: %v", err))
	}
}

type FeishuReporter struct {
	client *lark.Client
	chatId string
}

// sendMsg 封装了调用飞书 OpenAPI 发送卡片/文本的操作
func (r *FeishuReporter) sendMsg(text string) {
	// 构建文本消息内容
	textContent := map[string]string{
		"text": text,
	}
	contentBytes, _ := json.Marshal(textContent)
	contentStr := string(contentBytes)
	msgReq := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(larkim.CreateMessageV1ReceiveIDTypeChatId).
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(r.chatId).
			MsgType(larkim.MsgTypeText).
			Content(contentStr).
			Build()).
		Build()
	_, _ = r.client.Im.Message.Create(context.Background(), msgReq)
}

func (r *FeishuReporter) OnThinking(ctx context.Context) {
	r.sendMsg("🤔模型正在思考...")
}

func (r *FeishuReporter) OnToolCall(ctx context.Context, toolName string, args string) {
	r.sendMsg(fmt.Sprintf("🔧正在执行工具：%s\n参数：%s", toolName, args))
}

func (r *FeishuReporter) OnToolResult(ctx context.Context, toolName string, result string, isError bool) {
	if isError {
		r.sendMsg(fmt.Sprintf("❌执行工具错误：%s\n%s", toolName, result))
	} else {
		r.sendMsg(fmt.Sprintf("✅执行工具成功：%s", toolName))
	}
}

func (r *FeishuReporter) OnMessage(ctx context.Context, content string) {
	// 将模型最终的纯文本回答发给用户
	r.sendMsg(content)
}

// 编译时类型检查：确保 FeishuReporter 实现了 Reporter 接口var
var _ engine.Reporter = (*FeishuReporter)(nil)
