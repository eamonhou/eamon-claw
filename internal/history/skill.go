package history

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// 标准 Agent Skill 规范加载与解析器

type Skill struct {
	Name        string
	Description string
	Body        string //markdown正文指令
}

// SkillLoader 负责从本地文件系统中加载并解析符合规范的技能模板
type SkillLoader struct {
	workDir string
}

func NewSkillLoader(wd string) *SkillLoader {
	return &SkillLoader{
		workDir: wd,
	}
}

// LoadAll 加载 .claw/skills 目录下所有 SKILL.md，并格式化字符串准备注入到上下文中
func (s *SkillLoader) LoadAll() string {
	skillBasePath := filepath.Join(s.workDir, ".claw", "skills")

	// 如果目录不存在说明目前还没有任何技能
	if _, err := os.Stat(skillBasePath); os.IsNotExist(err) {
		return ""
	}

	var skillsBuilder strings.Builder

	skillsBuilder.WriteString("\n### 可用的专业技能（Agent Skills）\n")
	skillsBuilder.WriteString("以下是你拥有的标准化外挂技能，请在符合 description 描述的场景下严格遵循其正文指令：\n\n")

	if err := filepath.WalkDir(skillBasePath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !d.IsDir() && d.Name() == "SKILL.md" {
			content, err := os.ReadFile(path)

			// 将 content 解析为 Skill
			skill := parseSkillMd(string(content))

			if err == nil {
				skillsBuilder.WriteString(fmt.Sprintf("#### 技能名称：%s\n", skill.Name))
				skillsBuilder.WriteString(fmt.Sprintf("**触发条件**：%s\n\n", skill.Description))
				skillsBuilder.WriteString("**执行指南**:\n")
				skillsBuilder.WriteString(skill.Body)
				skillsBuilder.WriteString("\n\n---\n")
			}
		}
		return nil
	}); err != nil {
		return ""
	}
	return skillsBuilder.String()
}

func parseSkillMd(content string) Skill {
	skill := Skill{
		Name:        "Unknown Skill",
		Description: "No description provided",
		Body:        content,
	}

	// 简单解析 YAML Frontmatter (以 --- 包裹)
	if strings.HasPrefix(content, "---\n") || strings.HasPrefix(content, "---\r\n") {
		parts := strings.SplitN(content, "---", 3)
		if len(parts) == 3 {
			formatter := parts[1]
			skill.Body = strings.TrimSpace(parts[2])

			// 逐行提取 metadata
			lines := strings.Split(formatter, "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "name") {
					skill.Name = strings.TrimSpace(strings.TrimPrefix(line, "name:"))
				} else if strings.HasPrefix(line, "description") {
					skill.Description = strings.TrimSpace(strings.TrimPrefix(line, "description:"))
				}
			}
		}
	}
	return skill
}
