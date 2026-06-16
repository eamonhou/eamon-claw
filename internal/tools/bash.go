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
	workDir string
	maxLen  int
}

func NewBashTool(wd string) *bashTool {
	return &bashTool{
		workDir: wd,
		maxLen:  BASH_DEFAULT_MAX_LEN,
	}
}

func (t *bashTool) Name() string {
	return "bash"
}

func (t *bashTool) Definition() schema.ToolDefinition {
	return schema.ToolDefinition{
		Name:        t.Name(),
		Description: "在当前工作区执行任意的 bash 命令。支持链式命令（如 &&）。返回标准输出（stdout）和标准输入（stdin）。",
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
	ctx, cancel := context.WithTimeout(ctx, time.Second*30)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-c", input.Command)

	// 【驾驭底线 2】：绑定执行的工作区目录
	// 确保命令默认在用户指定的 WorkDir 下执行，而不是引擎启动时的绝对路径。
	cmd.Dir = t.workDir

	result, err := cmd.CombinedOutput()
	output := string(result)

	if ctx.Err() == context.DeadlineExceeded {
		return output + "\n[警告: 命令执行超时(30s)，已被系统强制终止。如果是启动常驻服务，请尝试将其转入后台。]", nil
	}

	// 【驾驭底线 3】：错误原样回传 (Self-Correction 自愈机制)
	// 当 bash 报错时（err != nil），我们绝对不能返回 Go 的 error 阻断程序！
	// 我们必须把 err 和 outputStr 拼接成字符串返回，利用大模型的自纠错能力自己分析报错！
	if err != nil {
		return "", fmt.Errorf("执行命令 %s 失败：%w", cmd.String(), err)
	}

	if output == "" {
		return "命令执行成功，无终端输出。", nil
	}

	// 【驾驭底线 4】：长度截断保护 (防 OOM)
	if len(output) > t.maxLen {
		return fmt.Sprintf("%s\n\n...[终端输出过长，已截断至前 %d 字节]...", output[:t.maxLen], t.maxLen), nil
	}

	return output, nil
}
