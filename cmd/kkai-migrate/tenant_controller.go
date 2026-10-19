package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/kkaimigrate"
	"gorm.io/gorm"
)

const tenantControllerProofPath = "/run/tenant-online/proof.json"

type tenantRuntimeEvidence struct {
	Name                 string `json:"name"`
	ContainerID          string `json:"container_id"`
	ImageID              string `json:"image_id"`
	Version              string `json:"version"`
	Role                 string `json:"role"`
	TenantRoutingEnabled *bool  `json:"tenant_routing_enabled"`
	TenantAware          *bool  `json:"tenant_aware"`
	SeparateLogDatabase  *bool  `json:"separate_log_database"`
}

// This is controller evidence, not a self-authenticating JSON signature. The
// root-owned read-only bind mount is the trust boundary; the controller must
// inspect live containers, routing and immutable image identities itself.
type tenantControllerProof struct {
	Schema            string                  `json:"schema"`
	Protocol          string                  `json:"protocol"`
	Operation         string                  `json:"operation"`
	InstalledInfraSHA string                  `json:"installed_infra_sha"`
	ImageID           string                  `json:"image_id"`
	PlanChecksum      string                  `json:"plan_checksum"`
	SourceDigest      string                  `json:"source_digest"`
	SourceVersion     int64                   `json:"source_version"`
	TargetVersion     int64                   `json:"target_version"`
	DatabaseName      string                  `json:"database_name"`
	DatabaseSchema    string                  `json:"database_schema"`
	IssuedAt          int64                   `json:"issued_at"`
	ExpiresAt         int64                   `json:"expires_at"`
	Nonce             string                  `json:"nonce"`
	Runtimes          []tenantRuntimeEvidence `json:"runtimes"`
}

var tenantProofHex = regexp.MustCompile(`^[0-9a-f]+$`)

func readTenantControllerProof(path string) (*tenantControllerProof, error) {
	if path != tenantControllerProofPath || runtime.GOOS != "linux" {
		return nil, fmt.Errorf("controller proof requires the fixed Linux read-only mount %s", tenantControllerProofPath)
	}
	if os.Geteuid() != 0 {
		return nil, fmt.Errorf("controller proof requires UID 0 in the isolated migration container")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("controller proof is unavailable")
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != 0400 || info.Size() > 65536 {
		return nil, fmt.Errorf("controller proof must be a root-owned 0400 regular file <=64KiB")
	}
	mounts, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return nil, fmt.Errorf("cannot verify controller proof mount")
	}
	readOnly := false
	for _, line := range strings.Split(string(mounts), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 5 && fields[4] == path {
			for _, option := range strings.Split(fields[5], ",") {
				if option == "ro" {
					readOnly = true
				}
			}
		}
	}
	if !readOnly {
		return nil, fmt.Errorf("controller proof is not an independent read-only bind mount")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read controller proof")
	}
	var proof tenantControllerProof
	if err := common.Unmarshal(raw, &proof); err != nil {
		return nil, fmt.Errorf("invalid controller proof JSON")
	}
	return &proof, nil
}

func validateTenantControllerProof(proof *tenantControllerProof, operation string, status *kkaimigrate.TenantOnlineExpansionStatus, now time.Time) error {
	if proof == nil || status == nil || proof.Schema != "kkai_tenant_online_proof_v1" || proof.Protocol != "tenant-online-v1" || proof.Operation != operation || (operation != "apply" && operation != "finalize") {
		return fmt.Errorf("invalid controller proof protocol or operation")
	}
	if len(proof.InstalledInfraSHA) != 40 || !tenantProofHex.MatchString(proof.InstalledInfraSHA) || !tenantProofImage(proof.ImageID) || len(proof.Nonce) != 32 || !tenantProofHex.MatchString(proof.Nonce) {
		return fmt.Errorf("invalid controller proof artifact identity")
	}
	if proof.SourceVersion != 11 || proof.TargetVersion != 14 || proof.PlanChecksum != status.PlanChecksum || proof.SourceDigest != status.SourceDigest || proof.DatabaseName != status.DatabaseName || proof.DatabaseSchema != status.DatabaseSchema || proof.DatabaseName == "" {
		return fmt.Errorf("controller proof does not match the exact database/source/compiled plan")
	}
	if proof.IssuedAt <= 0 || proof.IssuedAt > now.Unix() || proof.ExpiresAt <= now.Unix() || proof.ExpiresAt <= proof.IssuedAt || proof.ExpiresAt-proof.IssuedAt > 120 {
		return fmt.Errorf("controller proof is expired or has an invalid validity window")
	}
	if len(proof.Runtimes) != 3 {
		return fmt.Errorf("controller proof requires active, rollback and writer runtime evidence")
	}
	roles := map[string]bool{}
	containers := map[string]bool{}
	for _, instance := range proof.Runtimes {
		if (instance.Role != "active" && instance.Role != "rollback" && instance.Role != "writer") || roles[instance.Role] || instance.Name == "" || len(instance.ContainerID) != 64 || !tenantProofHex.MatchString(instance.ContainerID) || containers[instance.ContainerID] || !tenantProofImage(instance.ImageID) || instance.Version == "" {
			return fmt.Errorf("controller proof runtime identities are invalid or repeated")
		}
		if instance.TenantRoutingEnabled == nil || *instance.TenantRoutingEnabled || instance.SeparateLogDatabase == nil || *instance.SeparateLogDatabase || instance.TenantAware == nil || (operation == "finalize" && !*instance.TenantAware) {
			return fmt.Errorf("controller proof runtime must have tenant routes off and main-database logs; finalize also requires tenant-aware binaries")
		}
		roles[instance.Role], containers[instance.ContainerID] = true, true
	}
	return nil
}

func tenantProofImage(value string) bool {
	return len(value) == 71 && strings.HasPrefix(value, "sha256:") && tenantProofHex.MatchString(strings.TrimPrefix(value, "sha256:"))
}

func runTenantControllerMutation(ctx context.Context, db *gorm.DB, operation, proofPath string, output io.Writer) error {
	if db == nil || db.Dialector.Name() != kkaimigrate.DialectPostgres {
		return fmt.Errorf("controller online mutation requires PostgreSQL >=12; offline fallback has no production CLI")
	}
	proof, err := readTenantControllerProof(proofPath)
	if err != nil {
		return err
	}
	status, err := kkaimigrate.PlanTenantOnlineExpansion(ctx, db)
	if err != nil {
		return err
	}
	if err := validateTenantControllerProof(proof, operation, status, time.Now()); err != nil {
		return err
	}
	guard := func(ctx context.Context, locked *gorm.DB) error {
		name, schema, err := kkaimigrate.TenantOnlineDatabaseIdentity(ctx, locked)
		if err != nil {
			return err
		}
		if name != status.DatabaseName || schema != status.DatabaseSchema {
			return fmt.Errorf("controller database identity changed")
		}
		return validateTenantControllerProof(proof, operation, status, time.Now())
	}
	options := kkaimigrate.Options{AuthorizeMutation: guard}
	if operation == "apply" {
		_, err = kkaimigrate.ApplyTenantOnlineExpansion(ctx, db, options)
	} else {
		_, err = kkaimigrate.FinalizeTenantOnlineSchema(ctx, db, options)
	}
	if err != nil {
		return err
	}
	return runTenantOnlineInspection(ctx, db, "status", output)
}
