package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/pkg/kkaimigrate"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
	var (
		dsn            string
		dryRun         bool
		checkOnly      bool
		minimumVersion int64
		timeout        time.Duration
	)
	flag.StringVar(&dsn, "dsn", firstNonEmpty(os.Getenv("KKAI_MIGRATION_DSN"), os.Getenv("SQL_DSN")), "database DSN")
	flag.BoolVar(&dryRun, "dry-run", false, "show pending migrations without applying them")
	flag.BoolVar(&checkOnly, "check", false, "verify the minimum schema version and exit")
	flag.Int64Var(&minimumVersion, "min-version", kkaimigrate.CurrentVersion, "minimum schema version for --check")
	flag.DurationVar(&timeout, "timeout", 5*time.Minute, "overall migration timeout")
	flag.Parse()

	if strings.TrimSpace(dsn) == "" {
		log.Fatal("KKAI_MIGRATION_DSN or SQL_DSN is required")
	}
	db, err := openDatabase(dsn)
	if err != nil {
		log.Fatal("failed to open migration database")
	}
	sqlDB, err := db.DB()
	if err != nil {
		log.Fatal("failed to access migration database")
	}
	defer sqlDB.Close()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if checkOnly {
		if err := kkaimigrate.Check(ctx, db, minimumVersion); err != nil {
			log.Fatalf("KKAI schema check failed: %v", err)
		}
		fmt.Printf("KKAI schema is ready at version %d\n", minimumVersion)
		return
	}

	result, err := kkaimigrate.Apply(ctx, db, kkaimigrate.Options{DryRun: dryRun})
	if err != nil {
		log.Fatalf("KKAI migration failed: %v", err)
	}
	if dryRun {
		for _, item := range result.Pending {
			fmt.Printf("pending %04d %s %s\n", item.Version, item.Name, item.Checksum)
		}
		return
	}
	for _, item := range result.Applied {
		fmt.Printf("applied %04d %s %s\n", item.Version, item.Name, item.Checksum)
	}
}

func openDatabase(dsn string) (*gorm.DB, error) {
	switch {
	case strings.HasPrefix(dsn, "postgres://"), strings.HasPrefix(dsn, "postgresql://"):
		return gorm.Open(postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: true}), &gorm.Config{})
	case strings.HasPrefix(dsn, "sqlite://"):
		return gorm.Open(sqlite.Open(strings.TrimPrefix(dsn, "sqlite://")), &gorm.Config{})
	case strings.HasPrefix(dsn, "file:"):
		return gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	default:
		if !strings.Contains(dsn, "parseTime=") {
			separator := "?"
			if strings.Contains(dsn, "?") {
				separator = "&"
			}
			dsn += separator + "parseTime=true"
		}
		return gorm.Open(mysql.Open(dsn), &gorm.Config{})
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
