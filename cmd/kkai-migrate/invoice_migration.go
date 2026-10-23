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

const invoiceControllerProofPath = "/run/invoice-migration/proof.json"

type invoiceMigrationStatus struct {
	Schema          string `json:"schema"`
	CurrentVersion  int64  `json:"current_version"`
	TargetVersion   int64  `json:"target_version"`
	InvoicesPresent bool   `json:"invoices_present"`
	Safe            bool   `json:"safe"`
	SourceDigest    string `json:"source_digest"`
	TargetDigest    string `json:"target_digest"`
	PlanChecksum    string `json:"plan_checksum"`
	DatabaseName    string `json:"database_name"`
	DatabaseSchema  string `json:"database_schema"`
}

type invoiceRuntimeEvidence struct {
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

// Invoice mutation has an independent protocol and fixed proof mount. The
// root controller attests live runtime/process identities; application JSON is
// never a substitute for that root-owned read-only trust boundary.
type invoiceControllerProof struct {
	tenantControllerProof
	TargetDigest string                   `json:"target_digest"`
	Runtimes     []invoiceRuntimeEvidence `json:"runtimes"`
}

var invoiceMountNamespace = regexp.MustCompile(`^mnt:\[[1-9][0-9]*\]$`)

func validateInvoiceMigrationCommand(operation, proofPath string, jsonOutput, conflicting bool) error {
	if !jsonOutput || conflicting {
		return fmt.Errorf("--invoice-migration requires --json and cannot be combined with other operations")
	}
	switch operation {
	case "plan", "status":
		if proofPath != "" {
			return fmt.Errorf("read-only invoice inspection does not accept a controller proof")
		}
	case "apply":
		if proofPath != invoiceControllerProofPath {
			return fmt.Errorf("invoice mutation requires the fixed installed invoice-controller proof mount")
		}
	default:
		return fmt.Errorf("--invoice-migration expects plan, status, or apply")
	}
	return nil
}

func inspectInvoiceMigration(ctx context.Context, db *gorm.DB) (*invoiceMigrationStatus, error) {
	precheck, err := kkaimigrate.PrecheckInvoiceExpand(ctx, db)
	if err != nil {
		return nil, err
	}
	if precheck.CurrentVersion == kkaimigrate.RequestTicketSchemaVersion && precheck.InvoicesPresent {
		return nil, fmt.Errorf("%w: unreceipted invoice tables require operator repair", kkaimigrate.ErrSchemaNotReady)
	}
	if precheck.CurrentVersion == kkaimigrate.InvoiceSchemaVersion {
		if err := kkaimigrate.CheckInvoiceSchema(ctx, db); err != nil {
			return nil, err
		}
	}
	contract, err := kkaimigrate.ContractForDialect(db.Dialector.Name())
	if err != nil {
		return nil, err
	}
	if contract.RuntimeMinVersion != 14 || contract.RuntimeMaxVersion != 17 || contract.MigrationTargetVersion != 14 || contract.MigrationKind != kkaimigrate.MigrationKindNone || contract.MigrationSetDigest != contract.CompatiblePrefixes["14"] || contract.CompatiblePrefixes["16"] == "" || contract.CompatiblePrefixes["17"] == "" {
		return nil, fmt.Errorf("%w: invoice controller requires the exact v14-v17 compatibility runtime", kkaimigrate.ErrUnsafeMigration)
	}
	name, schema, err := kkaimigrate.TenantOnlineDatabaseIdentity(ctx, db)
	if err != nil {
		return nil, err
	}
	return &invoiceMigrationStatus{
		Schema: "kkai_invoice_migration_status_v1", CurrentVersion: precheck.CurrentVersion, TargetVersion: precheck.TargetVersion,
		InvoicesPresent: precheck.InvoicesPresent, Safe: precheck.Safe, SourceDigest: contract.CompatiblePrefixes["16"], TargetDigest: contract.CompatiblePrefixes["17"],
		PlanChecksum: kkaimigrate.InvoicePlanChecksum(), DatabaseName: name, DatabaseSchema: schema,
	}, nil
}

func validateInvoiceControllerProof(proof *invoiceControllerProof, status *invoiceMigrationStatus, now time.Time) error {
	if proof == nil || status == nil || proof.Schema != "kkai_invoice_migration_proof_v1" || proof.Protocol != "invoice-migration-v1" || proof.Operation != "apply" {
		return fmt.Errorf("invalid invoice controller proof protocol or operation")
	}
	expected, err := kkaimigrate.ContractForDialect(kkaimigrate.DialectPostgres)
	if err != nil {
		return err
	}
	if expected.RuntimeMinVersion != 14 || expected.RuntimeMaxVersion != 17 || expected.MigrationTargetVersion != 14 || status.Schema != "kkai_invoice_migration_status_v1" || (status.CurrentVersion != 16 && status.CurrentVersion != 17) || status.TargetVersion != 17 || !status.Safe || status.SourceDigest != expected.CompatiblePrefixes["16"] || status.TargetDigest != expected.CompatiblePrefixes["17"] || status.PlanChecksum != kkaimigrate.InvoicePlanChecksum() || status.InvoicesPresent != (status.CurrentVersion == 17) {
		return fmt.Errorf("invoice controller requires the exact canonical v16/v17 database and compiled plan")
	}
	if proof.SourceVersion != 16 || proof.TargetVersion != 17 || proof.SourceDigest != status.SourceDigest || proof.TargetDigest != status.TargetDigest || proof.PlanChecksum != status.PlanChecksum || proof.DatabaseName == "" || proof.DatabaseName != status.DatabaseName || proof.DatabaseSchema == "" || proof.DatabaseSchema != status.DatabaseSchema {
		return fmt.Errorf("invoice controller proof does not match the exact database/source/compiled plan")
	}
	if len(proof.InstalledInfraSHA) != 40 || !tenantProofHex.MatchString(proof.InstalledInfraSHA) || !tenantProofImage(proof.ImageID) || len(proof.Nonce) != 32 || !tenantProofHex.MatchString(proof.Nonce) {
		return fmt.Errorf("invalid invoice controller artifact identity")
	}
	if proof.IssuedAt <= 0 || proof.IssuedAt > now.Unix() || proof.ExpiresAt <= now.Unix() || proof.ExpiresAt <= proof.IssuedAt || proof.ExpiresAt-proof.IssuedAt > 120 {
		return fmt.Errorf("invoice controller proof is expired or has an invalid validity window")
	}
	if len(proof.Runtimes) != 3 {
		return fmt.Errorf("invoice controller proof requires active, rollback and writer runtime evidence")
	}
	roles, containers, processes := map[string]bool{}, map[string]bool{}, map[int64]bool{}
	for _, instance := range proof.Runtimes {
		if (instance.Role != "active" && instance.Role != "rollback" && instance.Role != "writer") || roles[instance.Role] || instance.Name == "" || len(instance.ContainerID) != 64 || !tenantProofHex.MatchString(instance.ContainerID) || containers[instance.ContainerID] || !tenantProofImage(instance.ImageID) || instance.Version == "" {
			return fmt.Errorf("invoice controller runtime identities are invalid or repeated")
		}
		startedAt, err := time.Parse(time.RFC3339Nano, instance.ContainerStartedAt)
		if err != nil || startedAt.Unix() <= 0 || startedAt.Unix() > proof.IssuedAt || instance.ContainerInitStartTicks == 0 || instance.ApplicationPID <= 0 || processes[instance.ApplicationPID] || instance.ApplicationStartTicks < instance.ContainerInitStartTicks || !invoiceMountNamespace.MatchString(instance.ApplicationMountNamespace) || instance.ApplicationExecutableDevice == 0 || instance.ApplicationExecutableInode == 0 || len(instance.ApplicationCgroupSHA256) != 64 || !tenantProofHex.MatchString(instance.ApplicationCgroupSHA256) {
			return fmt.Errorf("invoice controller runtime process identities are invalid or repeated")
		}
		if instance.TenantRoutingEnabled == nil || instance.TenantAware == nil || !*instance.TenantAware || instance.SeparateLogDatabase == nil || *instance.SeparateLogDatabase || instance.SchemaContract.SchemaManagement != "external" || !reflect.DeepEqual(instance.SchemaContract.SchemaContract, expected) {
			return fmt.Errorf("invoice mutation requires exact v14-v17-compatible runtimes and main-database logs")
		}
		if instance.Role == "active" && instance.ImageID != proof.ImageID {
			return fmt.Errorf("invoice migration image must match the active runtime image")
		}
		roles[instance.Role], containers[instance.ContainerID], processes[instance.ApplicationPID] = true, true, true
	}
	return nil
}

func runInvoiceMigration(ctx context.Context, db *gorm.DB, operation, proofPath string, output io.Writer) error {
	if err := validateInvoiceMigrationCommand(operation, proofPath, true, false); err != nil {
		return err
	}
	status, err := inspectInvoiceMigration(ctx, db)
	if err != nil {
		return err
	}
	if operation == "apply" {
		if db.Dialector.Name() != kkaimigrate.DialectPostgres || common.SchemaManagementMode != common.SchemaManagementExternal {
			return fmt.Errorf("production invoice mutation requires PostgreSQL >=12 and an external-schema image")
		}
		raw, err := readRootControllerProofJSON(proofPath, invoiceControllerProofPath)
		if err != nil {
			return err
		}
		var proof invoiceControllerProof
		if err = common.Unmarshal(raw, &proof); err != nil {
			return fmt.Errorf("invalid invoice controller proof JSON")
		}
		if err = validateInvoiceControllerProof(&proof, status, time.Now()); err != nil {
			return err
		}
		guard := func(ctx context.Context, locked *gorm.DB) error {
			fresh, err := inspectInvoiceMigration(ctx, locked)
			if err != nil {
				return err
			}
			return validateInvoiceControllerProof(&proof, fresh, time.Now())
		}
		if _, err = kkaimigrate.ApplyInvoiceExpand(ctx, db, kkaimigrate.Options{AuthorizeMutation: guard}); err != nil {
			return err
		}
		status, err = inspectInvoiceMigration(ctx, db)
		if err != nil {
			return err
		}
		if status.CurrentVersion != kkaimigrate.InvoiceSchemaVersion {
			return fmt.Errorf("%w: invoice migration did not produce the target receipt", kkaimigrate.ErrSchemaNotReady)
		}
	}
	encoded, err := common.Marshal(status)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(output, string(encoded))
	return err
}
