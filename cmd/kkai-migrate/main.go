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
	"gorm.io/gorm/logger"
)

func main() {
	var (
		dsn                   string
		dsnFromStdin          bool
		dryRun                bool
		checkOnly             bool
		currentOnly           bool
		precheck              bool
		describe              bool
		dialect               string
		minimumVersion        int64
		targetVersion         int64
		tenantLogSchema       string
		tenantExpand          string
		tenantControllerProof string
		tenantRuntime         bool
		observe               bool
		jsonOutput            bool
		timeout               time.Duration
	)
	flag.StringVar(&dsn, "dsn", firstNonEmpty(os.Getenv("KKAI_MIGRATION_DSN"), os.Getenv("SQL_DSN")), "database DSN")
	flag.BoolVar(&dsnFromStdin, "dsn-stdin", false, "read one database DSN from stdin")
	flag.BoolVar(&dryRun, "dry-run", false, "show pending migrations without applying them")
	flag.BoolVar(&checkOnly, "check", false, "verify the minimum schema version and exit")
	flag.BoolVar(&currentOnly, "current", false, "observe the current database migration prefix")
	flag.BoolVar(&precheck, "precheck", false, "run the read-only precheck for an explicit maintenance target")
	flag.BoolVar(&describe, "describe-contract", false, "describe the runtime schema contract")
	flag.StringVar(&dialect, "dialect", "", "database dialect for --describe-contract")
	flag.Int64Var(&minimumVersion, "min-version", 0, "minimum schema version for --check; defaults to the dialect requirement")
	flag.Int64Var(&targetVersion, "target", 0, "explicit target: 3 (new database baseline), 4, 5, 6, 7, 8, 9, 10, 11, or 13; 15 permits precheck/dry-run only; target 12 is retired; omitted keeps the runtime target")
	flag.BoolVar(&observe, "observe", false, "read and validate the current database migration prefix")
	flag.BoolVar(&jsonOutput, "json", false, "emit machine-readable JSON")
	flag.DurationVar(&timeout, "timeout", 5*time.Minute, "overall migration timeout")
	flag.StringVar(&tenantLogSchema, "tenant-log-schema", "", "separate LOG_DB tenant schema operation: apply, check, or status")
	flag.StringVar(&tenantExpand, "tenant-expand", "", "online expansion: plan/status/check are read-only; apply/finalize require the installed root-controller proof")
	flag.StringVar(&tenantControllerProof, "tenant-controller-proof", "", "fixed root-controller proof mount for apply/finalize")
	flag.BoolVar(&tenantRuntime, "tenant-runtime", false, "declare compiled tenant runtime capability; requires --dialect and --json, no database access")
	flag.Parse()
	if tenantRuntime {
		if !jsonOutput || dialect == "" || tenantExpand != "" || tenantControllerProof != "" || describe || observe || precheck || currentOnly || checkOnly || dryRun || minimumVersion != 0 || targetVersion != 0 || tenantLogSchema != "" || dsnFromStdin || flag.NArg() != 0 {
			log.Fatal("--tenant-runtime requires only --dialect and --json")
		}
		if err := writeTenantRuntimeContract(dialect, os.Stdout); err != nil {
			log.Fatal(err)
		}
		return
	}
	if tenantControllerProof != "" && tenantExpand != "apply" && tenantExpand != "finalize" {
		log.Fatal("--tenant-controller-proof is only valid for apply/finalize")
	}
	if tenantExpand != "" {
		if err := validateTenantOnlineCommand(tenantExpand, tenantControllerProof, jsonOutput, describe || observe || precheck || currentOnly || checkOnly || dryRun || minimumVersion != 0 || targetVersion != 0 || dialect != "" || tenantLogSchema != "" || flag.NArg() != 0); err != nil {
			log.Fatal(err)
		}
	}
	if tenantLogSchema != "" {
		if tenantLogSchema != "apply" && tenantLogSchema != "check" && tenantLogSchema != "status" {
			log.Fatal("--tenant-log-schema expects apply, check, or status")
		}
		if describe || observe || precheck || currentOnly || checkOnly || dryRun || minimumVersion != 0 || targetVersion != 0 || dialect != "" || jsonOutput {
			log.Fatal("--tenant-log-schema cannot be combined with other operations")
		}
	}
	if describe {
		if strings.TrimSpace(dialect) == "" || !jsonOutput || observe || checkOnly || dryRun || currentOnly || precheck || minimumVersion != 0 || targetVersion != 0 || dsnFromStdin {
			log.Fatal("--describe-contract requires --dialect and --json and cannot be combined with database operations")
		}
		output, err := describeContractJSON(dialect)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(output)
		return
	}
	if precheck {
		if !jsonOutput || (targetVersion != kkaimigrate.AuthenticationSchemaVersion && targetVersion != kkaimigrate.TenantSchemaVersion && targetVersion != kkaimigrate.TenantCompatibilitySchemaVersion && targetVersion != kkaimigrate.ResellerApplicationSchemaVersion) || observe || currentOnly || checkOnly || dryRun || minimumVersion != 0 || dialect != "" {
			log.Fatal("--precheck requires --target 8, 12, 13, or 15 and --json and cannot be combined with other operations")
		}
	} else if observe {
		if !currentOnly || !jsonOutput || checkOnly || dryRun || minimumVersion != 0 || targetVersion != 0 || dialect != "" {
			log.Fatal("--observe requires --current --json and cannot be combined with migration operations")
		}
	} else if currentOnly || jsonOutput && tenantExpand == "" || dialect != "" {
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
	if jsonOutput {
		// SQL diagnostics must not corrupt the machine-readable stdout stream.
		// Operation errors still propagate to the fatal stderr message below.
		db = db.Session(&gorm.Session{Logger: db.Logger.LogMode(logger.Silent)})
	}
	sqlDB, err := db.DB()
	if err != nil {
		log.Fatal("failed to access migration database")
	}
	defer sqlDB.Close()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if tenantExpand != "" {
		var err error
		if tenantExpand == "apply" || tenantExpand == "finalize" {
			err = runTenantControllerMutation(ctx, db, tenantExpand, tenantControllerProof, os.Stdout)
		} else {
			err = runTenantOnlineInspection(ctx, db, tenantExpand, os.Stdout)
		}
		if err != nil {
			log.Fatalf("KKAI tenant expansion inspection failed: %v", err)
		}
		return
	}
	if tenantLogSchema != "" {
		if err := runTenantLogSchema(ctx, db, tenantLogSchema, os.Stdout); err != nil {
			log.Fatalf("KKAI tenant log schema failed: %v", err)
		}
		return
	}
	if precheck {
		if err := runMigrationPrecheck(ctx, db, targetVersion, os.Stdout); err != nil {
			log.Fatalf("KKAI migration precheck failed: %v", err)
		}
		return
	}
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

func runMigrationPrecheck(ctx context.Context, db *gorm.DB, target int64, output io.Writer) error {
	var result any
	var err error
	var unsafeTenant bool
	switch target {
	case kkaimigrate.AuthenticationSchemaVersion:
		result, err = kkaimigrate.PrecheckAuthenticationExpand(ctx, db)
	case kkaimigrate.TenantSchemaVersion:
		var report *kkaimigrate.TenantSchemaPrecheck
		report, err = kkaimigrate.PrecheckTenantSchema(ctx, db)
		result = report
		unsafeTenant = report != nil && !report.Safe
	case kkaimigrate.TenantCompatibilitySchemaVersion:
		var report *kkaimigrate.TenantCompatibilityPrecheck
		report, err = kkaimigrate.PrecheckTenantCompatibilitySchema(ctx, db)
		result = report
		unsafeTenant = report != nil && !report.Safe
	case kkaimigrate.ResellerApplicationSchemaVersion:
		result, err = kkaimigrate.PrecheckResellerApplicationExpand(ctx, db)
	default:
		return fmt.Errorf("unsupported precheck target %d; expected 8, 12, 13, or 15", target)
	}
	if err != nil {
		return err
	}
	encoded, err := common.Marshal(result)
	if err != nil {
		return err
	}
	if _, err = fmt.Fprintln(output, string(encoded)); err != nil {
		return err
	}
	if unsafeTenant {
		return fmt.Errorf("%w: tenant identities are invalid or ambiguous; operator repair required", kkaimigrate.ErrSchemaNotReady)
	}
	return nil
}

func applyMigrationTarget(ctx context.Context, db *gorm.DB, targetVersion int64, options kkaimigrate.Options) (*kkaimigrate.Result, error) {
	switch targetVersion {
	case 0:
		return kkaimigrate.Apply(ctx, db, options)
	case kkaimigrate.JobLeaseSchemaVersion:
		return kkaimigrate.ApplyLegacyBaseline(ctx, db, options)
	case kkaimigrate.OutboxEventKeySchemaVersion:
		return kkaimigrate.ApplyOutboxEventKeyCompatibility(ctx, db, options)
	case kkaimigrate.VideoStudioSchemaVersion:
		return kkaimigrate.ApplyVideoStudioExpand(ctx, db, options)
	case kkaimigrate.VideoSampleCategorySchemaVersion:
		return kkaimigrate.ApplyVideoSampleCategoryExpand(ctx, db, options)
	case kkaimigrate.ImageStudioSchemaVersion:
		return kkaimigrate.ApplyImageStudioExpand(ctx, db, options)
	case kkaimigrate.AuthenticationSchemaVersion:
		return kkaimigrate.ApplyAuthenticationExpand(ctx, db, options)
	case kkaimigrate.RC41ArchitectureSchemaVersion:
		return kkaimigrate.ApplyRC41ArchitectureExpand(ctx, db, options)
	case kkaimigrate.ImageAsyncSchemaVersion:
		return kkaimigrate.ApplyImageAsyncExpand(ctx, db, options)
	case kkaimigrate.MembershipSchemaVersion:
		return kkaimigrate.ApplyMembershipExpand(ctx, db, options)
	case kkaimigrate.TenantSchemaVersion:
		return nil, fmt.Errorf("%w: maintenance target 12 is retired; use explicit --target 13 from pristine schema 11", kkaimigrate.ErrUnsafeMigration)
	case kkaimigrate.TenantCompatibilitySchemaVersion:
		return kkaimigrate.ApplyTenantCompatibilitySchema(ctx, db, options)
	case kkaimigrate.TenantOnlineSchemaVersion:
		return nil, fmt.Errorf("%w: online contract requires verified tenant-aware active, rollback and background runtimes; it is not an ordinary --target operation", kkaimigrate.ErrUnsafeMigration)
	case kkaimigrate.ResellerApplicationSchemaVersion:
		if !options.DryRun {
			return nil, fmt.Errorf("%w: target 15 mutation requires installed controller verification of v15-compatible serving and rollback runtimes; only precheck/dry-run is available", kkaimigrate.ErrUnsafeMigration)
		}
		return kkaimigrate.ApplyResellerApplicationExpand(ctx, db, options)
	default:
		return nil, fmt.Errorf("unsupported KKAI migration target %d; expected 3, 4, 5, 6, 7, 8, 9, 10, 11, 13, or read-only 15", targetVersion)
	}
}

func observeCurrentSchema(ctx context.Context, db *gorm.DB) (*kkaimigrate.Observation, error) {
	observation, err := kkaimigrate.Observe(ctx, db)
	if err != nil {
		return nil, err
	}
	// Observe validates the historical schema at its recorded version. The
	// current application models apply only after its runtime schema is present.
	if observation.CurrentVersion >= kkaimigrate.RequiredRuntimeVersion {
		if err := model.ValidateMainSchemaPrerequisites(db.WithContext(ctx)); err != nil {
			return nil, err
		}
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

func runTenantLogSchema(ctx context.Context, db *gorm.DB, operation string, output io.Writer) error {
	switch operation {
	case "apply":
		if err := kkaimigrate.ApplyTenantLogSchema(ctx, db, kkaimigrate.Options{}); err != nil {
			return err
		}
	case "check":
		if err := kkaimigrate.CheckTenantLogSchema(ctx, db); err != nil {
			return err
		}
	case "status":
		err := kkaimigrate.CheckTenantLogSchema(ctx, db)
		result := struct {
			Schema string `json:"schema"`
			Ready  bool   `json:"ready"`
		}{"kkai-tenant-log-v12", err == nil}
		encoded, encodeErr := common.Marshal(result)
		if encodeErr != nil {
			return encodeErr
		}
		_, writeErr := fmt.Fprintln(output, string(encoded))
		return writeErr
	default:
		return fmt.Errorf("unsupported tenant log schema operation %q", operation)
	}
	_, err := fmt.Fprintln(output, "KKAI tenant log schema is ready")
	return err
}
