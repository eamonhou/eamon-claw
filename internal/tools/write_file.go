package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"eamon-claw/internal/schema"
)

type WriteFileTool struct {
	workDir string
}

func NewWriteFileTool(wd string) *WriteFileTool {
	return &WriteFileTool{
		workDir: wd,
	}
}

func (t *WriteFileTool) Name() string {
	return "write_file"
}

func (t *WriteFileTool) Definition() schema.ToolDefinition {
	return schema.ToolDefinition{
		Name:        t.Name(),
		Description: "创建或者覆盖写入一个文件。如果目录不存在自动创建。请提供相对于工作区的相对路径。",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": schema.InputSchemaParams{
				"path": schema.InputSchemaParams{
					"type":        "string",
					"description": "要写入的文件路径，如src/main.go",
				},
				"content": schema.InputSchemaParams{
					"type":        "string",
					"description": "要写入的完整文件内容",
				},
			},
			"required": []string{"path", "content"},
		},
	}
}

type writeFileArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (t *WriteFileTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var input writeFileArgs
	if err := json.Unmarshal(args, &input); err != nil {
		return "", fmt.Errorf("命令参数解析失败：%w", err)
	}

	fpath := filepath.Join(t.workDir, input.Path)

	// 自动创建缺失的父级目录
	if err := os.MkdirAll(filepath.Dir(fpath), 0755); err != nil {
		return "", fmt.Errorf("创建父目录失败：%w", err)
	}

	// 写入文件内容，权限设置为 0644
	if err := os.WriteFile(fpath, []byte(input.Content), 0644); err != nil {
		return "", fmt.Errorf("写入文件内容失败：%w", err)
	}

	return fmt.Sprintf("成功内容写入到文件：%s", input.Path), nil
}
