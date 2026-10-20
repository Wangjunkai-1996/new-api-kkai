package main

import (
	"context"
	"fmt"
	"io"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/kkaimigrate"
	"gorm.io/gorm"
)

func validateTenantOnlineInspection(operation string, jsonOutput, conflicting bool) error {
	if operation != "plan" && operation != "status" && operation != "check" {
		return fmt.Errorf("--tenant-expand expects the read-only operations plan, status, or check")
	}
	if !jsonOutput || conflicting {
		return fmt.Errorf("--tenant-expand requires --json and cannot be combined with other operations")
	}
	return nil
}

func validateTenantOnlineCommand(operation, proofPath string, jsonOutput, conflicting bool) error {
	if operation == "apply" || operation == "finalize" {
		if proofPath != tenantControllerProofPath || !jsonOutput || conflicting {
			return fmt.Errorf("online writes require --json and the fixed installed root-controller proof mount")
		}
		return nil
	}
	if proofPath != "" {
		return fmt.Errorf("read-only tenant inspection does not accept a controller proof")
	}
	return validateTenantOnlineInspection(operation, jsonOutput, conflicting)
}

// Read-only inspection never delegates to a mutating migration operation.
func runTenantOnlineInspection(ctx context.Context, db *gorm.DB, operation string, output io.Writer) error {
	if err := validateTenantOnlineInspection(operation, true, false); err != nil {
		return err
	}
	var status *kkaimigrate.TenantOnlineExpansionStatus
	var err error
	if operation == "plan" {
		status, err = kkaimigrate.PlanTenantOnlineExpansion(ctx, db)
	} else {
		status, err = kkaimigrate.ObserveTenantOnlineExpansion(ctx, db)
		if err == nil && operation == "check" {
			if status.CurrentVersion == kkaimigrate.TenantOnlineSchemaVersion || status.CurrentVersion == kkaimigrate.ResellerApplicationSchemaVersion {
				err = kkaimigrate.CheckTenantOnlineSchema(ctx, db)
			} else {
				err = kkaimigrate.CheckTenantOnlineExpansion(ctx, db)
			}
		}
	}
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
