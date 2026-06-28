package research

import (
	"database/sql"
	"fmt"
)

type SchemaData struct {
	TotalRows  int64   //表总行数
	TotalPages int64   //预估总数据页
	MatchRows  int64   //条件命中行数
	Ratio      float64 //数据占比
}

type SchemaDataProvider interface {
	GetSchemaData(payload *SlowQueryPayload) (*SchemaData, error)
}

type SimpleSchemaDataProvider struct {
	db                   *sql.DB // 绑定的隔离读库连接池
	schemaStructProvider SchemaStructProvider
}

func NewSimpleSchemaDataProvider(db *sql.DB, schemaStructProvider SchemaStructProvider) *SimpleSchemaDataProvider {
	return &SimpleSchemaDataProvider{
		db:                   db,
		schemaStructProvider: schemaStructProvider,
	}
}

func (p *SimpleSchemaDataProvider) GetSchemaData(payload *SlowQueryPayload) (*SchemaData, error) {
	// 1. 从隔离读库中动态查询该表的总行数 (TotalRows)
	var totalRows int64
	err := p.db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM `%s`", payload.TableName)).Scan(&totalRows)
	if err != nil || totalRows == 0 {
		return nil, fmt.Errorf("读库统计失败，表 [%s] 可能不存在或无数据: %w", payload.TableName, err)
	}

	// 利用 SchemaStructProvider 代替写死的 100
	rowsPerPage := p.schemaStructProvider.EstimateRowsPerPage(payload.TableName)

	// 粗略预估物理页数 (InnoDB 默认 16KB 一页，这里假设每页平均存 100 条记录)
	totalPages := totalRows / rowsPerPage
	if totalPages == 0 {
		totalPages = 1
	}

	// 2. 根据不同的全谱系操作符，自适应动态组装“特征压榨 SQL”，计算当前慢查询条件覆盖的数据占比 (Ratio)
	var countSQL string
	switch payload.Operator {
	case "=":
		countSQL = fmt.Sprintf("SELECT COUNT(*) FROM `%s` WHERE `%s` = '%s'", payload.TableName, payload.ColumnName, payload.CurrentVal)
	case "LIKE":
		countSQL = fmt.Sprintf("SELECT COUNT(*) FROM `%s` WHERE `%s` LIKE '%s'", payload.TableName, payload.ColumnName, payload.CurrentVal)
	case "BETWEEN":
		countSQL = fmt.Sprintf("SELECT COUNT(*) FROM `%s` WHERE `%s` BETWEEN %s", payload.TableName, payload.ColumnName, payload.CurrentVal)
	case "IN":
		countSQL = fmt.Sprintf("SELECT COUNT(*) FROM `%s` WHERE `%s` IN (%s)", payload.TableName, payload.ColumnName, payload.CurrentVal)
	default:
		countSQL = fmt.Sprintf("SELECT COUNT(*) FROM `%s` WHERE `%s` = '%s'", payload.TableName, payload.ColumnName, payload.CurrentVal)
	}

	var matchRows int64
	err = p.db.QueryRow(countSQL).Scan(&matchRows)
	if err != nil {
		return nil, fmt.Errorf("执行特征压榨 SQL 失败: %w", err)
	}

	// 计算非参数化大样本占比
	ratio := float64(matchRows) / float64(totalRows)
	return &SchemaData{
		TotalRows:  totalRows,
		TotalPages: totalPages,
		MatchRows:  matchRows,
		Ratio:      ratio,
	}, nil
}
