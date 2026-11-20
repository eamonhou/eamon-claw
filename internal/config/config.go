package config

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

// FileConfig 是配置文件（.claw/config.json）的结构，所有字段可选
type FileConfig struct {
	ModelName         string `json:"model_name"`
	APIBaseURL        string `json:"api_base_url"`
	SessionID         string `json:"session_id"`
	PlanMode          *bool  `json:"plan_mode"`
	EnableThinking    *bool  `json:"enable_thinking"`
	MaxTurnMain       int    `json:"max_turn_main"`
	MaxTurnSub        int    `json:"max_turn_sub"`
	CompactMaxChar    int    `json:"compact_max_char"`
	CompactRetainLast int    `json:"compact_retain_last"`
	BashTimeoutSec    int    `json:"bash_timeout_sec"`
	BashMaxLen        int    `json:"bash_max_len"`
	ReadFileMaxLen    int    `json:"read_file_max_len"`
	FeishuPort        string `json:"feishu_port"`
	FeishuEncryptKey  string `json:"feishu_encrypt_key"`
	FeishuVerifyToken string `json:"feishu_verify_token"`
}

// Config 是运行时使用的完整配置，由默认值 + 配置文件 + flag 三层合并而成
type Config struct {
	ModelName         string
	APIKey            string // 只从环境变量 VECTAI_API_KEY 读取
	APIBaseURL        string
	SessionID         string
	PlanMode          bool
	EnableThinking    bool
	MaxTurnMain       int
	MaxTurnSub        int
	CompactMaxChar    int
	CompactRetainLast int
	BashTimeoutSec    int
	BashMaxLen        int
	ReadFileMaxLen    int
	FeishuPort        string
	FeishuEncryptKey  string
	FeishuVerifyToken string
	Prompt            string
}

// Load 按优先级合并配置：代码默认值 < 配置文件 < 命令行 flag
// 配置文件路径：<workDir>/.claw/config.json
func Load(workDir string) *Config {
	c := defaults()

	// 第一层：配置文件覆盖默认值
	mergeFileConfig(c, loadFileConfig(workDir))

	// 第二层：命令行 flag 覆盖配置文件
	// 使用指针 flag，只有用户显式传入才覆盖
	var (
		prompt   = flag.String("prompt", "", "要交给 Agent 执行的任务描述")
		model    = flag.String("model", "", "模型名称")
		session  = flag.String("session", "", "会话 ID")
		plan     = flag.Bool("plan", false, "开启计划模式")
		thinking = flag.Bool("thinking", false, "开启慢思考")
	)
	flag.Parse()

	if *prompt != "" {
		c.Prompt = *prompt
	}
	if *model != "" {
		c.ModelName = *model
	}
	if *session != "" {
		c.SessionID = *session
	}
	// bool flag 只在用户明确传入时才覆盖（通过检测 flag.Visit）
	flag.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "plan":
			c.PlanMode = *plan
		case "thinking":
			c.EnableThinking = *thinking
		}
	})

	// API Key 只走环境变量，不进配置文件（避免密钥落盘）
	c.APIKey = os.Getenv("VECTAI_API_KEY")

	return c
}

// Validate 检查必填项
func (c *Config) Validate() error {
	if c.Prompt == "" {
		return fmt.Errorf("请通过 -prompt 参数提供任务描述")
	}
	if c.APIKey == "" {
		return fmt.Errorf("请设置环境变量 VECTAI_API_KEY")
	}
	return nil
}

// ConfigFilePath 返回当前项目的配置文件路径（供外部打印提示用）
func ConfigFilePath(workDir string) string {
	return filepath.Join(workDir, ".claw", "config.json")
}

// loadFileConfig 读取并解析配置文件，文件不存在时返回零值结构体
func loadFileConfig(workDir string) FileConfig {
	path := ConfigFilePath(workDir)
	data, err := os.ReadFile(path)
	if err != nil {
		return FileConfig{}
	}
	var fc FileConfig
	if err := json.Unmarshal(data, &fc); err != nil {
		fmt.Fprintf(os.Stderr, "[Config] 配置文件解析失败（%s）：%v，已忽略\n", path, err)
		return FileConfig{}
	}
	return fc
}

// mergeFileConfig 将配置文件中非零值字段覆盖到 c 中
func mergeFileConfig(c *Config, fc FileConfig) {
	if fc.ModelName != "" {
		c.ModelName = fc.ModelName
	}
	if fc.APIBaseURL != "" {
		c.APIBaseURL = fc.APIBaseURL
	}
	if fc.SessionID != "" {
		c.SessionID = fc.SessionID
	}
	if fc.PlanMode != nil {
		c.PlanMode = *fc.PlanMode
	}
	if fc.EnableThinking != nil {
		c.EnableThinking = *fc.EnableThinking
	}
	if fc.MaxTurnMain > 0 {
		c.MaxTurnMain = fc.MaxTurnMain
	}
	if fc.MaxTurnSub > 0 {
		c.MaxTurnSub = fc.MaxTurnSub
	}
	if fc.CompactMaxChar > 0 {
		c.CompactMaxChar = fc.CompactMaxChar
	}
	if fc.CompactRetainLast > 0 {
		c.CompactRetainLast = fc.CompactRetainLast
	}
	if fc.BashTimeoutSec > 0 {
		c.BashTimeoutSec = fc.BashTimeoutSec
	}
	if fc.BashMaxLen > 0 {
		c.BashMaxLen = fc.BashMaxLen
	}
	if fc.ReadFileMaxLen > 0 {
		c.ReadFileMaxLen = fc.ReadFileMaxLen
	}
	if fc.FeishuPort != "" {
		c.FeishuPort = fc.FeishuPort
	}
	if fc.FeishuEncryptKey != "" {
		c.FeishuEncryptKey = fc.FeishuEncryptKey
	}
	if fc.FeishuVerifyToken != "" {
		c.FeishuVerifyToken = fc.FeishuVerifyToken
	}
}

func defaults() *Config {
	return &Config{
		ModelName:         "gemini-3-pro-preview",
		APIBaseURL:        "https://api.vectorengine.ai/v1",
		SessionID:         "chat_01",
		PlanMode:          false,
		EnableThinking:    false,
		MaxTurnMain:       20,
		MaxTurnSub:        15,
		CompactMaxChar:    20000,
		CompactRetainLast: 30,
		BashTimeoutSec:    30,
		BashMaxLen:        8000,
		ReadFileMaxLen:    4000,
		FeishuPort:        ":48080",
	}
}
