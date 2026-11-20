package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"eamon-claw/internal/engine"
	"eamon-claw/internal/history"
	"eamon-claw/internal/schema"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

type FeishuBot struct {
	appId       string
	appSecret   string
	client      *lark.Client
	workDir     string
	encryptKey  string
	verifyToken string

	// 持有核心引擎引用
	engine *engine.AgentEngine
}

func NewFeishu(eg *engine.AgentEngine, workDir string, encryptKey string, verifyToken string) *FeishuBot {
	appId := os.Getenv("FEISHU_APP_ID")
	appSecret := os.Getenv("FEISHU_APP_SECRET")

	if appId == "" || appSecret == "" {
		log.Fatal("请设置环境变量 FEISHU_APP_ID 和 FEISHU_APP_SECRET")
	}

	client := lark.NewClient(appId, appSecret)

	return &FeishuBot{
		appId:       appId,
		appSecret:   appSecret,
		client:      client,
		engine:      eg,
		workDir:     workDir,
		encryptKey:  encryptKey,
		verifyToken: verifyToken,
	}
}

func (b *FeishuBot) GetEventDispatcher() *dispatcher.EventDispatcher {
	handler := dispatcher.NewEventDispatcher(b.encryptKey, b.verifyToken).OnP2MessageReceiveV1(func(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
		// 正确解析飞书消息 JSON，避免手动字符串截取导致转义问题
		var msgBody struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal([]byte(*event.Event.Message.Content), &msgBody); err != nil {
			log.Printf("[Feishu] 消息解析失败：%v\n", err)
			return nil
		}
		contentStr := msgBody.Text

		chatId := *event.Event.Message.ChatId
		log.Printf("[Feishu] 收到会话 %s 消息：%s\n", chatId, contentStr)

		// 拦截人工审批的特殊口令
		reporter := &FeishuReporter{client: b.client, chatId: chatId}

		switch {
		case len(contentStr) > 8 && contentStr[:8] == "approve ":
			taskID := contentStr[8:]
			GlobalApprovalManager.ResolveApproval(taskID, true, "人类管理员已批准操作")
			log.Printf("[Feishu] 会话 %s: ✅ 已为您批准任务 %s", chatId, taskID)
		case len(contentStr) > 7 && contentStr[:7] == "reject ":
			taskID := contentStr[7:]
			GlobalApprovalManager.ResolveApproval(taskID, false, "人类管理员认为该操作存在极高风险，已无情拒绝")
			log.Printf("[Feishu] 会话 %s: 🚫 已拒绝任务 %s", chatId, taskID)
		default:
			// 每条消息独立创建 Session，避免多用户数据竞争
			go b.handleAgentRun(ctx, chatId, contentStr, reporter)
		}

		return nil
	}).OnP2MessageReadV1(func(ctx context.Context, event *larkim.P2MessageReadV1) error {
		// 消息已读事件，静默忽略
		return nil
	})
	return handler
}

func (b *FeishuBot) handleAgentRun(ctx context.Context, chatId string, prompt string, reporter *FeishuReporter) {
	// 每个 chatId 对应独立 Session，避免多用户混用同一会话
	session := history.GlobalSessionManager.GetOrCreate(chatId, b.workDir)
	session.Append(schema.Message{
		Role:    schema.RoleUser,
		Content: prompt,
	})
	err := b.engine.Run(ctx, session, reporter, false)
	if err != nil {
		reporter.sendMsg(fmt.Sprintf("❌ Agent 运行崩溃: %v", err))
	}
}

type FeishuReporter struct {
	client *lark.Client
	chatId string
}

// sendMsg 封装了调用飞书 OpenAPI 发送文本消息的操作
func (r *FeishuReporter) sendMsg(text string) {
	textContent := map[string]string{
		"text": text,
	}
	contentBytes, _ := json.Marshal(textContent)
	msgReq := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(larkim.CreateMessageV1ReceiveIDTypeChatId).
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(r.chatId).
			MsgType(larkim.MsgTypeText).
			Content(string(contentBytes)).
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
	r.sendMsg(content)
}

// 编译时类型检查：确保 FeishuReporter 实现了 Reporter 接口
var _ engine.Reporter = (*FeishuReporter)(nil)
