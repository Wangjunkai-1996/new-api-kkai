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
	"github.com/QuantumNous/new-api/internal/kkaischemacli"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/kkaimigrate"

	"gorm.io/gorm"
)

func main() {
	var (
		dsn            string
		dsnFromStdin   bool
		dryRun         bool
		checkOnly      bool
		currentOnly    bool
		describe       bool
		dialect        string
		minimumVersion int64
		targetVersion  int64
		observe        bool
		jsonOutput     bool
		timeout        time.Duration
	)
	flag.StringVar(&dsn, "dsn", firstNonEmpty(os.Getenv("KKAI_MIGRATION_DSN"), os.Getenv("SQL_DSN")), "database DSN")
	flag.BoolVar(&dsnFromStdin, "dsn-stdin", false, "read one database DSN from stdin")
	flag.BoolVar(&dryRun, "dry-run", false, "show pending migrations without applying them")
	flag.BoolVar(&checkOnly, "check", false, "verify the minimum schema version and exit")
	flag.BoolVar(&currentOnly, "current", false, "observe the current database migration prefix")
	flag.BoolVar(&describe, "describe-contract", false, "describe the runtime schema contract")
	flag.StringVar(&dialect, "dialect", "", "database dialect for --describe-contract")
	flag.Int64Var(&minimumVersion, "min-version", 0, "minimum schema version for --check; defaults to the dialect requirement")
	flag.Int64Var(&targetVersion, "target", 0, "explicit maintenance target: 4 or 5; omitted keeps the runtime target")
	flag.BoolVar(&observe, "observe", false, "read and validate the current database migration prefix")
	flag.BoolVar(&jsonOutput, "json", false, "emit machine-readable JSON")
	flag.DurationVar(&timeout, "timeout", 5*time.Minute, "overall migration timeout")
	flag.Parse()
	if describe {
		if strings.TrimSpace(dialect) == "" || !jsonOutput || observe || checkOnly || dryRun || currentOnly || minimumVersion != 0 || targetVersion != 0 || dsnFromStdin {
			log.Fatal("--describe-contract requires --dialect and --json and cannot be combined with database operations")
		}
		output, err := describeContractJSON(dialect)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(output)
		return
	}
	if observe {
		if !currentOnly || !jsonOutput || checkOnly || dryRun || minimumVersion != 0 || targetVersion != 0 || dialect != "" {
			log.Fatal("--observe requires --current --json and cannot be combined with migration operations")
		}
	} else if currentOnly || jsonOutput || dialect != "" {
		log.Fatal("--current and --json require --observe or --describe-contract")
	}

	resolvedDSN, err := resolveMigrationDSN(dsn, dsnFromStdin, os.Stdin)
	if err != nil {
		log.Fatal(err)
	}
	db, err := openDatabase(resolvedDSN)
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
	if observe {
		observation, err := observeCurrentSchema(ctx, db)
		if err != nil {
			log.Fatalf("KKAI schema observation failed: %v", err)
		}
		encoded, err := common.Marshal(observation)
		if err != nil {
			log.Fatal("failed to encode KKAI schema observation")
		}
		fmt.Println(string(encoded))
		return
	}
	if checkOnly {
		if targetVersion != 0 {
			log.Fatal("--check cannot be combined with --target")
		}
		if minimumVersion == 0 {
			err = kkaimigrate.CheckRequired(ctx, db)
		} else {
			err = kkaimigrate.Check(ctx, db, minimumVersion)
		}
		if err != nil {
			log.Fatalf("KKAI schema check failed: %v", err)
		}
		fmt.Println("KKAI schema is ready")
		return
	}

	result, err := applyMigrationTarget(ctx, db, targetVersion, kkaimigrate.Options{DryRun: dryRun})
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

func applyMigrationTarget(ctx context.Context, db *gorm.DB, targetVersion int64, options kkaimigrate.Options) (*kkaimigrate.Result, error) {
	switch targetVersion {
	case 0:
		return kkaimigrate.Apply(ctx, db, options)
	case kkaimigrate.OutboxEventKeySchemaVersion:
		return kkaimigrate.ApplyOutboxEventKeyCompatibility(ctx, db, options)
	case kkaimigrate.VideoStudioSchemaVersion:
		return kkaimigrate.ApplyVideoStudioExpand(ctx, db, options)
	default:
		return nil, fmt.Errorf("unsupported KKAI migration target %d; expected 4 or 5", targetVersion)
	}
}

func observeCurrentSchema(ctx context.Context, db *gorm.DB) (*kkaimigrate.Observation, error) {
	observation, err := kkaimigrate.Observe(ctx, db)
	if err != nil {
		return nil, err
	}
	if err := model.ValidateMainSchemaPrerequisites(db.WithContext(ctx)); err != nil {
		return nil, err
	}
	return observation, nil
}

func describeContractJSON(dialect string) (string, error) {
	contract, err := kkaimigrate.ContractForDialect(strings.TrimSpace(dialect))
	if err != nil {
		return "", err
	}
	output := struct {
		kkaimigrate.SchemaContract
		SchemaManagement string `json:"schema_management"`
	}{
		SchemaContract:   contract,
		SchemaManagement: common.SchemaManagementMode,
	}
	encoded, err := common.Marshal(output)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func resolveMigrationDSN(explicitDSN string, fromStdin bool, reader io.Reader) (string, error) {
	return kkaischemacli.ResolveDSN(
		explicitDSN,
		fromStdin,
		reader,
		"KKAI_MIGRATION_DSN, SQL_DSN, --dsn, or --dsn-stdin is required",
	)
}

func openDatabase(dsn string) (*gorm.DB, error) {
	return kkaischemacli.OpenDatabase(dsn)
}

func firstNonEmpty(values ...string) string {
	return kkaischemacli.FirstNonEmpty(values...)
}
