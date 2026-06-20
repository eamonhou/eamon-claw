package research

import (
	"runtime"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/load"
)

// SystemLoadFactors 物理代价模型的环境动态权重
type SystemLoadFactors struct {
	Alpha float64 // CPU 扰动因子
	Beta  float64 // I/O 阻尼因子
}

// LoadDetector 系统负载特征感知探测器
type LoadDetector struct{}

// NewLocalDetector 本地探测器构造函数
func NewLocalDetector() *LoadDetector {
	return &LoadDetector{}
}

// Detect 获取当前时刻本地自适应修正后的物理代价因子 (Alpha 与 Beta 完整版)
func (d *LoadDetector) Detect() SystemLoadFactors {
	factors := SystemLoadFactors{Alpha: 1.0, Beta: 1.0}

	// 1. 动态抽取 CPU 物理特征 -> 计算 Alpha
	percent, err := cpu.Percent(time.Millisecond*100, false)
	if err == nil && len(percent) > 0 {
		cpuUsage := percent[0] / 100.0
		// 如果本地 CPU 使用率超过 70%，线性放大 CPU 评估代价
		if cpuUsage > 0.7 {
			factors.Alpha = 1.0 + (cpuUsage-0.7)*2.0
		}
	}

	// 2. 动态抽取系统整体负载 (包含I/O排队) -> 计算 Beta
	// 获取当前本机的 CPU 物理核心数 (如 4核、8核)
	numCPU := float64(runtime.NumCPU())

	avg, err := load.Avg()
	if err == nil {
		// Linux/Mac 下，如果 1 分钟内的 Load Average 超过了物理核心数
		// 意味着大量进程在排队等待 CPU 或处于不可中断的磁盘 I/O 等待 (IO Wait)
		if avg.Load1 > numCPU {
			// 计算超载比例，动态作为 I/O 阻尼因子 Beta 的放大系数
			overloadRatio := avg.Load1 / numCPU
			// 设定阻尼上限，防止极端情况下数值过大导致公式失真
			if overloadRatio > 3.0 {
				overloadRatio = 3.0
			}
			factors.Beta = overloadRatio
		}
	}

	return factors
}
