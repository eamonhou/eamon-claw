package feishu

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"sync"
)

type ApprovalResult struct {
	Allowed bool
	Reason  string
}

// ApprovalManager 统一管理当前正在审批的任务
type ApprovalManager struct {
	// map{ 审批id: 审批通知通道}
	approvingTasks map[string]chan ApprovalResult
	// 并发安全
	mu *sync.RWMutex
}

var GlobalApprovalManager = &ApprovalManager{
	approvingTasks: make(map[string]chan ApprovalResult),
	mu:             &sync.RWMutex{},
}

// WaitForApproval 发送飞书通知并阻塞协程等待审批结果
func (m *ApprovalManager) WaitForApproval(ctx context.Context, taskId string, toolName string, args string, reporter *FeishuReporter) (bool, string) {
	// 创建用于阻塞当前引擎协程的 channel (容量为 1 防止死锁)
	ch := make(chan ApprovalResult, 1)

	m.mu.Lock()
	m.approvingTasks[taskId] = ch
	m.mu.Unlock()

	noticeMsg := fmt.Sprintf(`⚠️ **高危操作审批请求**
Agent 试图执行以下动作：
- 工具：%s
- 参数：%s

任务Id：%s
--> 请在此消息下方回复 "approve %s" 或 "reject %s" 来决定是否放行。`, toolName, args, taskId, taskId, taskId)

	if reporter != nil {
		reporter.sendMsg(noticeMsg)
	} else {
		// 回退到终端打印 (兼容本地 CLI 模式)
		log.Printf("%s\n", noticeMsg)
	}

	log.Printf("[Approval] 审核请求已发送 taskId：%s，等待中...\n", taskId)

	select {
	case <-ctx.Done():
		return false, "审核超时，请重新申请执行工具"
	case result := <-ch:
		m.mu.Lock()
		delete(m.approvingTasks, taskId)
		m.mu.Unlock()
		return result.Allowed, result.Reason
	}
}

func (m *ApprovalManager) ResolveApproval(taskId string, allowed bool, reason string) {
	ch, exist := m.approvingTasks[taskId]

	if exist {
		log.Printf("[Approval] 收到来自飞书的审批结果【TaskId: %s, allowed: %v】\n", taskId, allowed)
		ch <- ApprovalResult{Allowed: allowed, Reason: reason}
	} else {
		log.Printf("[Approval] 找不到对应的 TaskID: %s，可能已超时或处理完毕\n", taskId)
	}
}

// IsDangerousCommand 简单的正则检查黑名单，判断该工具调用是否需要审批
func IsDangerousCommand(toolName string, args string) bool {
	// 对于纯读取的工具，默认 YOLO 模式，全部放行
	if toolName != "bash" && toolName != "write_file" && toolName != "edit_file" {
		return false
	}
	// 针对 bash 的高危模式匹配
	if toolName == "bash" {
		dangerousPatterns := []string{
			`rm\s+-r`, // 级联删除
			`sudo\s+`, // 提权
			`drop\s+`, // 数据库删除
			`>.*\.go`, // 恶意覆盖源代码
		}
		for _, p := range dangerousPatterns {
			matched, _ := regexp.MatchString(p, args)
			if matched {
				return true
			}
		}
	}
	return false
}
