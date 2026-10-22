# 需求工单：v16 Schema 与启用边界

第一版仅开放平台主站（tenant 0）。v16 只新增 `request_tickets`、
`request_ticket_events` 两张表及四个查询索引；不增加附件表，不改写用户、
余额、通知或代理站数据。事件表同时保存不可变的状态时间线与站内通知记录，
已读时间是事件唯一可变的字段。

状态由业务层限制为：`pending → accepted → scheduling → developing → completed`，
或 `pending → rejected`。状态更新、反馈写入与新通知在一个业务事务内提交。

## 运行时兼容

本次未带 `kkai_bridge` 的运行时合同是 `(min=14, max=16, target=14)`。
它可运行于完整 canonical v14、v15、v16，启动不会执行 v15/v16 DDL。
`kkai_bridge` 的 `(11,14,11)` 合同保持不变，不能部署到 v15/v16 数据库。
v1–v15 的迁移定义、checksum 与 prefix digest 均保持原值；v16 canonical
ledger 延续在线租户路径，排除已退役的 v12/v13。

只有 canonical v16 receipt、完整历史 ledger、租户在线迁移 receipt、两张表的
字段类型/长度/非空约束、主键与四个索引共同校验通过，工单接口才启用。
仅手工建表或 GORM AutoMigrate 不会启用功能。完整 canonical v14/v15 的应用
可照常启动，工单/工单动态接口返回 HTTP 503，code 为
`REQUEST_TICKET_SCHEMA_NOT_READY`。缺少 ledger 的空库或 v16 物理结构漂移
会被应用原有的全局 `CheckRequired` 拒绝启动；不能把缺少 ledger 描述成仅工单
返回 503。本次未提高启动所需的最低版本。
gate 在 router 初始化时冻结；完成迁移后需要重启/替换服务进程。

## 本地开发：从全新 SQLite 文件启用

空库需要先显式 bootstrap，不能直接运行应用来生成 canonical receipt。
下面的隔离示例复用现有公开迁移库入口，先创建历史基础表，依次建立
`1..11,14,15,16`，再使用现有 `model.InitDB` / `model.InitLogDB` 初始化
完整应用表。它不读取网络 DSN，只接受独占创建的全新 SQLite 文件；任何已有
路径都会拒绝，包括以前创建但未成功完成的文件。失败后保留该文件供排查，
修正原因后换一个新临时目录重试。

先将以下完整示例保存为 `/tmp/request-ticket-local-bootstrap.go`：

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/kkaimigrate"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func main() {
	if len(os.Args) != 2 {
		log.Fatal("usage: go run bootstrap.go /absolute/new-local.db")
	}
	path, err := filepath.Abs(os.Args[1])
	must(err)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	must(err) // Refuse every existing path, including a production snapshot.
	must(file.Close())
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	must(err)
	for _, statement := range []string{
		"CREATE TABLE users(id INTEGER PRIMARY KEY,telegram_id TEXT,username VARCHAR(20),email VARCHAR(50),CONSTRAINT uni_users_username UNIQUE(username))",
		"CREATE TABLE tokens(id INTEGER PRIMARY KEY)",
		"CREATE TABLE passkey_credentials(id INTEGER PRIMARY KEY)",
		"CREATE TABLE options(key VARCHAR(255) PRIMARY KEY,value TEXT)",
		"CREATE TABLE midjourneys(id INTEGER PRIMARY KEY,mj_id TEXT,channel_id BIGINT,quota BIGINT,status TEXT)",
		"CREATE TABLE top_ups(id INTEGER PRIMARY KEY)",
		"CREATE TABLE user_oauth_bindings(id INTEGER PRIMARY KEY)",
		"CREATE TABLE logs(id INTEGER PRIMARY KEY)",
		"CREATE TABLE quota_data(id INTEGER PRIMARY KEY)",
		"CREATE TABLE tasks(id INTEGER PRIMARY KEY)",
	} {
		must(db.Exec(statement).Error)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	for _, apply := range []func(context.Context, *gorm.DB, kkaimigrate.Options) (*kkaimigrate.Result, error){
		kkaimigrate.ApplyLegacyBaseline, kkaimigrate.ApplyOutboxEventKeyCompatibility,
		kkaimigrate.ApplyVideoStudioExpand, kkaimigrate.ApplyVideoSampleCategoryExpand,
		kkaimigrate.ApplyImageStudioExpand, kkaimigrate.ApplyAuthenticationExpand,
		kkaimigrate.ApplyRC41ArchitectureExpand, kkaimigrate.ApplyImageAsyncExpand,
		kkaimigrate.ApplyMembershipExpand, kkaimigrate.ApplyTenantOnlineExpansion,
		kkaimigrate.FinalizeTenantOnlineSchema, kkaimigrate.ApplyResellerApplicationExpand,
		kkaimigrate.ApplyRequestTicketExpand,
	} {
		_, err := apply(ctx, db, kkaimigrate.Options{})
		must(err)
	}
	var indexes []struct{ Name, SQL string }
	must(db.Raw("SELECT name, sql FROM sqlite_master WHERE type = 'index' AND sql IS NOT NULL").Scan(&indexes).Error)
	sqlDB, err := db.DB()
	must(err)
	must(sqlDB.Close())
	// Pin all application initialization to the new SQLite file, ignoring DSNs.
	must(os.Setenv("SQL_DSN", "local"))
	must(os.Setenv("LOG_SQL_DSN", ""))
	must(os.Setenv("KKAI_NODE_ROLE", "leader"))
	common.SQLitePath = path
	must(common.InitNodeRoleFromEnvironment())
	must(model.InitDB())
	must(model.InitLogDB())
	// Initial SQLite AutoMigrate may rebuild historical base tables. Preserve
	// the indexes just produced by the official migrations on this new file.
	for _, index := range indexes {
		var count int64
		must(model.DB.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?", index.Name).Scan(&count).Error)
		if count == 0 {
			must(model.DB.Exec(index.SQL).Error)
		}
	}
	must(kkaimigrate.CheckRequired(ctx, model.DB))
	must(kkaimigrate.CheckRequestTicketSchema(ctx, model.DB))
	must(model.ValidateMainSchemaPrerequisites(model.DB))
	must(model.ValidateLogSchemaPrerequisites(model.DB))
	fmt.Println("Local v16 database ready:", path)
}
```

从本仓库根目录显式执行，命令本身即授权对这个新建本地库进行扩展：

```sh
KKAI_LOCAL_DB_DIR="$(mktemp -d /tmp/kkai-request-dev.XXXXXX)"
go run /tmp/request-ticket-local-bootstrap.go "$KKAI_LOCAL_DB_DIR/request-tickets.db"
SQL_DSN=local LOG_SQL_DSN= SQLITE_PATH="$KKAI_LOCAL_DB_DIR/request-tickets.db" \
  TENANT_ROUTING_ENABLED=false TENANT_PLATFORM_HOSTS=localhost \
  KKAI_NODE_ROLE=leader DISABLE_BACKGROUND_TASKS=true go run .
```

示例在首次 GORM 补齐历史基础表时保留官方迁移生成的索引，随后同时执行
`CheckRequired`、`CheckRequestTicketSchema`、主库/日志 prerequisites 校验；
任何形状不匹配都会停止。通过 `http://localhost:3000` 访问；Host 配置只接受
域名，不能加入 `127.0.0.1` 这样的 IP 字面量。这个索引保留步骤只属于全新本地库的 bootstrap，
不能拿来修复已有数据库或生产漂移。应用启动后仍按正常初始化页面创建账号。

若已有**经过独立确认的隔离 canonical v14/v15 开发库**，不必重建：停止该库
的应用写入，使用本节相同的库入口依次调用缺少的
`ApplyResellerApplicationExpand`（v14→v15）和 `ApplyRequestTicketExpand`
（v15→v16），再运行以上校验并重启应用。调用者需自行明确授权并校验开发库
身份；不要修改全新库示例的 `O_EXCL` 保护来接受已有路径。没有 canonical
ledger 的旧开发库不能补写假 receipt，可保留旧库并采用上述全新库路径。

本地库入口的 `Options{}` 只用于这种隔离演练；它不是生产操作命令。
生产仍必须走本文末尾的独立授权和 controller 证明流程。

## 可执行的只读检查

使用与目标版本相同源码构建的 `kkai-migrate`；DSN 从标准输入读取，避免出现在
进程参数中。以下命令不创建表、不写 migration receipt：

```sh
./kkai-migrate --describe-contract --dialect postgres --json
./kkai-migrate --precheck --target 16 --json --dsn-stdin
./kkai-migrate --target 16 --dry-run --dsn-stdin
./kkai-migrate --observe --current --json --dsn-stdin
```

v16 precheck 只接受完整 canonical v15 或 v16；v14 必须先走独立 v15 流程。
缺少部分 v16 表/索引但已存在对象形状完全正确时，precheck 允许恢复中断的
MySQL DDL；任何已有字段或同名索引不匹配都会停止，绝不自动删除或重建。

## 隔离演练与 mutation 入口

库入口是 `kkaimigrate.ApplyRequestTicketExpand(ctx, db, options)`，复用共享
schema advisory lock 并在实际 DDL 前调用 `options.AuthorizeMutation`。
SQLite/PostgreSQL 的 DDL 与 v16 receipt 在同一个事务中；MySQL 隐式提交
DDL，失败后仅允许精确形状的幂等恢复。调用前必须独立完成 canonical v15。

```go
// 仅用于隔离数据库演练；db 不得连接生产数据库。
_, err := kkaimigrate.ApplyRequestTicketExpand(ctx, db, kkaimigrate.Options{
    Offline: db.Dialector.Name() == kkaimigrate.DialectMySQL,
})
```

项目内的 `TestRequestTicketExpand*` 是该入口的可重复执行演练：

```sh
go test ./pkg/kkaimigrate -run '^TestRequestTicket' -count=1 -timeout=120s
```

SQLite 演练默认执行。真实 PostgreSQL/MySQL 演练分别需要配置
`KKAI_IMAGE_ASYNC_POSTGRES_TEST_DSN`、`KKAI_IMAGE_ASYNC_MYSQL_TEST_DSN`，
fixture 只创建并清理隔离 schema/database，不得把这些变量指向无隔离授权的生产账号。
PostgreSQL 9.6–11 的隔离/停写演练还需 `Options{Offline:true}`；生产 CLI
不提供 Offline。

## 生产启用尚需独立授权与 controller 支持

本次功能实现不执行生产迁移。普通 `--target 16` 明确拒绝 mutation，
既有 `--reseller-prefix apply` 的 v15 proof 协议也不能授权 v16。
目前代码提供完整可测试的 library mutation 入口，但没有宣称生产 controller
已支持或安装 v16；生产启用需要另行实现、评审并安装精确版本的 controller
流程，然后在单独获准的 schema 操作中调用上述入口。

controller 应在 mutation 前及锁内重新核对：目标数据库身份、canonical v15
源摘要、精确 v16 plan/target 摘要，以及 active、rollback、writer 三类运行时
都明确接受该 v16 prefix。验证备份与隔离演练后才能应用，完成后独立检查
`--check --min-version 16` 和 `--observe --current --json`，再替换进程开启 gate。
不得用临时脚本直连生产库、GORM AutoMigrate、普通发布或 v15 proof 绕过这些门槛。
