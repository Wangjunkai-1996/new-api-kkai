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

const membershipPaymentControllerProofPath = "/run/membership-payment-migration/proof.json"

type membershipPaymentMigrationStatus struct {
	Schema                         string `json:"schema"`
	CurrentVersion                 int64  `json:"current_version"`
	TargetVersion                  int64  `json:"target_version"`
	MembershipPaymentSchemaPresent bool   `json:"membership_payment_schema_present"`
	Safe                           bool   `json:"safe"`
	SourceDigest                   string `json:"source_digest"`
	TargetDigest                   string `json:"target_digest"`
	PlanChecksum                   string `json:"plan_checksum"`
	DatabaseName                   string `json:"database_name"`
	DatabaseSchema                 string `json:"database_schema"`
}

type membershipPaymentRuntimeEvidence = shopRuntimeEvidence

// Membership payment mutation has an independent protocol and fixed proof mount.
// The root controller attests live process identities; application JSON is not
// a substitute for that root-owned read-only trust boundary.
type membershipPaymentControllerProof struct {
	tenantControllerProof
	TargetDigest string                             `json:"target_digest"`
	Runtimes     []membershipPaymentRuntimeEvidence `json:"runtimes"`
}

func validateMembershipPaymentMigrationCommand(operation, proofPath string, jsonOutput, conflicting bool) error {
	if !jsonOutput || conflicting {
		return fmt.Errorf("--membership-payment-migration requires --json and cannot be combined with other operations")
	}
	switch operation {
	case "plan", "status":
		if proofPath != "" {
			return fmt.Errorf("read-only membership payment inspection does not accept a controller proof")
		}
	case "apply":
		if proofPath != membershipPaymentControllerProofPath {
			return fmt.Errorf("membership payment mutation requires the fixed installed membership payment controller proof mount")
		}
	default:
		return fmt.Errorf("--membership-payment-migration expects plan, status, or apply")
	}
	return nil
}

func inspectMembershipPaymentMigration(ctx context.Context, db *gorm.DB) (*membershipPaymentMigrationStatus, error) {
	precheck, err := kkaimigrate.PrecheckMembershipPaymentExpand(ctx, db)
	if err != nil {
		return nil, err
	}
	present, err := kkaimigrate.MembershipPaymentSchemaObjectsPresent(db.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	if precheck.CurrentVersion == kkaimigrate.ShopSchemaVersion && present {
		return nil, fmt.Errorf("%w: unreceipted membership payment objects require operator repair", kkaimigrate.ErrSchemaNotReady)
	}
	if precheck.CurrentVersion == kkaimigrate.MembershipPaymentSchemaVersion {
		if err := kkaimigrate.CheckMembershipPaymentSchema(ctx, db); err != nil {
			return nil, err
		}
	}
	contract, err := kkaimigrate.ContractForDialect(db.Dialector.Name())
	if err != nil {
		return nil, err
	}
	if contract.RuntimeMinVersion != 14 || contract.RuntimeMaxVersion != 20 ||
		contract.MigrationTargetVersion != 14 || contract.MigrationKind != kkaimigrate.MigrationKindNone ||
		contract.MigrationSetDigest != contract.CompatiblePrefixes["14"] ||
		contract.CompatiblePrefixes["19"] == "" || contract.CompatiblePrefixes["20"] == "" {
		return nil, fmt.Errorf("%w: membership payment controller requires the exact v14-v20 compatibility runtime", kkaimigrate.ErrUnsafeMigration)
	}
	name, schema, err := kkaimigrate.TenantOnlineDatabaseIdentity(ctx, db)
	if err != nil {
		return nil, err
	}
	return &membershipPaymentMigrationStatus{
		Schema:                         "kkai_membership_payment_migration_status_v1",
		CurrentVersion:                 precheck.CurrentVersion,
		TargetVersion:                  precheck.TargetVersion,
		MembershipPaymentSchemaPresent: present,
		Safe:                           precheck.Safe,
		SourceDigest:                   contract.CompatiblePrefixes["19"],
		TargetDigest:                   contract.CompatiblePrefixes["20"],
		PlanChecksum:                   kkaimigrate.MembershipPaymentPlanChecksum(),
		DatabaseName:                   name,
		DatabaseSchema:                 schema,
	}, nil
}

func validateMembershipPaymentControllerProof(proof *membershipPaymentControllerProof, status *membershipPaymentMigrationStatus, now time.Time) error {
	expected, err := kkaimigrate.ContractForDialect(kkaimigrate.DialectPostgres)
	if err != nil {
		return err
	}
	if expected.RuntimeMinVersion != 14 || expected.RuntimeMaxVersion != 20 || expected.MigrationTargetVersion != 14 {
		return fmt.Errorf("membership payment controller requires the exact compiled v14-v20 compatibility runtime")
	}
	return validateMembershipPaymentProofAgainstContract(proof, status, now, expected)
}

// The installed protocol binds database and process evidence to one exact
// runtime contract. The production wrapper supplies only its compiled contract.
func validateMembershipPaymentProofAgainstContract(proof *membershipPaymentControllerProof, status *membershipPaymentMigrationStatus, now time.Time, expected kkaimigrate.SchemaContract) error {
	if proof == nil || status == nil || proof.Schema != "kkai_membership_payment_migration_proof_v1" || proof.Protocol != "membership-payment-migration-v1" || proof.Operation != "apply" {
		return fmt.Errorf("invalid membership payment controller proof protocol or operation")
	}
	if expected.RuntimeMinVersion != 14 || expected.RuntimeMaxVersion != 20 || expected.MigrationTargetVersion != 14 ||
		status.Schema != "kkai_membership_payment_migration_status_v1" || (status.CurrentVersion != 19 && status.CurrentVersion != 20) ||
		status.TargetVersion != 20 || !status.Safe ||
		status.SourceDigest != expected.CompatiblePrefixes["19"] || status.TargetDigest != expected.CompatiblePrefixes["20"] ||
		status.PlanChecksum != kkaimigrate.MembershipPaymentPlanChecksum() || status.MembershipPaymentSchemaPresent != (status.CurrentVersion == 20) {
		return fmt.Errorf("membership payment controller requires the exact canonical v19/v20 database and compiled plan")
	}
	if proof.SourceVersion != 19 || proof.TargetVersion != 20 || proof.SourceDigest != status.SourceDigest || proof.TargetDigest != status.TargetDigest || proof.PlanChecksum != status.PlanChecksum || proof.DatabaseName == "" || proof.DatabaseName != status.DatabaseName || proof.DatabaseSchema == "" || proof.DatabaseSchema != status.DatabaseSchema {
		return fmt.Errorf("membership payment controller proof does not match the exact database/source/compiled plan")
	}
	if len(proof.InstalledInfraSHA) != 40 || !tenantProofHex.MatchString(proof.InstalledInfraSHA) || !tenantProofImage(proof.ImageID) || len(proof.Nonce) != 32 || !tenantProofHex.MatchString(proof.Nonce) {
		return fmt.Errorf("invalid membership payment controller artifact identity")
	}
	if proof.IssuedAt <= 0 || proof.IssuedAt > now.Unix() || proof.ExpiresAt <= now.Unix() || proof.ExpiresAt <= proof.IssuedAt || proof.ExpiresAt-proof.IssuedAt > 120 {
		return fmt.Errorf("membership payment controller proof is expired or has an invalid validity window")
	}
	if len(proof.Runtimes) != 3 {
		return fmt.Errorf("membership payment controller proof requires active, rollback and writer runtime evidence")
	}
	roles, containers, processes := map[string]bool{}, map[string]bool{}, map[int64]bool{}
	for _, instance := range proof.Runtimes {
		if (instance.Role != "active" && instance.Role != "rollback" && instance.Role != "writer") || roles[instance.Role] || instance.Name == "" || len(instance.ContainerID) != 64 || !tenantProofHex.MatchString(instance.ContainerID) || containers[instance.ContainerID] || !tenantProofImage(instance.ImageID) || instance.Version == "" {
			return fmt.Errorf("membership payment controller runtime identities are invalid or repeated")
		}
		startedAt, err := time.Parse(time.RFC3339Nano, instance.ContainerStartedAt)
		if err != nil || startedAt.Unix() <= 0 || startedAt.Unix() > proof.IssuedAt || instance.ContainerInitStartTicks == 0 || instance.ApplicationPID <= 0 || processes[instance.ApplicationPID] || instance.ApplicationStartTicks < instance.ContainerInitStartTicks || !shopMountNamespace.MatchString(instance.ApplicationMountNamespace) || instance.ApplicationExecutableDevice == 0 || instance.ApplicationExecutableInode == 0 || len(instance.ApplicationCgroupSHA256) != 64 || !tenantProofHex.MatchString(instance.ApplicationCgroupSHA256) {
			return fmt.Errorf("membership payment controller runtime process identities are invalid or repeated")
		}
		if instance.TenantRoutingEnabled == nil || instance.TenantAware == nil || !*instance.TenantAware || instance.SeparateLogDatabase == nil || *instance.SeparateLogDatabase || instance.SchemaContract.SchemaManagement != "external" || !reflect.DeepEqual(instance.SchemaContract.SchemaContract, expected) {
			return fmt.Errorf("membership payment mutation requires exact v14-v20-compatible runtimes and main-database logs")
		}
		if instance.Role == "active" && instance.ImageID != proof.ImageID {
			return fmt.Errorf("membership payment migration image must match the active runtime image")
		}
		roles[instance.Role], containers[instance.ContainerID], processes[instance.ApplicationPID] = true, true, true
	}
	return nil
}

func runMembershipPaymentMigration(ctx context.Context, db *gorm.DB, operation, proofPath string, output io.Writer) error {
	if err := validateMembershipPaymentMigrationCommand(operation, proofPath, true, false); err != nil {
		return err
	}
	status, err := inspectMembershipPaymentMigration(ctx, db)
	if err != nil {
		return err
	}
	if operation == "apply" {
		if db.Dialector.Name() != kkaimigrate.DialectPostgres || common.SchemaManagementMode != common.SchemaManagementExternal {
			return fmt.Errorf("production membership payment mutation requires PostgreSQL >=12 and an external-schema image")
		}
		var serverVersion int
		if err := db.WithContext(ctx).Raw("SHOW server_version_num").Scan(&serverVersion).Error; err != nil {
			return err
		}
		if serverVersion < 120000 {
			return fmt.Errorf("production membership payment mutation requires PostgreSQL >=12")
		}
		raw, err := readRootControllerProofJSON(proofPath, membershipPaymentControllerProofPath)
		if err != nil {
			return err
		}
		var proof membershipPaymentControllerProof
		if err = common.Unmarshal(raw, &proof); err != nil {
			return fmt.Errorf("invalid membership payment controller proof JSON")
		}
		if err = validateMembershipPaymentControllerProof(&proof, status, time.Now()); err != nil {
			return err
		}
		guard := func(ctx context.Context, locked *gorm.DB) error {
			fresh, err := inspectMembershipPaymentMigration(ctx, locked)
			if err != nil {
				return err
			}
			return validateMembershipPaymentControllerProof(&proof, fresh, time.Now())
		}
		if _, err = kkaimigrate.ApplyMembershipPaymentExpand(ctx, db, kkaimigrate.Options{AuthorizeMutation: guard}); err != nil {
			return err
		}
		status, err = inspectMembershipPaymentMigration(ctx, db)
		if err != nil {
			return err
		}
		if status.CurrentVersion != kkaimigrate.MembershipPaymentSchemaVersion {
			return fmt.Errorf("%w: membership payment migration did not produce the target receipt", kkaimigrate.ErrSchemaNotReady)
		}
	}
	encoded, err := common.Marshal(status)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(output, string(encoded))
	return err
}
