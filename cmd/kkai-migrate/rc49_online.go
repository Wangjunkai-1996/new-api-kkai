package main

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/kkaimigrate"
	"gorm.io/gorm"
)

const rc49ControllerProofPath = "/run/rc49-expansion/proof.json"

type rc49ControllerProof struct {
	tenantControllerProof
	TargetDigest    string                `json:"target_digest"`
	ExpansionDigest string                `json:"expansion_digest"`
	Runtimes        []shopRuntimeEvidence `json:"runtimes"`
}

func validateRC49OnlineCommand(operation, proofPath string, jsonOutput, conflicting bool) error {
	if !jsonOutput || conflicting {
		return fmt.Errorf("--rc49-expand requires --json and cannot be combined with other operations")
	}
	switch operation {
	case "plan", "status":
		if proofPath != "" {
			return fmt.Errorf("read-only rc49 inspection does not accept a controller proof")
		}
	case "apply", "repair-index":
		if proofPath != rc49ControllerProofPath {
			return fmt.Errorf("rc49 mutation requires the fixed installed controller proof mount")
		}
	default:
		return fmt.Errorf("--rc49-expand expects plan, status, apply, or repair-index")
	}
	return nil
}

func validateRC49ControllerProof(proof *rc49ControllerProof, operation string, status *kkaimigrate.RC49OnlineStatus, now time.Time) error {
	if proof == nil || status == nil || proof.Schema != "kkai_rc49_expansion_proof_v1" || proof.Protocol != "rc49-expansion-v1" || proof.Operation != operation || (operation != "apply" && operation != "repair-index") {
		return fmt.Errorf("invalid rc49 controller proof protocol or operation")
	}
	if status.Schema != "kkai_rc49_expansion_status_v1" || status.CurrentVersion != 21 || status.TargetVersion != 21 ||
		status.SourceDigest != kkaimigrate.RC49OnlineSourceDigest() || status.TargetDigest != status.SourceDigest ||
		status.PlanChecksum != kkaimigrate.RC49OnlinePlanChecksum() || status.ExpansionDigest != kkaimigrate.RC49OnlineExpansionDigest() {
		return fmt.Errorf("rc49 controller requires exact canonical21 and the compiled expansion plan")
	}
	if operation == "apply" && (!status.Safe || (status.State != "pristine" && status.State != "columns_ready" && status.State != "index_pending" && status.State != "ready")) ||
		operation == "repair-index" && (status.Safe || status.State != "index_invalid") {
		return fmt.Errorf("rc49 controller operation does not match the observed expansion state")
	}
	if proof.SourceVersion != 21 || proof.TargetVersion != 21 || proof.SourceDigest != status.SourceDigest || proof.TargetDigest != status.TargetDigest ||
		proof.PlanChecksum != status.PlanChecksum || proof.ExpansionDigest != status.ExpansionDigest ||
		proof.DatabaseName == "" || proof.DatabaseName != status.DatabaseName || proof.DatabaseSchema == "" || proof.DatabaseSchema != status.DatabaseSchema {
		return fmt.Errorf("rc49 controller proof does not match the exact database and expansion plan")
	}
	if len(proof.InstalledInfraSHA) != 40 || !tenantProofHex.MatchString(proof.InstalledInfraSHA) || !tenantProofImage(proof.ImageID) || len(proof.Nonce) != 32 || !tenantProofHex.MatchString(proof.Nonce) {
		return fmt.Errorf("invalid rc49 controller artifact identity")
	}
	if proof.IssuedAt <= 0 || proof.IssuedAt > now.Unix() || proof.ExpiresAt <= now.Unix() || proof.ExpiresAt <= proof.IssuedAt || proof.ExpiresAt-proof.IssuedAt > 120 {
		return fmt.Errorf("rc49 controller proof is expired or has an invalid validity window")
	}
	if len(proof.Runtimes) != 3 {
		return fmt.Errorf("rc49 controller proof requires active, rollback and writer runtime evidence")
	}
	roles, containers, processes := map[string]bool{}, map[string]bool{}, map[int64]bool{}
	for _, instance := range proof.Runtimes {
		if (instance.Role != "active" && instance.Role != "rollback" && instance.Role != "writer") || roles[instance.Role] || instance.Name == "" || len(instance.ContainerID) != 64 || !tenantProofHex.MatchString(instance.ContainerID) || containers[instance.ContainerID] || !tenantProofImage(instance.ImageID) || instance.Version == "" {
			return fmt.Errorf("rc49 controller runtime identities are invalid or repeated")
		}
		startedAt, err := time.Parse(time.RFC3339Nano, instance.ContainerStartedAt)
		if err != nil || startedAt.Unix() <= 0 || startedAt.Unix() > proof.IssuedAt || instance.ContainerInitStartTicks == 0 || instance.ApplicationPID <= 0 || processes[instance.ApplicationPID] || instance.ApplicationStartTicks < instance.ContainerInitStartTicks || !shopMountNamespace.MatchString(instance.ApplicationMountNamespace) || instance.ApplicationExecutableDevice == 0 || instance.ApplicationExecutableInode == 0 || len(instance.ApplicationCgroupSHA256) != 64 || !tenantProofHex.MatchString(instance.ApplicationCgroupSHA256) {
			return fmt.Errorf("rc49 controller runtime process identities are invalid or repeated")
		}
		if instance.TenantRoutingEnabled == nil || instance.TenantAware == nil || !*instance.TenantAware || instance.SeparateLogDatabase == nil || *instance.SeparateLogDatabase || instance.SchemaContract.SchemaManagement != "external" {
			return fmt.Errorf("rc49 online expansion requires tenant-aware external runtimes and main-database logs")
		}
		if err := kkaimigrate.ValidateRC49OnlineRuntimeContract(instance.SchemaContract.SchemaContract); err != nil {
			return err
		}
		roles[instance.Role], containers[instance.ContainerID], processes[instance.ApplicationPID] = true, true, true
	}
	return nil
}

func runRC49Online(ctx context.Context, db *gorm.DB, operation, proofPath string, output io.Writer) error {
	if err := validateRC49OnlineCommand(operation, proofPath, true, false); err != nil {
		return err
	}
	status, err := kkaimigrate.PlanRC49OnlineExpansion(ctx, db)
	if err != nil {
		return err
	}
	if operation == "apply" || operation == "repair-index" {
		if common.SchemaManagementMode != common.SchemaManagementExternal {
			return fmt.Errorf("rc49 production mutation requires an external-schema image")
		}
		raw, err := readRootControllerProofJSON(proofPath, rc49ControllerProofPath)
		if err != nil {
			return err
		}
		var proof rc49ControllerProof
		if err := common.Unmarshal(raw, &proof); err != nil {
			return fmt.Errorf("invalid rc49 controller proof JSON")
		}
		if err := validateRC49ControllerProof(&proof, operation, status, time.Now()); err != nil {
			return err
		}
		guard := func(ctx context.Context, locked *gorm.DB) error {
			// The library validates complete physical state under the schema
			// advisory lock. Keep revalidation under hot-table NOWAIT locks to
			// canonical ledger identity, database identity and proof expiry.
			version, err := kkaimigrate.WalletPaymentLedgerVersion(ctx, locked)
			if err != nil {
				return err
			}
			name, schema, err := kkaimigrate.TenantOnlineDatabaseIdentity(ctx, locked)
			if err != nil {
				return err
			}
			if version != 21 || name != status.DatabaseName || schema != status.DatabaseSchema {
				return fmt.Errorf("rc49 database identity changed under the schema lock")
			}
			return validateRC49ControllerProof(&proof, operation, status, time.Now())
		}
		options := kkaimigrate.Options{AuthorizeMutation: guard}
		if operation == "repair-index" {
			status, err = kkaimigrate.RepairRC49OnlineIndex(ctx, db, options)
		} else {
			status, err = kkaimigrate.ApplyRC49OnlineExpansion(ctx, db, options)
		}
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
