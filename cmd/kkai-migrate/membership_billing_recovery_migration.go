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

const membershipBillingRecoveryControllerProofPath = "/run/membership-billing-recovery-migration/proof.json"

type membershipBillingRecoveryMigrationStatus struct {
	Schema                                 string `json:"schema"`
	CurrentVersion                         int64  `json:"current_version"`
	TargetVersion                          int64  `json:"target_version"`
	MembershipBillingRecoverySchemaPresent bool   `json:"membership_billing_recovery_schema_present"`
	Safe                                   bool   `json:"safe"`
	SourceDigest                           string `json:"source_digest"`
	TargetDigest                           string `json:"target_digest"`
	PlanChecksum                           string `json:"plan_checksum"`
	DatabaseName                           string `json:"database_name"`
	DatabaseSchema                         string `json:"database_schema"`
}

type membershipBillingRecoveryRuntimeEvidence = shopRuntimeEvidence

// Membership billing recovery mutation has an independent protocol and fixed proof mount.
// The root controller attests live process identities; application JSON is not
// a substitute for that root-owned read-only trust boundary.
type membershipBillingRecoveryControllerProof struct {
	tenantControllerProof
	TargetDigest string                                     `json:"target_digest"`
	Runtimes     []membershipBillingRecoveryRuntimeEvidence `json:"runtimes"`
}

func validateMembershipBillingRecoveryMigrationCommand(operation, proofPath string, jsonOutput, conflicting bool) error {
	if !jsonOutput || conflicting {
		return fmt.Errorf("--membership-billing-recovery-migration requires --json and cannot be combined with other operations")
	}
	switch operation {
	case "plan", "status":
		if proofPath != "" {
			return fmt.Errorf("read-only membership billing recovery inspection does not accept a controller proof")
		}
	case "apply":
		if proofPath != membershipBillingRecoveryControllerProofPath {
			return fmt.Errorf("membership billing recovery mutation requires the fixed installed membership billing recovery controller proof mount")
		}
	default:
		return fmt.Errorf("--membership-billing-recovery-migration expects plan, status, or apply")
	}
	return nil
}

func inspectMembershipBillingRecoveryMigration(ctx context.Context, db *gorm.DB) (*membershipBillingRecoveryMigrationStatus, error) {
	precheck, err := kkaimigrate.PrecheckMembershipBillingRecoveryExpand(ctx, db)
	if err != nil {
		return nil, err
	}
	present, err := kkaimigrate.MembershipBillingRecoverySchemaObjectsPresent(db.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	if precheck.CurrentVersion == kkaimigrate.MembershipPaymentSchemaVersion && present {
		return nil, fmt.Errorf("%w: unreceipted membership billing recovery objects require operator repair", kkaimigrate.ErrSchemaNotReady)
	}
	if precheck.CurrentVersion == kkaimigrate.MembershipBillingRecoverySchemaVersion {
		if err := kkaimigrate.CheckMembershipBillingRecoverySchema(ctx, db); err != nil {
			return nil, err
		}
	}
	contract, err := kkaimigrate.ContractForDialect(db.Dialector.Name())
	if err != nil {
		return nil, err
	}
	if contract.RuntimeMinVersion != 14 || contract.RuntimeMaxVersion != 21 ||
		contract.MigrationTargetVersion != 14 || contract.MigrationKind != kkaimigrate.MigrationKindNone ||
		contract.MigrationSetDigest != contract.CompatiblePrefixes["14"] ||
		contract.CompatiblePrefixes["20"] == "" || contract.CompatiblePrefixes["21"] == "" {
		return nil, fmt.Errorf("%w: membership billing recovery controller requires the exact v14-v21 compatibility runtime", kkaimigrate.ErrUnsafeMigration)
	}
	name, schema, err := kkaimigrate.TenantOnlineDatabaseIdentity(ctx, db)
	if err != nil {
		return nil, err
	}
	return &membershipBillingRecoveryMigrationStatus{
		Schema:                                 "kkai_membership_billing_recovery_migration_status_v1",
		CurrentVersion:                         precheck.CurrentVersion,
		TargetVersion:                          precheck.TargetVersion,
		MembershipBillingRecoverySchemaPresent: present,
		Safe:                                   precheck.Safe,
		SourceDigest:                           contract.CompatiblePrefixes["20"],
		TargetDigest:                           contract.CompatiblePrefixes["21"],
		PlanChecksum:                           kkaimigrate.MembershipBillingRecoveryPlanChecksum(),
		DatabaseName:                           name,
		DatabaseSchema:                         schema,
	}, nil
}

func validateMembershipBillingRecoveryControllerProof(proof *membershipBillingRecoveryControllerProof, status *membershipBillingRecoveryMigrationStatus, now time.Time) error {
	expected, err := kkaimigrate.ContractForDialect(kkaimigrate.DialectPostgres)
	if err != nil {
		return err
	}
	if expected.RuntimeMinVersion != 14 || expected.RuntimeMaxVersion != 21 || expected.MigrationTargetVersion != 14 {
		return fmt.Errorf("membership billing recovery controller requires the exact compiled v14-v21 compatibility runtime")
	}

	if proof == nil || status == nil || proof.Schema != "kkai_membership_billing_recovery_migration_proof_v1" || proof.Protocol != "membership-billing-recovery-migration-v1" || proof.Operation != "apply" {
		return fmt.Errorf("invalid membership billing recovery controller proof protocol or operation")
	}
	if status.Schema != "kkai_membership_billing_recovery_migration_status_v1" || (status.CurrentVersion != 20 && status.CurrentVersion != 21) ||
		status.TargetVersion != 21 || !status.Safe ||
		status.SourceDigest != expected.CompatiblePrefixes["20"] || status.TargetDigest != expected.CompatiblePrefixes["21"] ||
		status.PlanChecksum != kkaimigrate.MembershipBillingRecoveryPlanChecksum() || status.MembershipBillingRecoverySchemaPresent != (status.CurrentVersion == 21) {
		return fmt.Errorf("membership billing recovery controller requires the exact canonical v20/v21 database and compiled plan")
	}
	if proof.SourceVersion != 20 || proof.TargetVersion != 21 || proof.SourceDigest != status.SourceDigest || proof.TargetDigest != status.TargetDigest || proof.PlanChecksum != status.PlanChecksum || proof.DatabaseName == "" || proof.DatabaseName != status.DatabaseName || proof.DatabaseSchema == "" || proof.DatabaseSchema != status.DatabaseSchema {
		return fmt.Errorf("membership billing recovery controller proof does not match the exact database/source/compiled plan")
	}
	if len(proof.InstalledInfraSHA) != 40 || !tenantProofHex.MatchString(proof.InstalledInfraSHA) || !tenantProofImage(proof.ImageID) || len(proof.Nonce) != 32 || !tenantProofHex.MatchString(proof.Nonce) {
		return fmt.Errorf("invalid membership billing recovery controller artifact identity")
	}
	if proof.IssuedAt <= 0 || proof.IssuedAt > now.Unix() || proof.ExpiresAt <= now.Unix() || proof.ExpiresAt <= proof.IssuedAt || proof.ExpiresAt-proof.IssuedAt > 120 {
		return fmt.Errorf("membership billing recovery controller proof is expired or has an invalid validity window")
	}
	if len(proof.Runtimes) != 3 {
		return fmt.Errorf("membership billing recovery controller proof requires active, rollback and writer runtime evidence")
	}
	roles, containers, processes := map[string]bool{}, map[string]bool{}, map[int64]bool{}
	for _, instance := range proof.Runtimes {
		if (instance.Role != "active" && instance.Role != "rollback" && instance.Role != "writer") || roles[instance.Role] || instance.Name == "" || len(instance.ContainerID) != 64 || !tenantProofHex.MatchString(instance.ContainerID) || containers[instance.ContainerID] || !tenantProofImage(instance.ImageID) || instance.Version == "" {
			return fmt.Errorf("membership billing recovery controller runtime identities are invalid or repeated")
		}
		startedAt, err := time.Parse(time.RFC3339Nano, instance.ContainerStartedAt)
		if err != nil || startedAt.Unix() <= 0 || startedAt.Unix() > proof.IssuedAt || instance.ContainerInitStartTicks == 0 || instance.ApplicationPID <= 0 || processes[instance.ApplicationPID] || instance.ApplicationStartTicks < instance.ContainerInitStartTicks || !shopMountNamespace.MatchString(instance.ApplicationMountNamespace) || instance.ApplicationExecutableDevice == 0 || instance.ApplicationExecutableInode == 0 || len(instance.ApplicationCgroupSHA256) != 64 || !tenantProofHex.MatchString(instance.ApplicationCgroupSHA256) {
			return fmt.Errorf("membership billing recovery controller runtime process identities are invalid or repeated")
		}
		if instance.TenantRoutingEnabled == nil || instance.TenantAware == nil || !*instance.TenantAware || instance.SeparateLogDatabase == nil || *instance.SeparateLogDatabase || instance.SchemaContract.SchemaManagement != "external" || !reflect.DeepEqual(instance.SchemaContract.SchemaContract, expected) {
			return fmt.Errorf("membership billing recovery mutation requires exact v14-v21-compatible runtimes and main-database logs")
		}
		if instance.Role == "active" && instance.ImageID != proof.ImageID {
			return fmt.Errorf("membership billing recovery migration image must match the active runtime image")
		}
		roles[instance.Role], containers[instance.ContainerID], processes[instance.ApplicationPID] = true, true, true
	}
	return nil
}

func runMembershipBillingRecoveryMigration(ctx context.Context, db *gorm.DB, operation, proofPath string, output io.Writer) error {
	if err := validateMembershipBillingRecoveryMigrationCommand(operation, proofPath, true, false); err != nil {
		return err
	}
	status, err := inspectMembershipBillingRecoveryMigration(ctx, db)
	if err != nil {
		return err
	}
	if operation == "apply" {
		if db.Dialector.Name() != kkaimigrate.DialectPostgres || common.SchemaManagementMode != common.SchemaManagementExternal {
			return fmt.Errorf("production membership billing recovery mutation requires PostgreSQL >=12 and an external-schema image")
		}
		var serverVersion int
		if err := db.WithContext(ctx).Raw("SHOW server_version_num").Scan(&serverVersion).Error; err != nil {
			return err
		}
		if serverVersion < 120000 {
			return fmt.Errorf("production membership billing recovery mutation requires PostgreSQL >=12")
		}
		raw, err := readRootControllerProofJSON(proofPath, membershipBillingRecoveryControllerProofPath)
		if err != nil {
			return err
		}
		var proof membershipBillingRecoveryControllerProof
		if err = common.Unmarshal(raw, &proof); err != nil {
			return fmt.Errorf("invalid membership billing recovery controller proof JSON")
		}
		if err = validateMembershipBillingRecoveryControllerProof(&proof, status, time.Now()); err != nil {
			return err
		}
		guard := func(ctx context.Context, locked *gorm.DB) error {
			fresh, err := inspectMembershipBillingRecoveryMigration(ctx, locked)
			if err != nil {
				return err
			}
			return validateMembershipBillingRecoveryControllerProof(&proof, fresh, time.Now())
		}
		if _, err = kkaimigrate.ApplyMembershipBillingRecoveryExpand(ctx, db, kkaimigrate.Options{AuthorizeMutation: guard}); err != nil {
			return err
		}
		status, err = inspectMembershipBillingRecoveryMigration(ctx, db)
		if err != nil {
			return err
		}
		if status.CurrentVersion != kkaimigrate.MembershipBillingRecoverySchemaVersion {
			return fmt.Errorf("%w: membership billing recovery migration did not produce the target receipt", kkaimigrate.ErrSchemaNotReady)
		}
	}
	encoded, err := common.Marshal(status)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(output, string(encoded))
	return err
}
