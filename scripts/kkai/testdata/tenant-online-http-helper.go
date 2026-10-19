// Local synthetic acceptance helper. This is not a production migration CLI.
package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/kkaimigrate"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func main() {
	if len(os.Args) != 2 {
		panic("expected hash, describe, expand, contract, settle, or release")
	}
	if os.Args[1] == "hash" {
		hash, err := bcrypt.GenerateFromPassword([]byte("LocalSyntheticAcceptance123"), bcrypt.MinCost)
		must(err)
		fmt.Println(string(hash))
		return
	}
	if os.Args[1] == "describe" {
		fmt.Printf("min=%d max=%d target=%d\n", kkaimigrate.RuntimeMinVersion, kkaimigrate.RuntimeMaxVersion, kkaimigrate.MigrationTargetVersion)
		return
	}
	config, err := pgx.ParseConfig(os.Getenv("KKAI_LOCAL_HTTP_DSN"))
	must(err)
	if config.Host != "127.0.0.1" || !strings.HasPrefix(config.Database, "kkai_test_") || len(config.Fallbacks) != 0 {
		panic("requires dedicated kkai_test_ database on 127.0.0.1 without fallbacks")
	}
	db, err := gorm.Open(postgres.New(postgres.Config{DSN: os.Getenv("KKAI_LOCAL_HTTP_DSN"), PreferSimpleProtocol: true}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	must(err)
	common.SetDatabaseTypes(common.DatabaseTypePostgreSQL, common.DatabaseTypePostgreSQL)
	sqlDB, err := db.DB()
	must(err)
	defer sqlDB.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if os.Args[1] == "settle" {
		options, parseErr := redis.ParseURL(os.Getenv("KKAI_LOCAL_HTTP_REDIS"))
		must(parseErr)
		host, _, addressErr := net.SplitHostPort(options.Addr)
		must(addressErr)
		if host != "127.0.0.1" {
			panic("requires dedicated loopback Redis")
		}
		common.RDB = redis.NewClient(options)
		defer common.RDB.Close()
		must(common.RDB.Ping(ctx).Err())
		model.DB = db
	}
	switch os.Args[1] {
	case "expand":
		_, err = kkaimigrate.ApplyTenantOnlineExpansion(ctx, db, kkaimigrate.Options{})
	case "contract":
		_, err = kkaimigrate.FinalizeTenantOnlineSchema(ctx, db, kkaimigrate.Options{})
	case "settle":
		var requests []string
		err = db.Model(&model.TenantConsumption{}).Where("status IN ?", []string{"settlement_pending", "refund_pending"}).Pluck("request_id", &requests).Error
		must(err)
		for _, request := range requests {
			must(model.SettleTenantConsumption(ctx, db, request))
		}
	case "release":
		// Advance only the worker's input clock in this disposable database.
		var due int64
		err = db.Model(&model.ResellerLedgerEntry{}).Select("COALESCE(MAX(available_at),0)").Scan(&due).Error
		must(err)
		var tenants []int64
		err = db.Model(&model.ResellerWallet{}).Pluck("tenant_id", &tenants).Error
		must(err)
		for _, tenant := range tenants {
			err = db.Transaction(func(tx *gorm.DB) error {
				_, releaseErr := model.ReleaseResellerPendingTx(tx, tenant, due, 0)
				return releaseErr
			})
			must(err)
		}
	default:
		panic("unsupported local helper mode")
	}
	must(err)
	if os.Args[1] != "release" && os.Args[1] != "settle" {
		status, observeErr := kkaimigrate.ObserveTenantOnlineExpansion(ctx, db)
		must(observeErr)
		payload, marshalErr := common.Marshal(status)
		must(marshalErr)
		fmt.Println(string(payload))
	}
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
