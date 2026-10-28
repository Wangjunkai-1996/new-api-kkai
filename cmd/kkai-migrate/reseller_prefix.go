package main

import (
	"context"
	"fmt"
	"io"
	"reflect"
	"time"

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

type resellerPrefixRuntimeEvidence struct {
	tenantRuntimeEvidence
	SchemaContract struct {
		kkaimigrate.SchemaContract
		SchemaManagement string `json:"schema_management"`
	} `json:"schema_contract"`
}

type resellerPrefixControllerProof struct {
	tenantControllerProof
	TargetDigest string                          `json:"target_digest"`
	Runtimes     []resellerPrefixRuntimeEvidence `json:"runtimes"`
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
		if proofPath != tenantControllerProofPath {
			return fmt.Errorf("prefix mutation requires the fixed installed root-controller proof mount")
		}
	default:
		return fmt.Errorf("--reseller-prefix expects plan, status, or apply")
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
	if contract.RuntimeMinVersion != 14 || (contract.RuntimeMaxVersion != 15 && contract.RuntimeMaxVersion != 16 && contract.RuntimeMaxVersion != 17 && contract.RuntimeMaxVersion != 18 && contract.RuntimeMaxVersion != 19) || contract.MigrationTargetVersion != 14 || contract.CompatiblePrefixes["14"] == "" || contract.CompatiblePrefixes["15"] == "" {
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

func validateResellerPrefixProof(proof *resellerPrefixControllerProof, status *resellerPrefixStatus, now time.Time) error {
	if proof == nil || status == nil || proof.Schema != "kkai_reseller_prefix_proof_v1" || proof.Protocol != "reseller-prefix-v1" || proof.Operation != "apply" {
		return fmt.Errorf("invalid prefix controller proof protocol or operation")
	}
	if proof.SourceVersion != 14 || proof.TargetVersion != 15 || proof.SourceDigest != status.SourceDigest || proof.TargetDigest != status.TargetDigest || proof.PlanChecksum != status.PlanChecksum || proof.DatabaseName == "" || proof.DatabaseName != status.DatabaseName || proof.DatabaseSchema != status.DatabaseSchema {
		return fmt.Errorf("prefix controller proof does not match the exact database/source/compiled plan")
	}
	if len(proof.InstalledInfraSHA) != 40 || !tenantProofHex.MatchString(proof.InstalledInfraSHA) || !tenantProofImage(proof.ImageID) || len(proof.Nonce) != 32 || !tenantProofHex.MatchString(proof.Nonce) {
		return fmt.Errorf("invalid prefix controller artifact identity")
	}
	if proof.IssuedAt <= 0 || proof.IssuedAt > now.Unix() || proof.ExpiresAt <= now.Unix() || proof.ExpiresAt <= proof.IssuedAt || proof.ExpiresAt-proof.IssuedAt > 120 {
		return fmt.Errorf("prefix controller proof is expired or has an invalid validity window")
	}
	if len(proof.Runtimes) != 3 {
		return fmt.Errorf("prefix controller proof requires active, rollback and writer runtime evidence")
	}
	expected := kkaimigrate.SchemaContract{
		RuntimeMinVersion: 14, RuntimeMaxVersion: 15, MigrationTargetVersion: 14, MigrationKind: kkaimigrate.MigrationKindNone,
		MigrationSetDigest: status.SourceDigest, CompatiblePrefixes: map[string]string{"14": status.SourceDigest, "15": status.TargetDigest},
	}
	roles, containers := map[string]bool{}, map[string]bool{}
	for _, instance := range proof.Runtimes {
		if (instance.Role != "active" && instance.Role != "rollback" && instance.Role != "writer") || roles[instance.Role] || instance.Name == "" || len(instance.ContainerID) != 64 || !tenantProofHex.MatchString(instance.ContainerID) || containers[instance.ContainerID] || !tenantProofImage(instance.ImageID) || instance.Version == "" {
			return fmt.Errorf("prefix controller runtime identities are invalid or repeated")
		}
		if instance.TenantRoutingEnabled == nil || instance.TenantAware == nil || !*instance.TenantAware || instance.SeparateLogDatabase == nil || *instance.SeparateLogDatabase || instance.SchemaContract.SchemaManagement != "external" || !reflect.DeepEqual(instance.SchemaContract.SchemaContract, expected) {
			return fmt.Errorf("prefix mutation requires v14/v15-compatible runtimes and main-database logs")
		}
		roles[instance.Role], containers[instance.ContainerID] = true, true
	}
	return nil
}

func runResellerPrefix(ctx context.Context, db *gorm.DB, operation, proofPath string, output io.Writer) error {
	if err := validateResellerPrefixCommand(operation, proofPath, true, false); err != nil {
		return err
	}
	status, err := inspectResellerPrefix(ctx, db)
	if err != nil {
		return err
	}
	if operation == "apply" {
		if db.Dialector.Name() != kkaimigrate.DialectPostgres {
			return fmt.Errorf("production prefix mutation requires PostgreSQL >=12")
		}
		raw, err := readTenantControllerProofJSON(proofPath)
		if err != nil {
			return err
		}
		var proof resellerPrefixControllerProof
		if err = common.Unmarshal(raw, &proof); err != nil {
			return fmt.Errorf("invalid prefix controller proof JSON")
		}
		if err = validateResellerPrefixProof(&proof, status, time.Now()); err != nil {
			return err
		}
		guard := func(ctx context.Context, locked *gorm.DB) error {
			name, schema, err := kkaimigrate.TenantOnlineDatabaseIdentity(ctx, locked)
			if err != nil {
				return err
			}
			if name != status.DatabaseName || schema != status.DatabaseSchema {
				return fmt.Errorf("prefix controller database identity changed")
			}
			return validateResellerPrefixProof(&proof, status, time.Now())
		}
		if _, err = kkaimigrate.ApplyResellerApplicationExpand(ctx, db, kkaimigrate.Options{AuthorizeMutation: guard}); err != nil {
			return err
		}
		status, err = inspectResellerPrefix(ctx, db)
		if err != nil {
			return err
		}
	}
	encoded, err := common.Marshal(status)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(output, string(encoded))
	return err
}
