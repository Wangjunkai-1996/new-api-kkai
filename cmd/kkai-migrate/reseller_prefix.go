package main

import (
	"context"
	"fmt"
	"io"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/kkaimigrate"
	"gorm.io/gorm"
)

type resellerPrefixStatus struct {
	Schema         string `json:"schema"`
	CurrentVersion int64  `json:"current_version"`
	TargetVersion  int64  `json:"target_version"`
	ColumnPresent  bool   `json:"column_present"`
	Safe           bool   `json:"safe"`
	SourceDigest   string `json:"source_digest"`
	TargetDigest   string `json:"target_digest"`
	PlanChecksum   string `json:"plan_checksum"`
	DatabaseName   string `json:"database_name"`
	DatabaseSchema string `json:"database_schema"`
}

func validateResellerPrefixCommand(operation, proofPath string, jsonOutput, conflicting bool) error {
	if !jsonOutput || conflicting {
		return fmt.Errorf("--reseller-prefix requires --json and cannot be combined with other operations")
	}
	switch operation {
	case "plan", "status":
		if proofPath != "" {
			return fmt.Errorf("read-only prefix inspection does not accept a controller proof")
		}
	case "apply":
		return fmt.Errorf("prefix mutation is unavailable in this read-only compatibility release")
	default:
		return fmt.Errorf("--reseller-prefix expects plan or status")
	}
	return nil
}

func inspectResellerPrefix(ctx context.Context, db *gorm.DB) (*resellerPrefixStatus, error) {
	precheck, err := kkaimigrate.PrecheckResellerApplicationExpand(ctx, db)
	if err != nil {
		return nil, err
	}
	if precheck.CurrentVersion == kkaimigrate.TenantOnlineSchemaVersion && precheck.ColumnPresent {
		return nil, fmt.Errorf("%w: partial application-prefix schema has no canonical receipt", kkaimigrate.ErrSchemaNotReady)
	}
	contract, err := kkaimigrate.ContractForDialect(db.Dialector.Name())
	if err != nil {
		return nil, err
	}
	if contract.RuntimeMinVersion != 14 || contract.RuntimeMaxVersion != 15 || contract.MigrationTargetVersion != 14 || contract.CompatiblePrefixes["14"] == "" || contract.CompatiblePrefixes["15"] == "" {
		return nil, fmt.Errorf("%w: prefix controller requires the v14/v15 compatibility runtime", kkaimigrate.ErrUnsafeMigration)
	}
	name, schema, err := kkaimigrate.TenantOnlineDatabaseIdentity(ctx, db)
	if err != nil {
		return nil, err
	}
	return &resellerPrefixStatus{
		Schema: "kkai_reseller_prefix_v1", CurrentVersion: precheck.CurrentVersion,
		TargetVersion: precheck.TargetVersion, ColumnPresent: precheck.ColumnPresent, Safe: precheck.Safe,
		SourceDigest: contract.CompatiblePrefixes["14"], TargetDigest: contract.CompatiblePrefixes["15"],
		PlanChecksum: kkaimigrate.ResellerApplicationPlanChecksum(), DatabaseName: name, DatabaseSchema: schema,
	}, nil
}

func runResellerPrefix(ctx context.Context, db *gorm.DB, operation, proofPath string, output io.Writer) error {
	if err := validateResellerPrefixCommand(operation, proofPath, true, false); err != nil {
		return err
	}
	status, err := inspectResellerPrefix(ctx, db)
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
