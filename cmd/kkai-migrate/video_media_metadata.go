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

const videoMediaMetadataControllerProofPath = "/run/video-media-metadata-expansion/proof.json"

type videoMediaMetadataControllerProof struct {
	Schema            string                `json:"schema"`
	Protocol          string                `json:"protocol"`
	Operation         string                `json:"operation"`
	InstalledInfraSHA string                `json:"installed_infra_sha"`
	ImageID           string                `json:"image_id"`
	SourceVersion     int64                 `json:"source_version"`
	TargetVersion     int64                 `json:"target_version"`
	SourceDigest      string                `json:"source_digest"`
	PlanChecksum      string                `json:"plan_checksum"`
	ExpansionDigest   string                `json:"expansion_digest"`
	DatabaseName      string                `json:"database_name"`
	DatabaseSchema    string                `json:"database_schema"`
	IssuedAt          int64                 `json:"issued_at"`
	ExpiresAt         int64                 `json:"expires_at"`
	Nonce             string                `json:"nonce"`
	Runtimes          []shopRuntimeEvidence `json:"runtimes"`
}

func validateVideoMediaMetadataCommand(operation, proofPath string, jsonOutput, conflicting bool) error {
	if !jsonOutput || conflicting {
		return fmt.Errorf("--video-media-metadata-expand requires --json and cannot be combined with other operations")
	}
	switch operation {
	case "plan", "status", "check":
		if proofPath != "" {
			return fmt.Errorf("read-only video media metadata inspection does not accept a controller proof")
		}
	case "apply":
		if proofPath != videoMediaMetadataControllerProofPath {
			return fmt.Errorf("video media metadata mutation requires the fixed installed controller proof mount")
		}
	default:
		return fmt.Errorf("--video-media-metadata-expand expects plan, status, check, or apply")
	}
	return nil
}

func validateVideoMediaMetadataControllerProof(proof *videoMediaMetadataControllerProof, status *kkaimigrate.VideoMediaMetadataExpansionStatus, now time.Time) error {
	if proof == nil || status == nil || proof.Schema != "kkai_video_media_metadata_expansion_proof_v1" || proof.Protocol != "video-media-metadata-v1" || proof.Operation != "apply" {
		return fmt.Errorf("invalid video media metadata controller proof protocol or operation")
	}
	if status.Dialect != kkaimigrate.DialectPostgres || !status.Safe || status.Schema != "kkai_video_media_metadata_expansion_v1" || status.CurrentVersion != kkaimigrate.MembershipBillingRecoverySchemaVersion || status.TargetVersion != kkaimigrate.MembershipBillingRecoverySchemaVersion || status.ExpansionDigest != kkaimigrate.VideoMediaMetadataExpansionDigest() || status.PlanChecksum != kkaimigrate.VideoMediaMetadataPlanChecksum() || status.SourceDigest == "" {
		return fmt.Errorf("video media metadata controller requires exact canonical21 and the compiled expansion plan")
	}
	if status.State != "pristine" && status.State != "receipt_missing" && status.State != "building" && status.State != "column_ready" && status.State != "ready" {
		return fmt.Errorf("video media metadata controller requires a safe resumable expansion state")
	}
	if proof.SourceVersion != status.CurrentVersion || proof.TargetVersion != status.TargetVersion || proof.SourceDigest != status.SourceDigest || proof.PlanChecksum != status.PlanChecksum || proof.ExpansionDigest != status.ExpansionDigest || proof.DatabaseName != status.DatabaseName || proof.DatabaseSchema != status.DatabaseSchema || proof.DatabaseName == "" || proof.DatabaseSchema == "" {
		return fmt.Errorf("video media metadata controller proof does not match the exact database and expansion plan")
	}
	if len(proof.InstalledInfraSHA) != 40 || !tenantProofHex.MatchString(proof.InstalledInfraSHA) || !tenantProofImage(proof.ImageID) || len(proof.Nonce) != 32 || !tenantProofHex.MatchString(proof.Nonce) {
		return fmt.Errorf("invalid video media metadata controller artifact identity")
	}
	if proof.IssuedAt <= 0 || proof.IssuedAt > now.Unix() || proof.ExpiresAt <= now.Unix() || proof.ExpiresAt <= proof.IssuedAt || proof.ExpiresAt-proof.IssuedAt > 120 {
		return fmt.Errorf("video media metadata controller proof is expired or has an invalid validity window")
	}
	if len(proof.Runtimes) != 3 {
		return fmt.Errorf("video media metadata controller proof requires active, rollback and writer runtime evidence")
	}
	roles, containers, processes := map[string]bool{}, map[string]bool{}, map[int64]bool{}
	for _, instance := range proof.Runtimes {
		if (instance.Role != "active" && instance.Role != "rollback" && instance.Role != "writer") || roles[instance.Role] || instance.Name == "" || len(instance.ContainerID) != 64 || !tenantProofHex.MatchString(instance.ContainerID) || containers[instance.ContainerID] || !tenantProofImage(instance.ImageID) || instance.Version == "" {
			return fmt.Errorf("video media metadata controller runtime identities are invalid or repeated")
		}
		startedAt, err := time.Parse(time.RFC3339Nano, instance.ContainerStartedAt)
		if err != nil || startedAt.Unix() <= 0 || startedAt.Unix() > proof.IssuedAt || instance.ContainerInitStartTicks == 0 || instance.ApplicationPID <= 0 || processes[instance.ApplicationPID] || instance.ApplicationStartTicks < instance.ContainerInitStartTicks || !shopMountNamespace.MatchString(instance.ApplicationMountNamespace) || instance.ApplicationExecutableDevice == 0 || instance.ApplicationExecutableInode == 0 || len(instance.ApplicationCgroupSHA256) != 64 || !tenantProofHex.MatchString(instance.ApplicationCgroupSHA256) {
			return fmt.Errorf("video media metadata controller runtime process identities are invalid or repeated")
		}
		if instance.TenantRoutingEnabled == nil || instance.TenantAware == nil || !*instance.TenantAware || instance.SeparateLogDatabase == nil || *instance.SeparateLogDatabase || instance.SchemaContract.SchemaManagement != "external" {
			return fmt.Errorf("video media metadata expansion requires tenant-aware external runtimes and main-database logs")
		}
		if err := kkaimigrate.ValidateVideoMediaMetadataRuntimeContract(instance.SchemaContract.SchemaContract); err != nil {
			return err
		}
		roles[instance.Role], containers[instance.ContainerID], processes[instance.ApplicationPID] = true, true, true
	}
	return nil
}

func runVideoMediaMetadataExpansion(ctx context.Context, db *gorm.DB, operation, proofPath string, output io.Writer) error {
	if err := validateVideoMediaMetadataCommand(operation, proofPath, true, false); err != nil {
		return err
	}
	status, err := kkaimigrate.PlanVideoMediaMetadataExpansion(ctx, db)
	if err != nil {
		return err
	}
	if operation == "check" {
		if err := kkaimigrate.CheckVideoMediaMetadataExpansion(ctx, db); err != nil {
			return err
		}
	}
	if operation == "apply" {
		if common.SchemaManagementMode != common.SchemaManagementExternal {
			return fmt.Errorf("video media metadata production mutation requires an external-schema image")
		}
		raw, err := readRootControllerProofJSON(proofPath, videoMediaMetadataControllerProofPath)
		if err != nil {
			return err
		}
		var proof videoMediaMetadataControllerProof
		if err := common.Unmarshal(raw, &proof); err != nil {
			return fmt.Errorf("invalid video media metadata controller proof JSON")
		}
		if err := validateVideoMediaMetadataControllerProof(&proof, status, time.Now()); err != nil {
			return err
		}
		guard := func(ctx context.Context, locked *gorm.DB) error {
			return authorizeVideoMediaMetadataMutation(ctx, locked, &proof, status)
		}
		status, err = kkaimigrate.ApplyVideoMediaMetadataExpand(ctx, db, kkaimigrate.Options{AuthorizeMutation: guard})
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

func authorizeVideoMediaMetadataMutation(ctx context.Context, locked *gorm.DB, proof *videoMediaMetadataControllerProof, status *kkaimigrate.VideoMediaMetadataExpansionStatus) error {
	// Apply revalidates the complete schema and receipt after acquiring the
	// migration lock. Keep each subsequent mutation bound to that same database
	// and a live proof without repeating the full schema scan under the lock.
	name, schema, err := kkaimigrate.TenantOnlineDatabaseIdentity(ctx, locked)
	if err != nil {
		return err
	}
	if status == nil || name != status.DatabaseName || schema != status.DatabaseSchema {
		return fmt.Errorf("video media metadata database identity changed under the schema lock")
	}
	return validateVideoMediaMetadataControllerProof(proof, status, time.Now())
}
