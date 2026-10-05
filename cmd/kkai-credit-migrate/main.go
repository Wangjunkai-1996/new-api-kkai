package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/kkaischemacli"
	"github.com/QuantumNous/new-api/pkg/creditmigration"
	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
	if len(os.Args) < 2 {
		fatal("usage: kkai-credit-migrate <inventory|plan|expression-prices|pricing-plan|pricing-check|apply|verify|restore|receipt-schema> [flags]")
	}
	command := os.Args[1]
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	dsn := fs.String("dsn", os.Getenv("KKAI_MIGRATION_DSN"), "offline migration database DSN (required; never defaults to SQL_DSN)")
	dsnFromStdin := fs.Bool("dsn-stdin", false, "read one offline migration DSN from stdin")
	dialect := fs.String("dialect", "", "database dialect: sqlite, postgres, or mysql (inferred from URL when omitted)")
	receiptTable := fs.String("receipt-table", creditmigration.DefaultReceiptTable, "pre-created receipt table")
	migrationID := fs.String("migration-id", "", "unique migration ID")
	planPath := fs.String("plan", "", "plan JSON path")
	pricingInventoryPath := fs.String("pricing-inventory", "", "reviewed pricing inventory JSON for pricing-plan")
	expressionSource := fs.String("expression-source", "", "read-only snapshot containing billing_options for expression-prices")
	pricingPlanPath := fs.String("pricing-plan", "", "validated pricing plan JSON path (required for apply)")
	outPath := fs.String("out", "", "write JSON result to this path")
	contractPath := fs.String("offline-maintenance-contract", "", "explicit offline maintenance contract JSON")
	timeout := fs.Duration("timeout", 5*time.Minute, "operation timeout")
	if err := fs.Parse(os.Args[2:]); err != nil {
		os.Exit(2)
	}
	if command == "receipt-schema" {
		schemaDialect := *dialect
		if schemaDialect == "" {
			schemaDialect = "sqlite"
		}
		if len(fs.Args()) > 0 {
			schemaDialect = fs.Args()[0]
		}
		fmt.Print(creditmigration.ReceiptTableSchema(*receiptTable, schemaDialect))
		return
	}
	if command == "pricing-check" {
		result, err := checkPricingPlan(readPricingPlan(*pricingPlanPath))
		if err != nil {
			fatal("pricing check: %v", err)
		}
		writeJSON(result, *outPath)
		return
	}
	if command == "expression-prices" {
		data, err := os.ReadFile(*expressionSource)
		if err != nil {
			fatal("read expression source: %v", err)
		}
		var snapshot struct {
			Options map[string]string `json:"billing_options"`
		}
		if err := common.Unmarshal(data, &snapshot); err != nil {
			fatal("decode expression source: %v", err)
		}
		var expressions map[string]string
		if err := common.UnmarshalJsonStr(snapshot.Options["billing_setting.billing_expr"], &expressions); err != nil {
			fatal("decode expression map: %v", err)
		}
		for model, source := range expressions {
			target, err := creditmigration.DivideMonetaryExpression(source)
			if err != nil {
				fatal("transform expression %s: %v", model, err)
			}
			expressions[model] = target
		}
		writeJSON(expressions, *outPath)
		return
	}
	if command == "pricing-plan" {
		if strings.TrimSpace(*pricingInventoryPath) == "" {
			fatal("--pricing-inventory is required")
		}
		data, err := os.ReadFile(*pricingInventoryPath)
		if err != nil {
			fatal("read pricing inventory: %v", err)
		}
		var inventory creditmigration.PricingInventory
		if err := creditmigration.Unmarshal(data, &inventory); err != nil {
			fatal("decode pricing inventory: %v", err)
		}
		plan, err := creditmigration.BuildPricingPlan(inventory)
		if err != nil {
			fatal("build pricing plan: %v", err)
		}
		if len(plan.Options) == 0 {
			fatal("pricing inventory must contain reviewed option before/after values")
		}
		if err := creditmigration.ValidatePricingPlan(plan); err != nil {
			fatal("pricing plan: %v", err)
		}
		writeJSON(plan, *outPath)
		return
	}
	resolvedDSN, err := kkaischemacli.ResolveDSN(*dsn, *dsnFromStdin, os.Stdin, "--dsn, --dsn-stdin or KKAI_MIGRATION_DSN is required; refusing to use the application's SQL_DSN")
	if err != nil {
		fatal("migration connection: %v", err)
	}
	db, err := openDatabase(resolvedDSN, *dialect)
	if err != nil {
		fatal("open database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		fatal("open SQL connection: %v", err)
	}
	defer sqlDB.Close()
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	switch command {
	case "inventory":
		requireID(*migrationID)
		result, err := creditmigration.InventoryDB(ctx, db, *migrationID, *receiptTable)
		if err != nil {
			fatal("inventory: %v", err)
		}
		writeJSON(result, *outPath)
	case "plan":
		requireID(*migrationID)
		result, err := creditmigration.BuildPlan(ctx, db, *migrationID, *receiptTable)
		if err != nil {
			fatal("plan: %v", err)
		}
		writeJSON(result, *outPath)
	case "apply":
		p := readPlan(*planPath)
		if strings.TrimSpace(*pricingPlanPath) == "" {
			fatal("--pricing-plan is required for apply; wallet conversion cannot run without a completed pricing plan")
		}
		pricingPlan := readPricingPlan(*pricingPlanPath)
		if pricingPlan.MigrationID != p.MigrationID {
			fatal("pricing plan migration ID %q does not match wallet plan %q", pricingPlan.MigrationID, p.MigrationID)
		}
		if err := creditmigration.ValidatePricingPlan(pricingPlan); err != nil {
			fatal("pricing plan: %v", err)
		}
		c := readContract(*contractPath, p.MigrationID)
		if err := creditmigration.Apply(ctx, db, p, c, pricingPlan); err != nil {
			fatal("apply: %v", err)
		}
		fmt.Println("applied", p.MigrationID)
	case "verify":
		p := readPlan(*planPath)
		if err := creditmigration.Verify(ctx, db, p); err != nil {
			fatal("verify: %v", err)
		}
		if *outPath != "" {
			var receipt creditmigration.Receipt
			if err := db.Table(p.ReceiptTable).Select("migration_id, plan_hash, pricing_plan_hash, state").Where("migration_id = ?", p.MigrationID).Take(&receipt).Error; err != nil {
				fatal("read verified receipt: %v", err)
			}
			writeJSON(map[string]any{"status": "verified", "scope": "main_wallet_and_options", "migration_id": receipt.MigrationID, "plan_hash": receipt.PlanHash, "pricing_plan_hash": receipt.PricingPlanHash, "receipt_state": receipt.State, "verified_at": time.Now().UTC().Format(time.RFC3339)}, *outPath)
			return
		}
		fmt.Println("verified", p.MigrationID)
	case "restore":
		requireID(*migrationID)
		c := readContract(*contractPath, *migrationID)
		if err := creditmigration.Restore(ctx, db, *migrationID, *receiptTable, c); err != nil {
			fatal("restore: %v", err)
		}
		fmt.Println("restored", *migrationID)
	default:
		fatal("unknown operation %q", command)
	}
}

func openDatabase(dsn, dialect string) (*gorm.DB, error) {
	lower := strings.ToLower(dsn)
	if strings.EqualFold(dialect, "postgres") || strings.EqualFold(dialect, "postgresql") {
		return gorm.Open(postgres.Open(dsn), &gorm.Config{})
	}
	if strings.EqualFold(dialect, "mysql") {
		return gorm.Open(mysql.Open(strings.TrimPrefix(dsn, "mysql://")), &gorm.Config{})
	}
	if dialect != "" && !strings.EqualFold(dialect, "sqlite") {
		return nil, fmt.Errorf("unsupported database dialect %q", dialect)
	}
	switch {
	case strings.HasPrefix(lower, "postgres://"), strings.HasPrefix(lower, "postgresql://"):
		return gorm.Open(postgres.Open(dsn), &gorm.Config{})
	case strings.HasPrefix(lower, "mysql://"):
		return gorm.Open(mysql.Open(strings.TrimPrefix(dsn, "mysql://")), &gorm.Config{})
	default:
		if strings.HasPrefix(lower, "sqlite://") {
			dsn = strings.TrimPrefix(dsn, "sqlite://")
		}
		return gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	}
}

func readPlan(path string) creditmigration.Plan {
	if strings.TrimSpace(path) == "" {
		fatal("--plan is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		fatal("read plan: %v", err)
	}
	var p creditmigration.Plan
	if err := creditmigration.Unmarshal(data, &p); err != nil {
		fatal("decode plan: %v", err)
	}
	return p
}

func readPricingPlan(path string) creditmigration.PricingPlan {
	data, err := os.ReadFile(path)
	if err != nil {
		fatal("read pricing plan: %v", err)
	}
	var plan creditmigration.PricingPlan
	if err := creditmigration.Unmarshal(data, &plan); err != nil {
		fatal("decode pricing plan: %v", err)
	}
	return plan
}

func readContract(path, migrationID string) creditmigration.MaintenanceContract {
	if strings.TrimSpace(path) == "" {
		fatal("--offline-maintenance-contract is required for write operations")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		fatal("read maintenance contract: %v", err)
	}
	var c creditmigration.MaintenanceContract
	if err := creditmigration.Unmarshal(data, &c); err != nil {
		fatal("decode maintenance contract: %v", err)
	}
	if err := c.Validate(migrationID); err != nil {
		fatal("maintenance contract: %v", err)
	}
	return c
}

func writeJSON(value any, path string) {
	data, err := common.Marshal(value)
	if err != nil {
		fatal("encode JSON: %v", err)
	}
	data = append(data, '\n')
	if path == "" {
		_, _ = os.Stdout.Write(data)
		return
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		fatal("open JSON output: %v", err)
	}
	defer file.Close()
	if err := file.Chmod(0600); err != nil {
		fatal("protect JSON output: %v", err)
	}
	if _, err := file.Write(data); err != nil {
		fatal("write JSON: %v", err)
	}
}

func requireID(id string) {
	if strings.TrimSpace(id) == "" {
		fatal("--migration-id is required")
	}
}
func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
