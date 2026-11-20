package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"

	"eamon-claw/internal/schema"
)

const BASH_DEFAULT_MAX_LEN int = 8000

type bashTool struct {
	workDir    string
	maxLen     int
	timeoutSec int
}

func NewBashTool(wd string, maxLen int, timeoutSec int) *bashTool {
	if maxLen <= 0 {
		maxLen = BASH_DEFAULT_MAX_LEN
	}
	if timeoutSec <= 0 {
		timeoutSec = 30
	}
	return &bashTool{
		workDir:    wd,
		maxLen:     maxLen,
		timeoutSec: timeoutSec,
	}
}

func (t *bashTool) Name() string {
	return "bash"
}

func (t *bashTool) Definition() schema.ToolDefinition {
	return schema.ToolDefinition{
		Name:        t.Name(),
		Description: "在当前工作区执行任意的 bash 命令。支持链式命令（如 &&）。返回标准输出（stdout）和标准错误（stderr）。",
		InputSchema: schema.InputSchemaParams{
			"type": "object",
			"properties": schema.InputSchemaParams{
				"command": schema.InputSchemaParams{
					"type":        "string",
					"description": "要执行的 bash 命令，例如: ls -la 或 go test ./...",
				},
			},
			"required": []string{"command"},
		},
	}
}

type bashToolArgs struct {
	Command string `json:"command"`
}

func (t *bashTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var input bashToolArgs
	if err := json.Unmarshal(args, &input); err != nil {
		return "", fmt.Errorf("命令参数解析失败：%w", err)
	}

	// 【驾驭底线 1】：Time Budgeting (时间预算与超时控制)
	// 给予 bash 命令一个最大执行时间，防止大模型卡死进程 (比如运行了 top 或持续监听的 Web 服务)
	ctx, cancel := context.WithTimeout(ctx, time.Duration(t.timeoutSec)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-c", input.Command)

	// 【驾驭底线 2】：绑定执行的工作区目录
	// 确保命令默认在用户指定的 WorkDir 下执行，而不是引擎启动时的绝对路径。
	cmd.Dir = t.workDir

	result, err := cmd.CombinedOutput()
	output := string(result)

	// 【驾驭底线 3】：长度截断保护 (防 OOM)
	if len(output) > t.maxLen {
		output = fmt.Sprintf("%s\n\n...[终端输出过长，已截断至前 %d 字节]...", output[:t.maxLen], t.maxLen)
	}

	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("命令执行超时(30s)，已被系统强制终止。如果是启动常驻服务，请尝试将其转入后台。已产生的输出：\n%s", output)
	}

	// 【驾驭底线 4】：错误原样回传 (Self-Correction 自愈机制)
	// 失败时必须把终端输出（含 stderr）一并带回，让大模型自己分析报错并修正
	if err != nil {
		return "", fmt.Errorf("命令执行失败（%v），终端输出：\n%s", err, output)
	}

	if output == "" {
		return "命令执行成功，无终端输出。", nil
	}

	return output, nil
}
