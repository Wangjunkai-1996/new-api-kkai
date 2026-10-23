package main

import (
	"context"
	"fmt"
	"io"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/kkaimigrate"
	"gorm.io/gorm"
)

const walletPaymentControllerProofPath = "/run/wallet-payment-migration/proof.json"

type walletPaymentMigrationStatus struct {
	Schema              string `json:"schema"`
	CurrentVersion      int64  `json:"current_version"`
	TargetVersion       int64  `json:"target_version"`
	WalletSchemaPresent bool   `json:"wallet_schema_present"`
	Safe                bool   `json:"safe"`
	SourceDigest        string `json:"source_digest"`
	TargetDigest        string `json:"target_digest"`
	PlanChecksum        string `json:"plan_checksum"`
	DatabaseName        string `json:"database_name"`
	DatabaseSchema      string `json:"database_schema"`
}

// This compatibility release exposes canonical plan/status inspection only.
// Controlled mutation is introduced by the following release, after every live
// runtime and the rollback pair can safely read the expanded schema.
func validateWalletPaymentMigrationCommand(operation, proofPath string, jsonOutput, conflicting bool) error {
	if !jsonOutput || conflicting {
		return fmt.Errorf("--wallet-payment-migration requires --json and cannot be combined with other operations")
	}
	if proofPath != "" {
		return fmt.Errorf("read-only wallet payment inspection does not accept a controller proof")
	}
	switch operation {
	case "plan", "status":
		return nil
	case "apply":
		return fmt.Errorf("wallet payment mutation is unavailable in this read-only compatibility release")
	default:
		return fmt.Errorf("--wallet-payment-migration expects plan or status in this read-only compatibility release")
	}
}

func inspectWalletPaymentMigration(ctx context.Context, db *gorm.DB) (*walletPaymentMigrationStatus, error) {
	precheck, err := kkaimigrate.PrecheckWalletPaymentExpand(ctx, db)
	if err != nil {
		return nil, err
	}
	present, err := kkaimigrate.WalletPaymentSchemaObjectsPresent(db.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	if precheck.CurrentVersion == kkaimigrate.InvoiceSchemaVersion && present {
		return nil, fmt.Errorf("%w: unreceipted wallet payment objects require operator repair", kkaimigrate.ErrSchemaNotReady)
	}
	if precheck.CurrentVersion == kkaimigrate.WalletPaymentSchemaVersion {
		if err := kkaimigrate.CheckWalletPaymentSchema(ctx, db); err != nil {
			return nil, err
		}
	}
	contract, err := kkaimigrate.ContractForDialect(db.Dialector.Name())
	if err != nil {
		return nil, err
	}
	if contract.RuntimeMinVersion != 14 || contract.RuntimeMaxVersion != 18 ||
		contract.MigrationTargetVersion != 14 || contract.MigrationKind != kkaimigrate.MigrationKindNone ||
		contract.MigrationSetDigest != contract.CompatiblePrefixes["14"] ||
		contract.CompatiblePrefixes["17"] == "" || contract.CompatiblePrefixes["18"] == "" {
		return nil, fmt.Errorf("%w: wallet payment controller requires the exact v14-v18 compatibility runtime", kkaimigrate.ErrUnsafeMigration)
	}
	name, schema, err := kkaimigrate.TenantOnlineDatabaseIdentity(ctx, db)
	if err != nil {
		return nil, err
	}
	return &walletPaymentMigrationStatus{
		Schema:              "kkai_wallet_payment_migration_status_v1",
		CurrentVersion:      precheck.CurrentVersion,
		TargetVersion:       precheck.TargetVersion,
		WalletSchemaPresent: present,
		Safe:                precheck.Safe,
		SourceDigest:        contract.CompatiblePrefixes["17"],
		TargetDigest:        contract.CompatiblePrefixes["18"],
		PlanChecksum:        kkaimigrate.WalletPaymentPlanChecksum(),
		DatabaseName:        name,
		DatabaseSchema:      schema,
	}, nil
}

func runWalletPaymentMigration(ctx context.Context, db *gorm.DB, operation, proofPath string, output io.Writer) error {
	if err := validateWalletPaymentMigrationCommand(operation, proofPath, true, false); err != nil {
		return err
	}
	status, err := inspectWalletPaymentMigration(ctx, db)
	if err != nil {
		return err
	}
	encoded, err := common.Marshal(status)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(output, string(encoded))
	return err
}
