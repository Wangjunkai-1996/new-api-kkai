package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/topuprecovery"
	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const maximumManifestBytes = 8 * 1024 * 1024

var sourceRevision = "development"

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: kkai-topup-recovery plan|apply|verify [flags]")
	}
	mode := os.Args[1]
	flags := flag.NewFlagSet(mode, flag.ExitOnError)
	activeFromID := flags.Int64("active-from-topup-id", 0, "first eligible topup ID")
	cutoffID := flags.Int64("cutoff-topup-id", 0, "inclusive historical topup cutoff; defaults to the current high-water mark")
	expectedSHA256 := flags.String("expected-sha256", "", "reviewed manifest SHA-256")
	timeout := flags.Duration("timeout", 20*time.Minute, "overall recovery timeout")
	if err := flags.Parse(os.Args[2:]); err != nil {
		log.Fatal(err)
	}

	db, err := openDatabase(strings.TrimSpace(os.Getenv("SQL_DSN")))
	if err != nil {
		log.Fatal("failed to open recovery database")
	}
	sqlDB, err := db.DB()
	if err != nil {
		log.Fatal("failed to access recovery database")
	}
	defer sqlDB.Close()
	provider, err := topuprecovery.NewEPayProviderFromDatabase(db, nil)
	if err != nil {
		log.Fatalf("failed to initialize EPay evidence provider: %v", err)
	}
	service, err := topuprecovery.NewFromDatabase(db, provider, sourceRevision)
	if err != nil {
		log.Fatalf("failed to initialize recovery quota configuration: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	switch mode {
	case "plan":
		resolvedCutoffID := *cutoffID
		if resolvedCutoffID == 0 {
			resolvedCutoffID, err = service.LatestCutoff(ctx, *activeFromID)
			if err != nil {
				log.Fatalf("resolve recovery cutoff: %v", err)
			}
		}
		manifest, err := service.Plan(ctx, *activeFromID, resolvedCutoffID)
		if err != nil {
			log.Fatalf("recovery plan failed: %v", err)
		}
		if err := writeJSON(os.Stdout, manifest); err != nil {
			log.Fatalf("write recovery plan output: %v", err)
		}
	case "apply", "verify":
		manifest, err := readManifest(os.Stdin)
		if err != nil {
			log.Fatalf("read recovery manifest: %v", err)
		}
		var result *topuprecovery.Result
		if mode == "apply" {
			result, err = service.Apply(ctx, manifest, *expectedSHA256)
		} else {
			result, err = service.Verify(ctx, manifest, *expectedSHA256)
		}
		if err != nil {
			log.Fatalf("recovery %s failed: %v", mode, err)
		}
		if err := writeJSON(os.Stdout, result); err != nil {
			log.Fatalf("write recovery %s output: %v", mode, err)
		}
	default:
		log.Fatalf("unsupported recovery mode %q", mode)
	}
}

func readManifest(reader io.Reader) (*topuprecovery.Manifest, error) {
	raw, err := io.ReadAll(io.LimitReader(reader, maximumManifestBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maximumManifestBytes {
		return nil, fmt.Errorf("manifest exceeds %d bytes", maximumManifestBytes)
	}
	manifest := &topuprecovery.Manifest{}
	if err := common.Unmarshal(raw, manifest); err != nil {
		return nil, err
	}
	return manifest, nil
}

func writeJSON(writer io.Writer, value any) error {
	encoded, err := common.Marshal(value)
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	written, err := writer.Write(encoded)
	if err != nil {
		return err
	}
	if written != len(encoded) {
		return io.ErrShortWrite
	}
	return nil
}

func openDatabase(dsn string) (*gorm.DB, error) {
	if dsn == "" {
		return nil, fmt.Errorf("SQL_DSN is required")
	}
	config := &gorm.Config{Logger: logger.Discard}
	switch {
	case strings.HasPrefix(dsn, "postgres://"), strings.HasPrefix(dsn, "postgresql://"):
		return gorm.Open(postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: true}), config)
	case strings.HasPrefix(dsn, "sqlite://"):
		return gorm.Open(sqlite.Open(strings.TrimPrefix(dsn, "sqlite://")), config)
	case strings.HasPrefix(dsn, "file:"):
		return gorm.Open(sqlite.Open(dsn), config)
	default:
		if !strings.Contains(dsn, "parseTime=") {
			separator := "?"
			if strings.Contains(dsn, "?") {
				separator = "&"
			}
			dsn += separator + "parseTime=true"
		}
		return gorm.Open(mysql.Open(dsn), config)
	}
}
