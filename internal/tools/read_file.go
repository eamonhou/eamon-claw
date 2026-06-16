package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"eamon-claw/internal/schema"
)

const READFILE_DEFAULT_MAX_LEN int = 4000

type ReadFileTool struct {
	// 将引擎的 WorkDir 注入给工具，限制它只能在此目录及其子目录下操作 workDir string
	workDir string

	// 可读取的文本的最大长度
	// 默认为800
	maxLen int
}

func NewReadFileTool(wd string) *ReadFileTool {
	return &ReadFileTool{
		workDir: wd,
		maxLen:  READFILE_DEFAULT_MAX_LEN,
	}
}

func (t *ReadFileTool) Name() string {
	return "read_file"
}

func (t *ReadFileTool) Definition() schema.ToolDefinition {
	return schema.ToolDefinition{
		Name:        t.Name(),
		Description: "读取指定文件的内容。请提供相对工作区的文件路径。",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "要读取的文件相对工作区的路径，如 cmd/claw/main.go",
				},
			},
			"required": []string{"path"},
		},
	}
}

type readFileArgs struct {
	Path string `json:"path"`
}

func (t *ReadFileTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var input readFileArgs
	if err := json.Unmarshal(args, &input); err != nil {
		return "", fmt.Errorf("命令参数解析失败：%w", err)
	}

	fpath := filepath.Join(t.workDir, input.Path)

	fp, err := os.Open(fpath)
	if err != nil {
		return "", fmt.Errorf("打开文件失败：%w", err)
	}
	defer fp.Close()

	content, err := io.ReadAll(fp)
	if err != nil {
		return "", fmt.Errorf("读取文件内容失败：%w", err)
	}

	// 【核心防线】长度截断保护
	// 为了防止大模型读取几百 MB 的日志文件导致 Context 瞬间爆炸 (OOM)，
	// 我们在工具内部直接进行物理截断。
	if len(content) > t.maxLen {
		truncatedMessage := fmt.Sprintf("%s\n\n...[由于内容过长，已被系统截断至前 %d 字节]...", content[:t.maxLen], t.maxLen)
		return truncatedMessage, nil
	}
	return string(content), nil
}
