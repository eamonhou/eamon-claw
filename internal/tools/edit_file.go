package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"eamon-claw/internal/schema"
)

type EditFileTool struct {
	workDir string
}

func NewEditFileTool(wd string) *EditFileTool {
	return &EditFileTool{
		workDir: wd,
	}
}

func (t *EditFileTool) Name() string {
	return "edit_file"
}

func (t *EditFileTool) Definition() schema.ToolDefinition {
	return schema.ToolDefinition{
		Name:        t.Name(),
		Description: "对现有文件进行局部的字符串替换。这比重写整个文件更安全、更快速。请提供足够的 old_text 上下文以确保匹配的唯一性。",
		InputSchema: schema.InputSchemaParams{
			"type": "object",
			"properties": schema.InputSchemaParams{
				"path": schema.InputSchemaParams{
					"type":        "string",
					"description": "要修改的文件相对路径",
				},
				"old_text": schema.InputSchemaParams{
					"type":        "string",
					"description": "文件中原有的文本。必须包含足够的上下文（建议上下各多包含几行），以确保在文件中的唯一性。",
				},
				"new_text": schema.InputSchemaParams{
					"type":        "string",
					"description": "要替换成的新文本",
				},
			},
			"required": []string{"path", "old_text", "new_text"},
		},
	}
}

type EditFileArgs struct {
	Path    string `json:"path"`
	OldText string `json:"old_text"`
	NewText string `json:"new_text"`
}

func (t *EditFileTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var input EditFileArgs
	if err := json.Unmarshal(args, &input); err != nil {
		return "", fmt.Errorf("命令参数解析失败：%w", err)
	}

	fpath := filepath.Join(t.workDir, input.Path)

	// 路径逃逸校验
	absWorkDir, err := filepath.Abs(t.workDir)
	if err != nil {
		return "", fmt.Errorf("工作区路径解析失败：%w", err)
	}
	absTarget, err := filepath.Abs(fpath)
	if err != nil {
		return "", fmt.Errorf("目标路径解析失败：%w", err)
	}
	if absTarget != absWorkDir && !strings.HasPrefix(absTarget, absWorkDir+string(filepath.Separator)) {
		return "", fmt.Errorf("禁止编辑工作区以外的路径：%s", input.Path)
	}

	content, err := os.ReadFile(fpath)
	if err != nil {
		return "", fmt.Errorf("读取文件失败：%w", err)
	}
	original := string(content)

	// 统计命中次数，确保唯一替换
	count := strings.Count(original, input.OldText)
	if count == 0 {
		return "", fmt.Errorf("在文件中未找到 old_text，请先用 read_file 确认文件当前内容后重试")
	}
	if count > 1 {
		return "", fmt.Errorf("old_text 匹配到了多处（共 %d 处），请提供更多上下文以确保唯一性", count)
	}

	updated := strings.Replace(original, input.OldText, input.NewText, 1)
	if err := os.WriteFile(fpath, []byte(updated), 0644); err != nil {
		return "", fmt.Errorf("写入文件失败：%w", err)
	}

	return fmt.Sprintf("成功编辑文件：%s", input.Path), nil
}
