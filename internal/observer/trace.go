package observer

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// 注意：为了防止不同包（Package）之间的 Key 发生冲突，
// Go 官方强烈建议：使用context的Value相关函时不要直接使用 string 或基础类型作为 Key，
// 而是自定义一个未导出的结构体类型。

// TraceKey 是 Context 中存放 Span 的专属 Key，防止冲突
// 确保除了当前包以外，外部任何人都无法通过普通的字符串（比如 "trace"）覆盖或
// 篡改 Context 里存放的链路数据，保证了类型安全。
type TraceKey struct{}

// Span 代表链路追踪中的一个时间跨度和操作节点
type Span struct {
	Name       string    `json:"name"`
	StartTime  time.Time `json:"start_time"`
	EndTime    time.Time `json:"end_time"`
	DurationMs int64     `json:"duration"`
	// 存放元数据 (如消耗的 Token, 执行的命令)
	Attributes map[string]interface{} `json:"attributes,omitempty"`
	// 子跨度
	Children []*Span `json:"children"`

	mu sync.Mutex
}

func StartSpan(ctx context.Context, name string) (context.Context, *Span) {
	newSpan := &Span{
		Name:       name,
		StartTime:  time.Now(),
		Attributes: make(map[string]interface{}),
	}

	// 从 context 中尝试获取父 Span
	// ctx.Value(traceKey{})： 这是对 context.Value() 的经典应用。
	// 它顺着 Context 树往上找，看看当前链路上是否已经有一个正在执行的 Span。
	if parent, ok := ctx.Value(TraceKey{}).(*Span); ok {
		parent.mu.Lock()
		parent.Children = append(parent.Children, newSpan)
		parent.mu.Unlock()
	}
	newCtx := context.WithValue(ctx, TraceKey{}, newSpan)
	return newCtx, newSpan
}

// EndSpan 结束跨度，计算耗时
func (s *Span) EndSpan() {
	s.EndTime = time.Now()
	s.DurationMs = s.EndTime.Sub(s.StartTime).Milliseconds()
}

// AddAttribute 为当前 Span 记录关键的元数据
func (s *Span) AddAttribute(key string, value interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Attributes[key] = value
}

// ExportTraceToFile 当整个根 Span 结束时，将其序列化并保存为本地 JSON 文件
func ExportTraceToFile(rootSpan *Span, workDir string, sessionId string) error {
	dirPath := filepath.Join(workDir, ".claw", "traces")
	os.Mkdir(dirPath, 0755)

	filename := filepath.Join(dirPath, fmt.Sprintf("trace_%s_%d.json", sessionId, time.Now().Unix()))
	data, err := json.MarshalIndent(rootSpan, "", "\t")
	if err != nil {
		return fmt.Errorf("span 序列化失败：%w", err)
	}

	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("span 序列化数据写入文件失败：%w", err)
	}
	return nil
}
