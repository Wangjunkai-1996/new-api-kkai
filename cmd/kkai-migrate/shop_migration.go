package main

import (
	"context"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/kkaimigrate"
	"gorm.io/gorm"
)

const shopControllerProofPath = "/run/shop-migration/proof.json"

type shopMigrationStatus struct {
	Schema            string `json:"schema"`
	CurrentVersion    int64  `json:"current_version"`
	TargetVersion     int64  `json:"target_version"`
	ShopSchemaPresent bool   `json:"shop_schema_present"`
	Safe              bool   `json:"safe"`
	SourceDigest      string `json:"source_digest"`
	TargetDigest      string `json:"target_digest"`
	PlanChecksum      string `json:"plan_checksum"`
	DatabaseName      string `json:"database_name"`
	DatabaseSchema    string `json:"database_schema"`
}

type shopRuntimeEvidence struct {
	tenantRuntimeEvidence
	ContainerStartedAt          string `json:"container_started_at"`
	ContainerInitStartTicks     uint64 `json:"container_init_start_ticks"`
	ApplicationPID              int64  `json:"application_pid"`
	ApplicationStartTicks       uint64 `json:"application_start_ticks"`
	ApplicationMountNamespace   string `json:"application_mount_namespace"`
	ApplicationExecutableDevice uint64 `json:"application_executable_device"`
	ApplicationExecutableInode  uint64 `json:"application_executable_inode"`
	ApplicationCgroupSHA256     string `json:"application_cgroup_sha256"`
	SchemaContract              struct {
		kkaimigrate.SchemaContract
		SchemaManagement string `json:"schema_management"`
	} `json:"schema_contract"`
}

// Shop mutation has an independent protocol and fixed proof mount.
// The root controller attests live process identities; application JSON is not
// a substitute for that root-owned read-only trust boundary.
type shopControllerProof struct {
	tenantControllerProof
	TargetDigest string                `json:"target_digest"`
	Runtimes     []shopRuntimeEvidence `json:"runtimes"`
}

var shopMountNamespace = regexp.MustCompile(`^mnt:\[[1-9][0-9]*\]$`)

func validateShopMigrationCommand(operation, proofPath string, jsonOutput, conflicting bool) error {
	if !jsonOutput || conflicting {
		return fmt.Errorf("--shop-migration requires --json and cannot be combined with other operations")
	}
	switch operation {
	case "plan", "status":
		if proofPath != "" {
			return fmt.Errorf("read-only shop inspection does not accept a controller proof")
		}
	case "apply":
		if proofPath != shopControllerProofPath {
			return fmt.Errorf("shop mutation requires the fixed installed shop controller proof mount")
		}
	default:
		return fmt.Errorf("--shop-migration expects plan, status, or apply")
	}
	return nil
}

func inspectShopMigration(ctx context.Context, db *gorm.DB) (*shopMigrationStatus, error) {
	precheck, err := kkaimigrate.PrecheckShopExpand(ctx, db)
	if err != nil {
		return nil, err
	}
	present, err := kkaimigrate.ShopSchemaObjectsPresent(db.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	if precheck.CurrentVersion == kkaimigrate.WalletPaymentSchemaVersion && present {
		return nil, fmt.Errorf("%w: unreceipted shop objects require operator repair", kkaimigrate.ErrSchemaNotReady)
	}
	if precheck.CurrentVersion == kkaimigrate.ShopSchemaVersion {
		if err := kkaimigrate.CheckShopSchema(ctx, db); err != nil {
			return nil, err
		}
	}
	contract, err := kkaimigrate.ContractForDialect(db.Dialector.Name())
	if err != nil {
		return nil, err
	}
	if contract.RuntimeMinVersion != 14 || contract.RuntimeMaxVersion != 19 ||
		contract.MigrationTargetVersion != 14 || contract.MigrationKind != kkaimigrate.MigrationKindNone ||
		contract.MigrationSetDigest != contract.CompatiblePrefixes["14"] ||
		contract.CompatiblePrefixes["18"] == "" || contract.CompatiblePrefixes["19"] == "" {
		return nil, fmt.Errorf("%w: shop controller requires the exact v14-v19 compatibility runtime", kkaimigrate.ErrUnsafeMigration)
	}
	name, schema, err := kkaimigrate.TenantOnlineDatabaseIdentity(ctx, db)
	if err != nil {
		return nil, err
	}
	return &shopMigrationStatus{
		Schema:            "kkai_shop_migration_status_v1",
		CurrentVersion:    precheck.CurrentVersion,
		TargetVersion:     precheck.TargetVersion,
		ShopSchemaPresent: present,
		Safe:              precheck.Safe,
		SourceDigest:      contract.CompatiblePrefixes["18"],
		TargetDigest:      contract.CompatiblePrefixes["19"],
		PlanChecksum:      kkaimigrate.ShopPlanChecksum(),
		DatabaseName:      name,
		DatabaseSchema:    schema,
	}, nil
}

func validateShopControllerProof(proof *shopControllerProof, status *shopMigrationStatus, now time.Time) error {
	if proof == nil || status == nil || proof.Schema != "kkai_shop_migration_proof_v1" || proof.Protocol != "shop-migration-v1" || proof.Operation != "apply" {
		return fmt.Errorf("invalid shop controller proof protocol or operation")
	}
	expected, err := kkaimigrate.ContractForDialect(kkaimigrate.DialectPostgres)
	if err != nil {
		return err
	}
	if expected.RuntimeMinVersion != 14 || expected.RuntimeMaxVersion != 19 || expected.MigrationTargetVersion != 14 ||
		status.Schema != "kkai_shop_migration_status_v1" || (status.CurrentVersion != 18 && status.CurrentVersion != 19) ||
		status.TargetVersion != 19 || !status.Safe ||
		status.SourceDigest != expected.CompatiblePrefixes["18"] || status.TargetDigest != expected.CompatiblePrefixes["19"] ||
		status.PlanChecksum != kkaimigrate.ShopPlanChecksum() || status.ShopSchemaPresent != (status.CurrentVersion == 19) {
		return fmt.Errorf("shop controller requires the exact canonical v18/v19 database and compiled plan")
	}
	if proof.SourceVersion != 18 || proof.TargetVersion != 19 || proof.SourceDigest != status.SourceDigest || proof.TargetDigest != status.TargetDigest || proof.PlanChecksum != status.PlanChecksum || proof.DatabaseName == "" || proof.DatabaseName != status.DatabaseName || proof.DatabaseSchema == "" || proof.DatabaseSchema != status.DatabaseSchema {
		return fmt.Errorf("shop controller proof does not match the exact database/source/compiled plan")
	}
	if len(proof.InstalledInfraSHA) != 40 || !tenantProofHex.MatchString(proof.InstalledInfraSHA) || !tenantProofImage(proof.ImageID) || len(proof.Nonce) != 32 || !tenantProofHex.MatchString(proof.Nonce) {
		return fmt.Errorf("invalid shop controller artifact identity")
	}
	if proof.IssuedAt <= 0 || proof.IssuedAt > now.Unix() || proof.ExpiresAt <= now.Unix() || proof.ExpiresAt <= proof.IssuedAt || proof.ExpiresAt-proof.IssuedAt > 120 {
		return fmt.Errorf("shop controller proof is expired or has an invalid validity window")
	}
	if len(proof.Runtimes) != 3 {
		return fmt.Errorf("shop controller proof requires active, rollback and writer runtime evidence")
	}
	roles, containers, processes := map[string]bool{}, map[string]bool{}, map[int64]bool{}
	for _, instance := range proof.Runtimes {
		if (instance.Role != "active" && instance.Role != "rollback" && instance.Role != "writer") || roles[instance.Role] || instance.Name == "" || len(instance.ContainerID) != 64 || !tenantProofHex.MatchString(instance.ContainerID) || containers[instance.ContainerID] || !tenantProofImage(instance.ImageID) || instance.Version == "" {
			return fmt.Errorf("shop controller runtime identities are invalid or repeated")
		}
		startedAt, err := time.Parse(time.RFC3339Nano, instance.ContainerStartedAt)
		if err != nil || startedAt.Unix() <= 0 || startedAt.Unix() > proof.IssuedAt || instance.ContainerInitStartTicks == 0 || instance.ApplicationPID <= 0 || processes[instance.ApplicationPID] || instance.ApplicationStartTicks < instance.ContainerInitStartTicks || !shopMountNamespace.MatchString(instance.ApplicationMountNamespace) || instance.ApplicationExecutableDevice == 0 || instance.ApplicationExecutableInode == 0 || len(instance.ApplicationCgroupSHA256) != 64 || !tenantProofHex.MatchString(instance.ApplicationCgroupSHA256) {
			return fmt.Errorf("shop controller runtime process identities are invalid or repeated")
		}
		if instance.TenantRoutingEnabled == nil || instance.TenantAware == nil || !*instance.TenantAware || instance.SeparateLogDatabase == nil || *instance.SeparateLogDatabase || instance.SchemaContract.SchemaManagement != "external" || !reflect.DeepEqual(instance.SchemaContract.SchemaContract, expected) {
			return fmt.Errorf("shop mutation requires exact v14-v19-compatible runtimes and main-database logs")
		}
		if instance.Role == "active" && instance.ImageID != proof.ImageID {
			return fmt.Errorf("shop migration image must match the active runtime image")
		}
		roles[instance.Role], containers[instance.ContainerID], processes[instance.ApplicationPID] = true, true, true
	}
	return nil
}

func runShopMigration(ctx context.Context, db *gorm.DB, operation, proofPath string, output io.Writer) error {
	if err := validateShopMigrationCommand(operation, proofPath, true, false); err != nil {
		return err
	}
	if operation == "apply" {
		return fmt.Errorf("%w: shop apply is disabled in compatibility milestone A", kkaimigrate.ErrUnsafeMigration)
	}
	status, err := inspectShopMigration(ctx, db)
	if err != nil {
		return err
	}
	if operation == "apply" {
		if db.Dialector.Name() != kkaimigrate.DialectPostgres || common.SchemaManagementMode != common.SchemaManagementExternal {
			return fmt.Errorf("production shop mutation requires PostgreSQL >=12 and an external-schema image")
		}
		var serverVersion int
		if err := db.WithContext(ctx).Raw("SHOW server_version_num").Scan(&serverVersion).Error; err != nil {
			return err
		}
		if serverVersion < 120000 {
			return fmt.Errorf("production shop mutation requires PostgreSQL >=12")
		}
		raw, err := readRootControllerProofJSON(proofPath, shopControllerProofPath)
		if err != nil {
			return err
		}
		var proof shopControllerProof
		if err = common.Unmarshal(raw, &proof); err != nil {
			return fmt.Errorf("invalid shop controller proof JSON")
		}
		if err = validateShopControllerProof(&proof, status, time.Now()); err != nil {
			return err
		}
		guard := func(ctx context.Context, locked *gorm.DB) error {
			fresh, err := inspectShopMigration(ctx, locked)
			if err != nil {
				return err
			}
			return validateShopControllerProof(&proof, fresh, time.Now())
		}
		if _, err = kkaimigrate.ApplyShopExpand(ctx, db, kkaimigrate.Options{AuthorizeMutation: guard}); err != nil {
			return err
		}
		status, err = inspectShopMigration(ctx, db)
		if err != nil {
			return err
		}
		if status.CurrentVersion != kkaimigrate.ShopSchemaVersion {
			return fmt.Errorf("%w: shop migration did not produce the target receipt", kkaimigrate.ErrSchemaNotReady)
		}
	}
	encoded, err := common.Marshal(status)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(output, string(encoded))
	return err
}
