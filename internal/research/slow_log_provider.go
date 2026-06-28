package research

import "time"

type SlowLogProvider interface {
	// FetchNextPayload 流式获取下一条待分析的慢查询纯净语义实体
	FetchNextPayload() (*SlowQueryPayload, error)
}

type SlowQueryPayload struct {
	StartTime time.Time
	QueryTime time.Duration

	RawSql     string
	TableName  string
	ColumnName string
	Operator   string
	CurrentVal string

	SqlHashId string
}

type SimpleSlowLogProvider struct {
}

func NewSimpleSlowLogProvider() *SimpleSlowLogProvider {
	return &SimpleSlowLogProvider{}
}

func (p *SimpleSlowLogProvider) FetchNextPayload() (*SlowQueryPayload, error) {
	return &SlowQueryPayload{
		StartTime:  time.Now(),
		QueryTime:  time.Second,
		RawSql:     `select * from orders where order_status="PROCESSING"`,
		TableName:  "orders",
		ColumnName: "order_status",
		Operator:   "=",
		CurrentVal: "PROCESSING",
		SqlHashId:  "dadasdasdsa",
	}, nil
}
