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
gate 在收到工单请求时只读核验，失败结果最多缓存 30 秒；完整 v16 校验通过后
在进程内缓存成功结果。因此在线迁移完成后无需重启或替换正在服务的进程，
后续工单请求即可启用。每次失败重试有独立的 3 秒超时，不执行 DDL。

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
生产仍必须走本文末尾的独立授权和已安装 controller 证明流程。

## 可执行的只读检查

使用与目标版本相同源码构建的 `kkai-migrate`；DSN 从标准输入读取，避免出现在
进程参数中。以下命令不创建表、不写 migration receipt：

```sh
./kkai-migrate --describe-contract --dialect postgres --json
./kkai-migrate --precheck --target 16 --json --dsn-stdin
./kkai-migrate --target 16 --dry-run --dsn-stdin
./kkai-migrate --observe --current --json --dsn-stdin
./kkai-migrate --request-ticket plan --json --dsn-stdin
./kkai-migrate --request-ticket status --json --dsn-stdin
```

v16 precheck 只接受完整 canonical v15 或 v16；v14 必须先走独立 v15 流程。
缺少部分 v16 表/索引但已存在对象形状完全正确时，precheck 允许恢复中断的
MySQL DDL；任何已有字段或同名索引不匹配都会停止，绝不自动删除或重建。
专用 `--request-ticket plan/status` 比通用 precheck 更严格：canonical v15 时
两张目标表必须都不存在；canonical v16 时要求完整 receipt 和精确物理结构。
它们返回 `kkai_request_ticket_v1`，绑定数据库/Schema 身份、source/target digest
与编译内的 v16 plan checksum，不恢复无 receipt 的部分生产对象。

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

## 生产启用：独立 v16 controller 协议

普通 `--target 16` 仍明确拒绝 mutation，既有 `--reseller-prefix apply` 的
v15 proof 协议也不能授权 v16。唯一生产写入口为
`--request-ticket apply --json --tenant-controller-proof /run/tenant-online/proof.json`，
只接受 PostgreSQL >=12。该入口必须由已经安装并验证的精确版本 controller
在专用迁移容器中调用，不能把源码提供协议视作生产已安装的证据。

生产流程先安装 controller 支持，再在 canonical v15 上按正常蓝绿发布路径
完成两个独立版本的全灰度验收，确保 active、rollback、writer 三个实际运行
进程均具有精确 `(14,16,14)`、`schema_management=external` 合同，且包含
canonical v14/v15/v16 的完整摘要。不能仅检查镜像内迁移二进制的声明，
也不能保留最高只支持 v15 的回滚进程再执行迁移。候选/灰度事务须先结束，
迁移期间保持既有路由与服务进程，前端启用独立于后端候选发布事务。

controller 的只读 plan 绑定当前数据库与运行进程身份；获准的 apply 必须
匹配该 plan 的确认摘要，并生成短效的 `request-ticket-v1` 证明：

```sh
sudo /usr/local/sbin/kkai-newapi-schema-migrate request-ticket plan
sudo /usr/local/sbin/kkai-newapi-schema-migrate request-ticket status
# 必须使用刚生成且通过复核的 plan.confirmation_sha256。
sudo /usr/local/sbin/kkai-newapi-schema-migrate request-ticket apply \
  --confirm-plan-sha256 EXACT_FRESH_PLAN_CONFIRMATION
```

- JSON schema 为 `kkai_request_ticket_proof_v1`，operation 为 `apply`。
- 固定 `/run/tenant-online/proof.json` 必须是独立只读 bind mount 中 root
  所有、权限 `0400`、不超过 64 KiB 的普通文件，迁移容器以 UID 0 校验。
- 证明绑定安装的 infra commit、精确迁移镜像 ID、数据库/Schema 身份、
  source 15 / target 16、两个 canonical digest、编译内 plan checksum、
  32 位小写十六进制 nonce；有效期不超过 120 秒。
- active、rollback、writer 三类证据必须来自不同容器，tenant-aware、日志
  使用主数据库，合同完整且精确匹配；迁移镜像必须等于 active 的镜像。

应用在执行前及共享 advisory lock 内的 DDL 事务中重新读取数据库状态并
校验证明。DDL 只创建两张新表和四个索引，与 v16 receipt 在同一事务提交；
遇锁竞争或超时即失败。controller 临时开放迁移身份并在成功或失败后收回
登录权，随后核对只读 v16 状态、运行进程身份和线上错误窗口。

迁移后用 `--request-ticket status --json`、`--check --min-version 16` 和
`--observe --current --json` 验证；gate 在下一次允许重试的请求上识别 receipt，
无需重启现有进程。前端按独立发布流程启用。回滚仅使用已验证的 v16 兼容
应用，不删除新表、不覆盖业务数据库。不得用临时脚本直连生产库、GORM
AutoMigrate、普通发布或 v15 proof 绕过这些门槛。
