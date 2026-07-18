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

var sourceRevision string

func main() {
	var (
		dsn              string
		dsnFromStdin     bool
		dryRun           bool
		checkOnly        bool
		current          bool
		describe         bool
		describeUpstream bool
		checkUpstream    bool
		bootstrapEmpty   bool
		jsonOutput       bool
		describeSource   string
		minimumVersion   int64
		timeout          time.Duration
	)
	flag.StringVar(&dsn, "dsn", firstNonEmpty(os.Getenv("KKAI_MIGRATION_DSN"), os.Getenv("SQL_DSN")), "database DSN")
	flag.BoolVar(&dsnFromStdin, "dsn-stdin", false, "read one database DSN from stdin")
	flag.BoolVar(&dryRun, "dry-run", false, "show pending migrations without applying them")
	flag.BoolVar(&checkOnly, "check", false, "verify the minimum schema version and exit")
	flag.BoolVar(&current, "current", false, "read the current KKAI schema version without applying migrations")
	flag.BoolVar(&describe, "describe", false, "print the image schema compatibility contract and exit")
	flag.BoolVar(&describeUpstream, "describe-upstream-schema", false, "print the versioned upstream model schema contract and exit")
	flag.BoolVar(&checkUpstream, "check-upstream-baseline", false, "read-only exact check of the upstream database schema")
	flag.BoolVar(&bootstrapEmpty, "bootstrap-empty-upstream-baseline", false, "create the upstream baseline in a strictly empty database")
	flag.BoolVar(&jsonOutput, "json", false, "emit machine-readable JSON for --describe or --check")
	flag.StringVar(&describeSource, "source-revision", sourceRevision, "source revision for --describe")
	flag.Int64Var(&minimumVersion, "min-version", kkaimigrate.RuntimeMinVersion, "minimum schema version for --check")
	flag.DurationVar(&timeout, "timeout", 5*time.Minute, "overall migration timeout")
	flag.Parse()
	if enabledModeCount(dryRun, checkOnly, current, describe, describeUpstream, checkUpstream, bootstrapEmpty) > 1 {
		log.Fatal("migration operation modes cannot be combined")
	}
	if describe {
		if !jsonOutput {
			log.Fatal("--describe requires --json")
		}
		encoded, err := describeJSON(describeSource)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(string(encoded))
		return
	}
	if describeUpstream {
		if !jsonOutput {
			log.Fatal("--describe-upstream-schema requires --json")
		}
		encoded, err := describeUpstreamJSON(describeSource)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(string(encoded))
		return
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
	if checkUpstream {
		if !jsonOutput {
			log.Fatal("--check-upstream-baseline requires --json")
		}
		adoption, err := kkaimigrate.ObserveUpstreamSchema(ctx, db, describeSource)
		if err != nil {
			log.Fatalf("upstream schema baseline check failed: %v", err)
		}
		encoded, err := adoption.CanonicalJSON()
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(string(encoded))
		if !adoption.Ready {
			log.Fatal("upstream schema baseline is incomplete")
		}
		return
	}
	if bootstrapEmpty {
		if err := validateEmptyBootstrapRuntime(os.Getenv(common.NodeRoleEnvironmentVariable)); err != nil {
			log.Fatal(err)
		}
		if err := model.BootstrapEmptyUpstreamSchema(ctx, db); err != nil {
			log.Fatalf("upstream schema baseline bootstrap failed: %v", err)
		}
		fmt.Println("upstream schema baseline bootstrapped")
		return
	}
	if current {
		observed, err := kkaimigrate.Observe(ctx, db)
		if err != nil {
			log.Fatalf("KKAI schema observation failed: %v", err)
		}
		if jsonOutput {
			encoded, err := currentJSON(observed)
			if err != nil {
				log.Fatal(err)
			}
			fmt.Println(string(encoded))
			return
		}
		fmt.Printf("KKAI schema is currently at version %d\n", observed.CurrentVersion)
		return
	}
	if checkOnly {
		if err := kkaimigrate.Check(ctx, db, minimumVersion); err != nil {
			log.Fatalf("KKAI schema check failed: %v", err)
		}
		if jsonOutput {
			observed, err := kkaimigrate.Observe(ctx, db)
			if err != nil {
				log.Fatalf("KKAI schema observation failed: %v", err)
			}
			encoded, err := checkJSON(observed, kkaimigrate.SchemaCompatibility(sourceRevision))
			if err != nil {
				log.Fatal(err)
			}
			fmt.Println(string(encoded))
			return
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

func validateEmptyBootstrapRuntime(nodeRole string) error {
	if strings.TrimSpace(nodeRole) != "" {
		return fmt.Errorf("--bootstrap-empty-upstream-baseline is unavailable in application node roles")
	}
	return nil
}

func enabledModeCount(modes ...bool) int {
	count := 0
	for _, enabled := range modes {
		if enabled {
			count++
		}
	}
	return count
}

func describeJSON(sourceRevision string) ([]byte, error) {
	return kkaimigrate.SchemaCompatibility(sourceRevision).CanonicalJSON()
}

func describeUpstreamJSON(sourceRevision string) ([]byte, error) {
	contract, err := kkaimigrate.UpstreamSchemaCompatibility(sourceRevision)
	if err != nil {
		return nil, err
	}
	return contract.CanonicalJSON()
}

func checkJSON(observed kkaimigrate.SchemaObservation, contract kkaimigrate.SchemaContract) ([]byte, error) {
	return common.Marshal(struct {
		Schema                 int    `json:"schema"`
		Ready                  bool   `json:"ready"`
		CurrentVersion         int64  `json:"current_version"`
		MigrationSetDigest     string `json:"migration_set_digest"`
		RuntimeMinVersion      int64  `json:"runtime_min_version"`
		RuntimeMaxVersion      int64  `json:"runtime_max_version"`
		MigrationTargetVersion int64  `json:"migration_target_version"`
	}{
		Schema:                 1,
		Ready:                  true,
		CurrentVersion:         observed.CurrentVersion,
		MigrationSetDigest:     observed.MigrationSetDigest,
		RuntimeMinVersion:      contract.RuntimeMinVersion,
		RuntimeMaxVersion:      contract.RuntimeMaxVersion,
		MigrationTargetVersion: contract.MigrationTargetVersion,
	})
}

func currentJSON(observed kkaimigrate.SchemaObservation) ([]byte, error) {
	return common.Marshal(observed)
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
