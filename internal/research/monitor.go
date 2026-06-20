package research

import (
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/hpcloud/tail"
)

type SlowQueryPayload struct {
	RawSQL     string
	TableName  string
	ColumnName string
	Operator   string
	CurrentVal string
}

type SlowLogMonitor struct {
	LogFilePath string
	OutChannel  chan *SlowQueryPayload

	// 引入并发锁与流式缓冲区，解决多行异步落盘冲突
	mu        sync.Mutex
	sqlBuffer strings.Builder
	lastPush  time.Time
}

func NewSlowLogMonitor(filePath string) *SlowLogMonitor {
	return &SlowLogMonitor{
		LogFilePath: filePath,
		OutChannel:  make(chan *SlowQueryPayload, 100),
		lastPush:    time.Now(),
	}
}

func (m *SlowLogMonitor) StartWatch() error {
	config := tail.Config{
		Follow: true,
		ReOpen: true,
	}

	t, err := tail.TailFile(m.LogFilePath, config)
	if err != nil {
		return err
	}

	// 🪐 核心升级一：开启专属守护协程，防止 SQL 憋在 Buffer 里
	// 每隔 200ms 检查一次，如果距离上次写日志已经过去 500ms，说明 SQL 已落盘完毕，立刻强制冲刷
	go func() {
		for {
			time.Sleep(200 * time.Millisecond)
			m.mu.Lock()
			if m.sqlBuffer.Len() > 0 && time.Since(m.lastPush) > 500*time.Millisecond {
				m.flushBuffer()
			}
			m.mu.Unlock()
		}
	}()

	// 核心升级二：高可靠日志流式解析
	go func() {
		for line := range t.Lines {
			text := strings.TrimSpace(line.Text)
			if text == "" {
				continue
			}

			m.mu.Lock()
			// 1. 如果遇到元数据行，且当前 Buffer 里已经积累了完整的 SQL，先触发一次冲刷
			if strings.HasPrefix(text, "#") {
				if m.sqlBuffer.Len() > 0 {
					m.flushBuffer()
				}
				m.mu.Unlock()
				continue
			}

			// 2. 过滤无用指令
			if strings.HasPrefix(strings.ToUpper(text), "SET") || strings.HasPrefix(strings.ToUpper(text), "USE") {
				m.mu.Unlock()
				continue
			}

			// 3. 安全拼接 SQL，更新时间戳
			m.sqlBuffer.WriteString(text)
			m.sqlBuffer.WriteString(" ")
			m.lastPush = time.Now()
			m.mu.Unlock()
		}
	}()

	return nil
}

// flushBuffer 专门负责清空缓冲区并将纯净的特征 Payload 推入 Channel
func (m *SlowLogMonitor) flushBuffer() {
	rawSQL := strings.TrimSuffix(m.sqlBuffer.String(), " ")
	rawSQL = strings.TrimSuffix(rawSQL, ";")
	m.sqlBuffer.Reset() // 立刻彻底清空，防止下一次重复消费

	if payload, err := m.parseSlowSQL(rawSQL); err == nil {
		// 非阻塞发送，防止 Channel 满导致死锁
		select {
		case m.OutChannel <- payload:
		default:
			log.Println("⚠️  [Channel 溢出] 消费速度过慢，抛弃当前慢查询特征")
		}
	} else {
		// 记录非目标 SQL（例如没有 WHERE 条件的普通全表查），不往通道送
		log.Printf("📝 [感知放行] 慢SQL不含特定谓词结构，跳过 LA-CPM: %s", rawSQL)
	}
}

// parseSlowSQL 保持你的全谱系解析引擎原样不变
func (m *SlowLogMonitor) parseSlowSQL(rawSQL string) (*SlowQueryPayload, error) {
	cleanSQL := strings.ToLower(strings.TrimSpace(rawSQL))

	reTable := regexp.MustCompile(`(?:from|update|join)\s+\x60?([a-z0-9_]+)\x60?`)
	tableMatches := reTable.FindStringSubmatch(cleanSQL)
	tableName := "unknown_table"
	if len(tableMatches) > 1 {
		tableName = tableMatches[1]
	}

	// 1. BETWEEN
	reBetween := regexp.MustCompile(`where\s+([a-z0-9_]+)\s+between\s+['"]?([^'"]+)['"]?\s+and\s+['"]?([^'"]+)['"]?`)
	if matches := reBetween.FindStringSubmatch(cleanSQL); len(matches) > 3 {
		return &SlowQueryPayload{RawSQL: rawSQL, TableName: tableName, ColumnName: matches[1], Operator: "BETWEEN", CurrentVal: fmt.Sprintf("%s AND %s", matches[2], matches[3])}, nil
	}

	// 2. IN
	reIn := regexp.MustCompile(`where\s+([a-z0-9_]+)\s+in\s*\(([^)]+)\)`)
	if matches := reIn.FindStringSubmatch(cleanSQL); len(matches) > 2 {
		cleanedIn := strings.ReplaceAll(strings.ReplaceAll(matches[2], "'", ""), `"`, "")
		return &SlowQueryPayload{RawSQL: rawSQL, TableName: tableName, ColumnName: matches[1], Operator: "IN", CurrentVal: cleanedIn}, nil
	}

	// 3. LIKE
	reLike := regexp.MustCompile(`where\s+([a-z0-9_]+)\s+like\s+['"]([^'"]+)['"]`)
	if matches := reLike.FindStringSubmatch(cleanSQL); len(matches) > 2 {
		return &SlowQueryPayload{RawSQL: rawSQL, TableName: tableName, ColumnName: matches[1], Operator: "LIKE", CurrentVal: matches[2]}, nil
	}

	// 4. 经典等号 =
	reEq := regexp.MustCompile(`where\s+([a-z0-9_]+)\s*=\s*['"]?([^'"]+)['"]?`)
	if matches := reEq.FindStringSubmatch(cleanSQL); len(matches) > 2 {
		return &SlowQueryPayload{RawSQL: rawSQL, TableName: tableName, ColumnName: matches[1], Operator: "=", CurrentVal: matches[2]}, nil
	}

	return nil, fmt.Errorf("sql未包含主流谓词")
}
