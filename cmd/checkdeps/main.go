// Command checkdeps 检查 config.toml 中各远端依赖是否可达，便于本地启动前排查。
package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	"GopherAI/config"

	_ "github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
)

func main() {
	conf := config.GetConfig()
	if conf == nil {
		fmt.Println("FAIL  加载 config/config.toml 失败")
		os.Exit(1)
	}
	fmt.Println("OK    加载 config/config.toml")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// MySQL：先不指定库名，确认账号可登录并列出可见数据库。
	mysqlDSN := fmt.Sprintf("%s:%s@tcp(%s:%d)/?charset=%s&parseTime=true&loc=Local",
		conf.MysqlUser, conf.MysqlPassword, conf.MysqlHost, conf.MysqlPort, conf.MysqlCharset)
	db, err := sql.Open("mysql", mysqlDSN)
	if err != nil {
		fail("MySQL 连接", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		fail("MySQL Ping", err)
	}
	fmt.Println("OK    MySQL 账号可登录")

	rows, err := db.QueryContext(ctx, "SHOW DATABASES")
	if err != nil {
		fail("MySQL SHOW DATABASES", err)
	}
	defer rows.Close()

	var dbs []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			fail("MySQL 读取库名", err)
		}
		dbs = append(dbs, name)
	}
	fmt.Printf("INFO  可见数据库: %v\n", dbs)

	targetDB := conf.MysqlDatabaseName
	hasTarget := false
	for _, name := range dbs {
		if name == targetDB {
			hasTarget = true
			break
		}
	}
	if !hasTarget {
		fmt.Printf("FAIL  配置库 %q 不在可见列表中，请远端创建并授权，或修改 config.toml 的 databaseName\n", targetDB)
		os.Exit(1)
	}

	// 再测目标库是否可进入。
	targetDSN := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=true&loc=Local",
		conf.MysqlUser, conf.MysqlPassword, conf.MysqlHost, conf.MysqlPort, targetDB, conf.MysqlCharset)
	target, err := sql.Open("mysql", targetDSN)
	if err != nil {
		fail("MySQL 目标库连接", err)
	}
	defer target.Close()
	if err := target.PingContext(ctx); err != nil {
		fail(fmt.Sprintf("MySQL 库 %q 访问", targetDB), err)
	}
	fmt.Printf("OK    MySQL 库 %q 可访问\n", targetDB)

	// Redis
	rdb := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", conf.RedisHost, conf.RedisPort),
		Password: conf.RedisPassword,
		DB:       conf.RedisDb,
	})
	defer rdb.Close()
	if err := rdb.Ping(ctx).Err(); err != nil {
		fail("Redis Ping", err)
	}
	fmt.Println("OK    Redis 可连接")

	fmt.Println("\n全部依赖检查通过，可启动 go run ./cmd/server 或 F5 调试后端。")
}

func fail(step string, err error) {
	fmt.Printf("FAIL  %s: %v\n", step, err)
	os.Exit(1)
}
