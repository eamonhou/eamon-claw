package research

import (
	"database/sql"
	"fmt"
	"log"
)

// CostCalculator 动态物理代价预测器
type CostCalculator struct {
	DB *sql.DB // 绑定的隔离读库连接池
}

// NewCostCalculator 构造函数
func NewCostCalculator(db *sql.DB) *CostCalculator {
	return &CostCalculator{DB: db}
}

// CalculateDynamicCost 核心函数：联动读库与环境负载，动态计算并打印物理代价得分
//
//	@param payload
//	@param factors
//	@return float64 计算非参数化大样本占比
//	@return float64 走全扫描耗时
//	@return float64 走二级索引耗时
func (c *CostCalculator) CalculateDynamicCost(payload *SlowQueryPayload, factors SystemLoadFactors) (float64, float64, float64) {
	// 1. 从隔离读库中动态查询该表的总行数 (TotalRows)
	var totalRows int64
	err := c.DB.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM `%s`", payload.TableName)).Scan(&totalRows)
	if err != nil || totalRows == 0 {
		log.Printf("❌ 读库统计失败，表 [%s] 可能不存在或无数据: %v\n", payload.TableName, err)
		return 0, 0, 0
	}

	// 利用 SchemaAnalyzer 代替写死的 100
	analyzer := NewSchemaAnalyzer(c.DB)
	rowsPerPage := analyzer.EstimateRowsPerPage(payload.TableName)

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
	err = c.DB.QueryRow(countSQL).Scan(&matchRows)
	if err != nil {
		log.Printf("❌ 执行特征压榨 SQL 失败: %v\n", err)
		return 0, 0, 0
	}

	// 计算非参数化大样本占比
	ratio := float64(matchRows) / float64(totalRows)

	// 3. 动态代价模型（LA-CPM）核心常数修正
	dynamicReadCost := 1.0 * factors.Beta  // 磁盘 I/O 常数（受环境阻尼因子修正）
	dynamicEvalCost := 0.2 * factors.Alpha // CPU 评估常数（受环境扰动因子修正）

	// 路径 A 代价：全表扫描
	costFullScan := float64(totalPages)*dynamicReadCost + float64(totalRows)*dynamicEvalCost

	// 路径 B 代价：走二级索引并物理回表
	predictedRows := float64(totalRows) * ratio
	costIndexScan := predictedRows*dynamicReadCost + predictedRows*dynamicEvalCost

	// 4. 控制台白盒化输出（完美对应你刚想到的“人在回路与可视化”竞争力）
	fmt.Printf("\n==================================================\n")
	fmt.Printf("📊 【LA-CPM 物理代价计算报告】\n")
	fmt.Printf("   📁 目标数据表: %s | 触发特征列: %s (%s)\n", payload.TableName, payload.ColumnName, payload.Operator)
	fmt.Printf("   📈 表总行数: %d | 预估总数据页: %d\n", totalRows, totalPages)
	fmt.Printf("   🎯 条件命中行数: %d | 数据占比 (Ratio): %.2f%%\n", matchRows, ratio*100)
	fmt.Printf("   🖥️  预估全表扫描代价 (Cost Full): %.2f\n", costFullScan)
	fmt.Printf("   🔍 预估索引回表代价 (Cost Index): %.2f\n", costIndexScan)

	if costIndexScan > costFullScan {
		multipler := costIndexScan / costFullScan
		fmt.Printf("   ⚠️  【决策判定】: 走索引代价是全表的 %.1fx！常规索引在此刻失效！\n", multipler)
	} else {
		fmt.Printf("   ✅ 【决策判定】: 走索引更划算，属于常规缺失索引导致的慢查询。\n")
	}
	fmt.Printf("==================================================\n")

	return ratio, costFullScan, costIndexScan
}
