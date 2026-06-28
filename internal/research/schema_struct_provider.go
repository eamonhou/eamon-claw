package research

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
)

type SchemaStructProvider interface {
	EstimateRowsPerPage(tableName string) int64
}

// InnodbCompactSchemaAnalyzer 表结构物理尺寸分析器
type InnodbCompactSchemaAnalyzer struct {
	DB *sql.DB
}

func NewInnodbCompactSchemaAnalyzer(db *sql.DB) *InnodbCompactSchemaAnalyzer {
	return &InnodbCompactSchemaAnalyzer{DB: db}
}

// EstimateRowsPerPage 根据表结构动态、精确地估算单物理页可以存放的记录条数
func (s *InnodbCompactSchemaAnalyzer) EstimateRowsPerPage(tableName string) int64 {
	// 默认兜底值
	defaultRowsPerPage := int64(100)

	// 1. 从系统元数据表查询该表所有字段的类型信息
	query := `
		SELECT DATA_TYPE, CHARACTER_MAXIMUM_LENGTH 
		FROM information_schema.COLUMNS 
		WHERE TABLE_NAME = ? AND TABLE_SCHEMA = DATABASE()`

	rows, err := s.DB.Query(query, tableName)
	if err != nil {
		log.Printf("ℹ️  [Schema分析跳过] 无法获取表 [%s] 的元数据: %v，采用默认100条/页", tableName, err)
		return defaultRowsPerPage
	}
	defer rows.Close()

	var totalRowSize int64 = 0
	var nullFieldsCount int64 = 0

	for rows.Next() {
		var dataType string
		var maxLength sql.NullInt64
		if err := rows.Scan(&dataType, &maxLength); err != nil {
			continue
		}

		// 2. 根据 InnoDB 物理存储引擎规范，动态累加每个字段的字节数
		nullFieldsCount++ // 假定字段都允许为 NULL，用于计算变长 NULL 标志位
		switch strings.ToUpper(dataType) {
		case "TINYINT":
			totalRowSize += 1
		case "SMALLINT":
			totalRowSize += 2
		case "INT", "INTEGER":
			totalRowSize += 4
		case "BIGINT":
			totalRowSize += 8
		case "TIMESTAMP":
			totalRowSize += 4
		case "DATETIME":
			totalRowSize += 5
		case "VARCHAR", "CHAR":
			// 变长字符根据长度分配物理空间。假设使用 UTF-8mb4 字符集，平均每个字符占用 2 字节（工业均值线）
			if maxLength.Valid {
				totalRowSize += (maxLength.Int64 * 2) + 2 // 加上 2 字节变长长度列表开销
			} else {
				totalRowSize += 30 // 兜底
			}
		default:
			totalRowSize += 8 // 其余未知或复杂类型（如 TEXT 指针）按 8 字节兜底
		}
	}

	// 3. 加上 InnoDB 行格式的固定元数据开销 (记录头 5 字节 + NULL 掩码位)
	nullMaskSize := (nullFieldsCount + 7) / 8
	totalRowSize += 5 + nullMaskSize

	if totalRowSize == 0 {
		return defaultRowsPerPage
	}

	// 4. 计算单页记录数
	// InnoDB 16KB 页除去 Header/Trailer 剩余可用空间为 16270 字节
	rowsPerPage := 16270 / totalRowSize

	// 限制物理边界：一行至少在本地占空间，一页至少存 2 条（InnoDB硬性规定），至多存几千条
	if rowsPerPage < 2 {
		rowsPerPage = 2
	}

	fmt.Println("\n==================================================")
	fmt.Printf("📐 [LA-CPM 物理Schema规整] 表 [%s] 精确推导平均单行大小: %d 字节 | 估算单物理页容纳记录数: %d 条/页\n",
		tableName, totalRowSize, rowsPerPage)

	return rowsPerPage
}
