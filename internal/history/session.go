package history

import (
	"sync"
	"time"

	"eamon-claw/internal/schema"
)

type Session struct {
	Id        string
	WorkDir   string
	CreatedAt time.Time
	UpdatedAt time.Time

	// 存放此 Session 中所有的用户输入、大模型回复和工具调用结果
	history []schema.Message
	size    int

	// 读写锁，防止并发读写历史时发生 Data Race
	mu *sync.RWMutex

	// 当前会话状态
	status int32

	// 用于统计该 Session 累计消耗的资源
	TotalPromptTokenNum     int64
	TotalCompletionTokenNum int64
	TotalCost               float64
}

func NewSession(id string, wd string) *Session {
	return &Session{
		Id:        id,
		WorkDir:   wd,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		history:   make([]schema.Message, 0),
		mu:        &sync.RWMutex{},
	}
}

func (s *Session) Append(msgs ...schema.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.history = append(s.history, msgs...)
	s.size++
	s.UpdatedAt = time.Now()

	// todo
	// 持久化预留点：在真实的工业级实现中（如 Claude Code），
	// 我们会在这里将 s.history 以 JSONL 的格式 Append 到 workDir/.claw/sessions/xxx.jsonl 中。
	// s.SaveToDisk()
}

// GetWorkingMemory 是驾驭工程的核心
// 他不会返回全量历史，而是从后往前截取最近的 N 条消息，形成 Agent 的“短期工作记忆”
func (s *Session) GetWorkingMemory(limit int) []schema.Message {
	s.mu.Lock()
	defer s.mu.Unlock()

	total := len(s.history)
	// 如果不设限或者历史总量小于限制，全量返回 (需要深拷贝以防外部修改)
	if limit == 0 || total <= limit {
		res := make([]schema.Message, total)
		copy(res, s.history)
		return res
	}

	// 截取最近的 limit 条消息
	res := make([]schema.Message, limit)
	copy(res, s.history[total-limit:])

	// 【驾驭防线】：大模型 API 强制要求历史消息的连续性！
	// 如果我们截断的第一条消息恰好是一个 ToolResult (RoleUser 且含有 ToolCallID)，
	// 但发出这个请求的 ToolCall 被我们截断抛弃了，大模型 API 会直接报 400 Bad Request。
	// 因此，如果切片首条属于“孤儿”工具响应，我们必须将其强行舍弃，顺延到下一条正常的 User/Assistant 消息。
	for len(res) > 0 {
		if res[0].Role == schema.RoleUser && res[0].ToolCallId != "" {
			res = res[1:]
		} else {
			break
		}
	}
	return res
}

func (s *Session) RecordUsage(promptNum int64, completionNum int64, cost float64) {
	s.mu.Lock()
	s.TotalPromptTokenNum += promptNum
	s.TotalCompletionTokenNum += completionNum
	s.TotalCost += cost
	s.mu.Unlock()
}

func (s *Session) Len() int {
	return s.size
}

// 全局 Session Manager 管理多用户/多终端隔离

type SessionManager struct {
	sessions map[string]*Session
	mu       *sync.RWMutex
}

var GlobalSessionManager = &SessionManager{
	sessions: make(map[string]*Session),
	mu:       &sync.RWMutex{},
}

// Exist 检查会话是否已经存在
//
//	@param id 会话 id
//	@return *Session
func (sm *SessionManager) Exist(id string) bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if _, ok := sm.sessions[id]; ok {
		return true
	}
	return false
}

// GetOrCreate 获取或者新建一个会话
func (sm *SessionManager) GetOrCreate(id string, workDir string) *Session {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if session, ok := sm.sessions[id]; ok {
		return session
	}
	session := NewSession(id, workDir)
	sm.sessions[id] = session
	return session
}

// 新创建一个 session，会覆盖老session
func (sm *SessionManager) Create(id string, workDir string) *Session {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session := NewSession(id, workDir)
	sm.sessions[id] = session
	return session
}

type SqlSession struct {
	Id        string
	WorkDir   string
	CreatedAt time.Time
	UpdatedAt time.Time

	// 存放此 Session 中所有的用户输入、大模型回复和工具调用结果
	history []schema.Message
	size    int

	// 读写锁，防止并发读写历史时发生 Data Race
	mu *sync.RWMutex

	// 用于统计该 Session 累计消耗的资源
	TotalPromptTokenNum     int64
	TotalCompletionTokenNum int64
	TotalCost               float64
}

func NewSqlSession(id string, wd string) *SqlSession {
	return &SqlSession{
		Id:        id,
		WorkDir:   wd,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		history:   make([]schema.Message, 0),
		mu:        &sync.RWMutex{},
	}
}

func (s *SqlSession) Append(msgs ...schema.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.history = append(s.history, msgs...)
	s.size++
	s.UpdatedAt = time.Now()

	// todo
	// 持久化预留点：在真实的工业级实现中（如 Claude Code），
	// 我们会在这里将 s.history 以 JSONL 的格式 Append 到 workDir/.claw/sessions/xxx.jsonl 中。
	// s.SaveToDisk()
}

// GetWorkingMemory 是驾驭工程的核心
// 他不会返回全量历史，而是从后往前截取最近的 N 条消息，形成 Agent 的“短期工作记忆”
func (s *SqlSession) GetWorkingMemory(limit int) []schema.Message {
	s.mu.Lock()
	defer s.mu.Unlock()

	total := len(s.history)
	// 如果不设限或者历史总量小于限制，全量返回 (需要深拷贝以防外部修改)
	if limit == 0 || total <= limit {
		res := make([]schema.Message, total)
		copy(res, s.history)
		return res
	}

	// 截取最近的 limit 条消息
	res := make([]schema.Message, limit)
	copy(res, s.history[total-limit:])

	// 【驾驭防线】：大模型 API 强制要求历史消息的连续性！
	// 如果我们截断的第一条消息恰好是一个 ToolResult (RoleUser 且含有 ToolCallID)，
	// 但发出这个请求的 ToolCall 被我们截断抛弃了，大模型 API 会直接报 400 Bad Request。
	// 因此，如果切片首条属于“孤儿”工具响应，我们必须将其强行舍弃，顺延到下一条正常的 User/Assistant 消息。
	for len(res) > 0 {
		if res[0].Role == schema.RoleUser && res[0].ToolCallId != "" {
			res = res[1:]
		} else {
			break
		}
	}
	return res
}

func (s *SqlSession) RecordUsage(promptNum int64, completionNum int64, cost float64) {
	s.mu.Lock()
	s.TotalPromptTokenNum += promptNum
	s.TotalCompletionTokenNum += completionNum
	s.TotalCost += cost
	s.mu.Unlock()
}

func (s *SqlSession) Len() int {
	return s.size
}
