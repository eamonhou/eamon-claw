package main

import (
	"database/sql"
	"eamon-claw/internal/research"
	"fmt"
	"log"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

func main() {
	slowLogProvider := research.NewSimpleSlowLogProvider()
	slowLogPayload, err := slowLogProvider.FetchNextPayload()
	if err != nil {
		log.Fatalf("❌ 获取慢查询日志失败: %v", err)
	}

	// 1. 初始化连接本地 Docker 中的 Prometheus
	sysLoaderProvider, err := research.NewPromDataLoader("http://127.0.0.1:9090")
	if err != nil {
		log.Fatalf("❌ 数据感知层适配器初始化失败: %v", err)
	}

	// 2. 模拟一条慢 SQL 的物理执行生命周期（假设跑了 3 秒，刚刚结束落盘）
	sqlEndTime := slowLogPayload.StartTime.Add(slowLogPayload.QueryTime)
	sqlStartTime := slowLogPayload.StartTime

	// 3. 调度你的双向因果时间窗契约接口，向两侧各拉开 10 秒的微观观测纵深
	systemLoadData, err := sysLoaderProvider.FetchLoadTimeline(sqlStartTime, sqlEndTime, 10*time.Second)
	if err != nil {
		log.Fatalf("❌ 双向契约数据拉取失败: %v", err)
	}

	dsn := "root:rootisme@tcp(127.0.0.1:3302)/crm_data"
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Fatalf("❌ 无法建立数据库连接池: %v", err)
	}
	defer db.Close()

	schemaStructProvider := research.NewInnodbCompactSchemaAnalyzer(db)

	schemaDataProvider := research.NewSimpleSchemaDataProvider(db, schemaStructProvider)
	schemaData, _ := schemaDataProvider.GetSchemaData(slowLogPayload)

	calculator := research.NewLaCpmCalculator()
	result := calculator.Calculate(systemLoadData, schemaData)

	fmt.Printf("\n✅ LA-CPM计算结果：%s\n", result)
}
