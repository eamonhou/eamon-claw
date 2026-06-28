package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"

	"eamon-claw/internal/schema"
)

//
// LA-CPM 算法工具
//

type LacpmTool struct {
	workDir string
	maxLen  int
}

func NewLacpmTool(wd string) *LacpmTool {
	return &LacpmTool{
		workDir: wd,
		maxLen:  BASH_DEFAULT_MAX_LEN,
	}
}

func (t *LacpmTool) Name() string {
	return "lacpm"
}

func (t *LacpmTool) Definition() schema.ToolDefinition {
	return schema.ToolDefinition{
		Name:        t.Name(),
		Description: "LA-CPM 算法工具",
		InputSchema: schema.InputSchemaParams{
			"type": "object",
			"properties": schema.InputSchemaParams{
				"prometheus_url": schema.InputSchemaParams{
					"type":        "string",
					"description": "Prometheus 监控基础设施服务物理端口 (默认 http://127.0.0.1:9090)",
				},
				"db_url": schema.InputSchemaParams{
					"type":        "string",
					"description": "分析隔离读库的 MySQL DSN 连接串",
				},
			},
			"required": []string{"prometheus_url", "db_url"},
		},
	}
}

type LacpmToolArgs struct {
	PrometheusUrl string `json:"prometheus_url"`
	DbUrl         string `json:"db_url"`
}

func (t *LacpmTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var input LacpmToolArgs
	if err := json.Unmarshal(args, &input); err != nil {
		return "", fmt.Errorf("LA-CPM 认知参数反序列化失败: %w", err)
	}

	// 赋予默认值兜底
	if input.PrometheusUrl == "" {
		input.PrometheusUrl = "http://127.0.0.1:9090"
	}

	// 【驾驭底线 1】：Time Budgeting 超时防御
	ctx, cancel := context.WithTimeout(ctx, time.Second*20)
	defer cancel()

	// 3. 核心修正：精准拼装本地二进制文件的声明式 Flags 路由，而非原生的裸参数
	cmdArgs := []string{
		"--prometheus_url=" + input.PrometheusUrl,
		"--db_url=" + input.DbUrl,
		// "--buffer=10", // 固定两侧外扩 10 秒的双向因果视窗
	}

	// 执行在 dist/ 下编译好的本地命令行工具
	cmd := exec.CommandContext(ctx, "./lacpm-tool", cmdArgs...)
	cmd.Dir = t.workDir

	result, err := cmd.CombinedOutput()
	output := string(result)

	fmt.Println(output)

	if ctx.Err() == context.DeadlineExceeded {
		return output + "\n[警告: LA-CPM 基础设施分析超时(20s)，已被 Harness 引擎断头终止。]", nil
	}

	// 【驾驭底线 3】：错误原样回传 (Self-Correction 自愈机制)
	// 将 stderr 的物理报错也作为 Observation 扔给大模型，让它自省是不是传错参数、或者是读库连接失败
	if err != nil {
		return fmt.Sprintf("❌ LA-CPM 本地二进制执行失败。终端输出:\n%s\n底层错误: %v", output, err), nil
	}

	// 【驾驭底线 4】：长度截断保护
	if len(output) > t.maxLen {
		return fmt.Sprintf("%s\n\n...[账单过长已被截断]...", output[:t.maxLen]), nil
	}

	return output, nil
}
