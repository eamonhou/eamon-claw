# go-tiny-claw

一个用 Go 编写的轻量级、可扩展 AI Agent 框架，支持工具调用、子智能体、上下文压缩、慢思考、飞书集成和全链路追踪。

---

## 目录

- [快速开始](#快速开始)
- [配置](#配置)
- [架构概览](#架构概览)
- [核心技术实现](#核心技术实现)
- [内置工具](#内置工具)
- [飞书集成](#飞书集成)

---

## 快速开始

**前置条件：** Go 1.26.3+，以及一个兼容 OpenAI 格式的 LLM API 密钥。

```bash
# 克隆并编译
git clone <repo-url>
cd eamon-claw
go build -o claw ./cmd/claw/

# 设置 API 密钥（唯一必须通过环境变量传入的项）
export VECTAI_API_KEY=your_api_key

# 运行一个任务
./claw -prompt "列出当前目录下的所有 Go 文件并统计行数"
```

**可选 flag：**

| Flag | 说明 | 默认值 |
|---|---|---|
| `-prompt` | 任务描述（必填） | — |
| `-model` | 模型名称 | `gemini-3-pro-preview` |
| `-session` | 会话 ID，用于隔离上下文 | `chat_01` |
| `-plan` | 开启计划模式（先规划再执行） | `false` |
| `-thinking` | 开启慢思考（两阶段推理） | `false` |

---

## 配置

配置优先级从低到高：**代码默认值 → `.claw/config.json` → 命令行 flag**。

在项目根目录创建 `.claw/config.json`（所有字段均可选）：

```json
{
  "model_name": "gemini-3-pro-preview",
  "api_base_url": "https://api.vectorengine.ai/v1",
  "session_id": "chat_01",
  "plan_mode": false,
  "enable_thinking": false,
  "max_turn_main": 20,
  "max_turn_sub": 15,
  "compact_max_char": 20000,
  "compact_retain_last": 30,
  "bash_timeout_sec": 30,
  "bash_max_len": 8000,
  "read_file_max_len": 8000,
  "feishu_port": ":48080",
  "feishu_encrypt_key": "",
  "feishu_verify_token": ""
}
```

> `VECTAI_API_KEY` 是唯一不写入配置文件的项，防止密钥落盘。

---

## 架构概览

```
cmd/claw/
└── main.go              # 入口：加载配置、注册工具、启动引擎

internal/
├── config/              # 三层配置合并（默认值 / JSON 文件 / flag）
├── engine/              # 核心推理循环、慢思考、防死循环提醒
├── history/             # 会话管理、上下文压缩、自愈、Skill 注入
├── observer/            # 链路追踪（Span / Trace）、费用统计
├── provider/            # LLM Provider 抽象（OpenAI 兼容格式）
├── schema/              # 消息、工具调用、工具结果的统一数据结构
├── tools/               # 工具注册表、中间件、内置工具实现
└── feishu/              # 飞书 Bot、事件分发、人工审批
```

**数据流：**

```
用户输入
  └─> Session（历史管理）
        └─> AgentEngine.Run（主循环）
              ├─> Compactor（上下文压缩）
              ├─> [可选] Thinking Phase（剥夺工具，强制规划）
              ├─> Action Phase（携带工具，生成调用）
              ├─> Registry.Execute（工具执行 + 中间件拦截）
              │     └─> [可选] SubagentTool（启动子智能体）
              └─> ReminderInject（防死循环提醒追加）
```

---

## 核心技术实现

### 1. 两阶段推理（慢思考）

开启 `-thinking` 后，每轮推理拆成两次 LLM 调用：

- **Thinking Phase**：不传工具定义，强迫模型先规划、再决策。若直接给模型工具列表，大多数模型会跳过思考直接调用工具。
- **Action Phase**：将 Thinking 结果作为 assistant 消息追加到历史，再携带工具进行第二次调用。

### 2. 上下文压缩（Compactor）

每轮推理前，`Compactor` 对消息列表做两种处理，防止上下文窗口溢出：

- **远期历史掩码（Masking）**：超过字符水位线的早期消息内容替换为 `[已压缩]`，保留角色标记以维持对话结构。
- **短期保护区截断（Truncation）**：最近 N 条消息（`compact_retain_last`）受保护，仅对超长单条消息做头尾截断。

工具返回结果往往是上下文膨胀的主因，压缩策略让 agent 在长任务中保持稳定。

### 3. 防死循环提醒（ReminderInject）

`ReminderInject` 追踪每轮的工具调用情况。若连续多轮调用同一工具且参数重复，会在工具结果全部收集完毕后，将提醒消息作为 `user` 消息追加——而非插入工具结果序列中间。

这个顺序至关重要：OpenAI 协议要求 `tool_result` 消息必须连续紧跟在含工具调用的 `assistant` 消息之后，中间插入其他消息会导致 400 错误。

### 4. 工具注册表与中间件

`Registry` 是工具的统一管理层，支持链式中间件：

```go
mainToolRegistry.Use(func(ctx context.Context, call schema.ToolCall) (bool, string) {
    if feishu.IsDangerousCommand(call.Name, string(call.Arguments)) {
        allowed, reason := feishu.GlobalApprovalManager.WaitForApproval(...)
        return allowed, reason
    }
    return true, ""
})
```

高危命令（如 `rm -rf`、`git reset --hard` 等）在执行前被中间件拦截，通过飞书向人工发起审批，引擎协程阻塞等待结果。工具定义按名称字母序排列，以提升 LLM API 的前缀缓存命中率。

### 5. 子智能体（SubagentTool）

主 agent 可通过 `spawn_subagent` 工具派生子任务。子 agent：

- 只持有**只读注册表**（`read_file`、`bash`），无法写文件或再次派生子 agent。
- 独立运行推理循环，最终输出纯文本摘要返回给主 agent。
- 通过 `AgentRunner` 接口注入，打破 `tools` 包与 `engine` 包之间的循环依赖。

### 6. 链路追踪与费用统计

`observer` 包实现了轻量级 Span/Trace 机制：

- 每次 `AgentEngine.Run` 开启根 Span，每轮 turn、每次 LLM 调用、每次工具执行分别记录子 Span。
- 任务结束后自动导出 JSON 格式的 Trace 文件到 `.claw/traces/` 目录，供回放和调试。

`CostTracker` 包装 LLM Provider，累计 input/output token 数，按模型单价换算人民币费用，任务结束后输出财务报表。

### 7. 自愈机制（RecoveryManager）

当工具执行返回错误时，`RecoveryManager` 会在下一轮推理的上下文中注入结构化的错误提示，引导模型分析原因、调整参数后重试，而非直接终止任务。

---

## 内置工具

| 工具 | 权限 | 说明 |
|---|---|---|
| `read_file` | 只读 | 读取文件内容，支持长度截断，含路径逃逸检查 |
| `write_file` | 写 | 创建或覆盖文件，限制在工作区内 |
| `edit_file` | 写 | 基于上下文的精确替换，拒绝歧义匹配 |
| `bash` | 写 | 执行 shell 命令，支持超时和输出截断，错误时回填 stderr 供模型自愈 |
| `spawn_subagent` | — | 派生只读子智能体处理子任务 |

所有文件类工具在执行前通过 `filepath.Abs` 验证路径边界，防止路径穿越攻击。

---

## 飞书集成

飞书模块目前处于预备状态（`main.go` 中已注释），核心能力已实现：

- **消息接收**：通过 Lark SDK 订阅 `P2MessageReceive` 事件，支持加密验签。
- **人工审批**：`ApprovalManager` 在高危操作前向飞书发送审批请求，引擎协程通过 channel 阻塞等待用户点击确认或拒绝。
- **配置统一**：`feishu_encrypt_key`、`feishu_verify_token` 均通过 `.claw/config.json` 管理。

启用步骤：在 `main.go` 中取消飞书相关注释，并在配置文件中填入 App ID、App Secret 及 Webhook 信息。
