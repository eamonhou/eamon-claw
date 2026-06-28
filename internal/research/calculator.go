package research

import (
	"encoding/json"
	"fmt"
)

// CalculatorResult 定义交付给上层 Agent 层大模型认知大脑的白盒化结构账单契约
type CalculatorResult struct {
	AlphaSteady        float64 `json:"alpha_steady"`         // EWMA 平滑后的稳态 CPU 扰动因子 (α)
	BetaSteady         float64 `json:"beta_steady"`          // EWMA 平滑后的稳态 I/O 阻尼因子 (β)
	CostFullScan       float64 `json:"cost_full_scan"`       // 全表顺序扫描物理总代价
	CostIndexScan      float64 `json:"cost_index_scan"`      // 二级索引回表随机 I/O 惩罚总代价
	CostRatio          float64 `json:"cost_ratio"`           // 代价放大倍数 (CostIndexScan / CostFullScan)
	ShouldResetSession bool    `json:"should_reset_session"` // 核心判定：传统 CBO 优化器是否在此刻物理崩溃
	Explanation        string  `json:"explanation"`          // 提供给大模型的白盒化成因定性解释
}

type LaCpmCalculator struct {
	// 优化器运作常数（对齐 MySQL 8.0 默认成本模型规范）
	ioBlockReadCost float64 // 顺序数据页 I/O 成本常数（默认 1.0）
	ioRandomCost    float64 // 随机回表 I/O 惩罚常数（默认 4.0）
	cpuRowCost      float64 // CPU 内存行读取与比较成本（默认 0.1）
	cpuIndexCost    float64 // 二级索引内部页查找成本（默认 0.1）
	ewmaWeight      float64 // EWMA 滑动窗口平滑自适应权重（λ，标准推荐 0.25）
}

func NewLaCpmCalculator() *LaCpmCalculator {
	return &LaCpmCalculator{
		ioBlockReadCost: 1.0,
		ioRandomCost:    4.0,
		cpuRowCost:      0.1,
		cpuIndexCost:    0.1,
		ewmaWeight:      0.25, // 权重越大，对最新的物理尖峰越敏感
	}
}

// Calculate 落地 LA-CPM 无状态演算核心，将多模态快照提炼为工业级确定性账单
func (c *LaCpmCalculator) Calculate(systemLoadData *AnalysisSnapshot, schemaData *SchemaData) string {
	// 0. 防御性边界检查：如果上游数据层或分析层发生断流，输出故障降级说明
	if systemLoadData == nil || schemaData == nil {
		return `{"error": "LA-CPM 演算失败：上游特征数据层或隔离库分析层响应为空"}`
	}

	// 1. 核心数理管道一：对双向时间窗高密时序列表执行 EWMA 自平滑演进
	alphaSteady := c.computeEwma(systemLoadData.AlphaTimeline)
	betaSteady := c.computeEwma(systemLoadData.BetaTimeline)

	// 2. 核心数理管道二：结合隔离库反褶积几何特征，开展多路径真实物理代价方程演算
	// 路径 A：全表顺序扫描物理代价 (Cost_FullScan)
	// 公式：数据总页数 * 顺序IO成本 + 表总行数 * CPU行成本 * α扰动因子
	costFullScan := (float64(schemaData.TotalPages) * c.ioBlockReadCost) +
		(float64(schemaData.TotalRows) * c.cpuRowCost * alphaSteady)

	// 路径 B：二级索引回表随机 I/O 惩罚代价 (Cost_IndexScan)
	// 公式：条件命中行数 * CPU索引成本 + 条件命中行数 * 随机IO成本 * β阻尼因子
	costIndexScan := (float64(schemaData.MatchRows) * c.cpuIndexCost) +
		(float64(schemaData.MatchRows) * c.ioRandomCost * betaSteady)

	// 3. 核心数理管道三：代价临界点级数判定与欧几里得阶跃裁决
	costRatio := 0.0
	if costFullScan > 0 {
		costRatio = costIndexScan / costFullScan
	}

	// 4. 为上层大模型（Agent层）编写白盒化成因定性解释
	var explanation string
	if costIndexScan > costFullScan {
		explanation = fmt.Sprintf("物理墙遭遇非线性塌方！当前宿主机存在严重 I/O 阻尼 (β:%.2f)，且目标字段数据选择性崩溃(占比达%.1f%%)。二级索引回表产生的随机IO惩罚已呈几何级数放大，真实物理总代价达到全表扫描的 %.2f 倍。传统静态CBO优化器已彻底致盲，引发错误的执行计划选型。建议拦截该字面量泛化并追加外部重置指令。",
			betaSteady, schemaData.Ratio*100, costRatio)
	} else {
		explanation = fmt.Sprintf("物理环境稳态收敛。当前系统负载处于安全基线 (α:%.2f, β:%.2f)，该字段命中行数 %d 处于收敛区间，二级索引物理执行路径综合评分优于全表扫描。",
			alphaSteady, betaSteady, schemaData.MatchRows)
	}

	// 5. 将计算对象高密封装为确定性的 DTO JSON 契约
	resultObj := CalculatorResult{
		AlphaSteady:   alphaSteady,
		BetaSteady:    betaSteady,
		CostFullScan:  costFullScan,
		CostIndexScan: costIndexScan,
		CostRatio:     costRatio,
		Explanation:   explanation,
	}

	jsonBytes, err := json.MarshalIndent(resultObj, "", "  ")
	if err != nil {
		return fmt.Sprintf(`{"error": "JSON 契约序列化失败: %v"}`, err)
	}

	return string(jsonBytes)
}

// computeEwma 指数加权移动平均滤波算子核心实现
func (c *LaCpmCalculator) computeEwma(timeline []MetricPoint) float64 {
	if len(timeline) == 0 {
		return 0.0 // 降级兜底值
	}

	// 以时序列表中的第一个采样点作为基准初始状态值
	steadyValue := timeline[0].Value

	// 沿着你设计的双向因果时间流，在内存中原地模拟时序推进
	for i := 1; i < len(timeline); i++ {
		// EWMA 递推递推方程：S_t = λ * Y_t + (1 - λ) * S_{t-1}
		steadyValue = c.ewmaWeight*timeline[i].Value + (1.0-c.ewmaWeight)*steadyValue
	}

	return steadyValue
}
