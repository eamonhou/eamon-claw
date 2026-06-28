package main

import (
	"database/sql"
	"fmt"
	"log"
	"math/rand"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

const (
	// 🪐 核心修改：将数据规模调整为学术与工程平衡的 100 万行物理靶场
	TotalRows = 1000000
	// 保持高吞吐的批处理大小
	BatchSize = 2000
	// 数据库连接 DSN (请根据你的 Docker 拓扑环境自适应修改物理端口与密码)
	DataSourceName = "root:rootisme@tcp(127.0.0.1:3302)/crm_data?charset=utf8mb4&parseTime=True&loc=Local"
)

func main() {
	// 1. 初始化数据库连接池句柄
	db, err := sql.Open("mysql", DataSourceName)
	if err != nil {
		log.Fatalf("❌ 数据库连接池初始化失败: %v", err)
	}
	defer db.Close()

	// 放大连接池物理边界，保障高频批量写入时的网络栈吞吐稳定性
	db.SetMaxOpenConns(100)
	db.SetMaxIdleConns(20)
	db.SetConnMaxLifetime(30 * time.Minute)

	if err := db.Ping(); err != nil {
		log.Fatalf("❌ 无法 Ping 通目标 MySQL 实例: %v", err)
	}
	log.Println("🚀 成功绑定目标 MySQL 实例，准备流式灌入 100 万行严苛倾斜数据...")

	// 2. 清理历史残余靶场数据
	_, _ = db.Exec("TRUNCATE TABLE `orders`")
	log.Println("🧹 历史靶场数据已彻底清空。")

	// 初始化随机数种子
	rand.Seed(time.Now().UnixNano())

	startTime := time.Now()
	insertedRows := 0

	// 3. 开始执行 100 万级高密批处理控制循环
	for insertedRows < TotalRows {
		currentBatchSize := BatchSize
		if TotalRows-insertedRows < BatchSize {
			currentBatchSize = TotalRows - insertedRows
		}

		// 动态流式构建多值插入批量 SQL 骨架
		valueStrings := make([]string, 0, currentBatchSize)
		valueArgs := make([]interface{}, 0, currentBatchSize*6) // 每行 6 个参数

		for i := 0; i < currentBatchSize; i++ {
			valueStrings = append(valueStrings, "(?, ?, ?, ?, ?, ?)")

			// 依据论文非均匀概率密度函数，控制状态字面量加权分布
			status := "COMPLETED"
			prob := rand.Intn(100) + 1
			if prob <= 3 {
				// 3% 稀疏概率均摊给三个小热点状态（PROCESSING, REFUNDED, CANCELLED）
				sparseProb := rand.Intn(3)
				switch sparseProb {
				case 0:
					status = "PROCESSING"
				case 1:
					status = "REFUNDED"
				case 2:
					status = "CANCELLED"
				}
			}

			// 增长唯一标识码，引入高频自增后缀，彻底绝杀长周期循环下的唯一键碰撞
			orderSn := fmt.Sprintf("SN%d%04d%08d", time.Now().Unix(), rand.Intn(10000), insertedRows+i)
			userId := rand.Int63n(9000000) + 1000000
			totalAmount := rand.Float64()*1000 + 10.0

			// 时间跨度向前滚动 30 Days，用作后续长跨度范围谓词算子分析
			daysAgo := rand.Intn(30)
			createTime := time.Now().AddDate(0, 0, -daysAgo)
			updateTime := createTime.Add(time.Duration(rand.Intn(3600)) * time.Second)

			valueArgs = append(valueArgs, orderSn)
			valueArgs = append(valueArgs, userId)
			valueArgs = append(valueArgs, totalAmount)
			valueArgs = append(valueArgs, status)
			valueArgs = append(valueArgs, createTime)
			valueArgs = append(valueArgs, updateTime)
		}

		// 组装最终的多值高吞吐 Insert 语句
		stmtStr := fmt.Sprintf("INSERT INTO `orders` (`order_sn`, `user_id`, `total_amount`, `order_status`, `create_time`, `update_time`) VALUES %s",
			strings.Join(valueStrings, ","))

		// 执行批量高并发写入
		_, err := db.Exec(stmtStr, valueArgs...)
		if err != nil {
			log.Fatalf("❌ 批处理写入发生破坏性中断，当前进度 %d: %v", insertedRows, err)
		}

		insertedRows += currentBatchSize

		// 🪐 每 10 万行打印一次进度，平滑控制台输出并减少 I/O 损耗
		if insertedRows%100000 == 0 {
			log.Printf("⏳ 感知流水线已无损灌入 %d 行数据... (已完成 %.1f%%)", insertedRows, float64(insertedRows)/TotalRows*100)
		}
	}

	log.Printf("✨ [数据制造成功] 成功向本地沙箱注入 %d 行高倾斜订单数据！总计耗时: %v\n", TotalRows, time.Since(startTime))

	// 4. 发起确定性合规校验，印证论文 97% 学术红线
	var completedCount int
	err = db.QueryRow("SELECT COUNT(*) FROM `orders` WHERE `order_status` = 'COMPLETED'").Scan(&completedCount)
	if err == nil {
		actualRatio := float64(completedCount) / float64(TotalRows) * 100.0
		log.Printf("📊 [学术指标合规检查] 已完成状态 (COMPLETED) 实际总数: %d 行, 绝对物理占比: %.2f%% (预期突破 97%%)\n", completedCount, actualRatio)
	}
}
