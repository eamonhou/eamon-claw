package tools

import (
	"context"
	"encoding/json"

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
	// TODO 四级降级容错
	return "", nil
}
