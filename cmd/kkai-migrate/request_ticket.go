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

type requestTicketStatus struct {
	Schema         string `json:"schema"`
	CurrentVersion int64  `json:"current_version"`
	TargetVersion  int64  `json:"target_version"`
	TicketsPresent bool   `json:"tickets_present"`
	EventsPresent  bool   `json:"events_present"`
	Safe           bool   `json:"safe"`
	SourceDigest   string `json:"source_digest"`
	TargetDigest   string `json:"target_digest"`
	PlanChecksum   string `json:"plan_checksum"`
	DatabaseName   string `json:"database_name"`
	DatabaseSchema string `json:"database_schema"`
}

type requestTicketRuntimeEvidence struct {
	tenantRuntimeEvidence
	SchemaContract struct {
		kkaimigrate.SchemaContract
		SchemaManagement string `json:"schema_management"`
	} `json:"schema_contract"`
}

type requestTicketControllerProof struct {
	tenantControllerProof
	TargetDigest string                         `json:"target_digest"`
	Runtimes     []requestTicketRuntimeEvidence `json:"runtimes"`
}

func validateRequestTicketCommand(operation, proofPath string, jsonOutput, conflicting bool) error {
	if !jsonOutput || conflicting {
		return fmt.Errorf("--request-ticket requires --json and cannot be combined with other operations")
	}
	switch operation {
	case "plan", "status":
		if proofPath != "" {
			return fmt.Errorf("read-only request ticket inspection does not accept a controller proof")
		}
	case "apply":
		if proofPath != tenantControllerProofPath {
			return fmt.Errorf("request ticket mutation requires the fixed installed root-controller proof mount")
		}
	default:
		return fmt.Errorf("--request-ticket expects plan, status, or apply")
	}
	return nil
}

func inspectRequestTicket(ctx context.Context, db *gorm.DB) (*requestTicketStatus, error) {
	if db == nil {
		return nil, kkaimigrate.ErrSchemaNotReady
	}
	precheck, err := kkaimigrate.PrecheckRequestTicketExpand(ctx, db)
	if err != nil {
		return nil, err
	}
	if precheck.CurrentVersion == 15 && (precheck.TicketsPresent || precheck.EventsPresent) {
		return nil, fmt.Errorf("%w: partial request ticket schema has no canonical receipt", kkaimigrate.ErrSchemaNotReady)
	}
	if precheck.CurrentVersion == 16 {
		if err := kkaimigrate.CheckRequestTicketSchema(ctx, db); err != nil {
			return nil, err
		}
	}
	contract, err := kkaimigrate.ContractForDialect(db.Dialector.Name())
	if err != nil {
		return nil, err
	}
	if contract.RuntimeMinVersion != 14 || (contract.RuntimeMaxVersion != 16 && contract.RuntimeMaxVersion != 17) || contract.MigrationTargetVersion != 14 || contract.MigrationKind != kkaimigrate.MigrationKindNone || contract.MigrationSetDigest != contract.CompatiblePrefixes["14"] || contract.CompatiblePrefixes["15"] == "" || contract.CompatiblePrefixes["16"] == "" {
		return nil, fmt.Errorf("%w: request ticket inspection requires the canonical v14/v15/v16 prefixes", kkaimigrate.ErrUnsafeMigration)
	}
	name, schema, err := kkaimigrate.TenantOnlineDatabaseIdentity(ctx, db)
	if err != nil {
		return nil, err
	}
	return &requestTicketStatus{
		Schema: "kkai_request_ticket_v1", CurrentVersion: precheck.CurrentVersion, TargetVersion: precheck.TargetVersion,
		TicketsPresent: precheck.TicketsPresent, EventsPresent: precheck.EventsPresent, Safe: precheck.Safe,
		SourceDigest: contract.CompatiblePrefixes["15"], TargetDigest: contract.CompatiblePrefixes["16"],
		PlanChecksum: kkaimigrate.RequestTicketPlanChecksum(), DatabaseName: name, DatabaseSchema: schema,
	}, nil
}

func validateRequestTicketProof(proof *requestTicketControllerProof, status *requestTicketStatus, now time.Time) error {
	if proof == nil || status == nil || proof.Schema != "kkai_request_ticket_proof_v1" || proof.Protocol != "request-ticket-v1" || proof.Operation != "apply" {
		return fmt.Errorf("invalid request ticket controller proof protocol or operation")
	}
	expected, err := kkaimigrate.ContractForDialect(kkaimigrate.DialectPostgres)
	if err != nil {
		return err
	}
	// This installed mutation protocol remains bound to its original contract.
	// New additive invoice compatibility permits read-only inspection above,
	// never broader authorization for this historical controller.
	expected.RuntimeMaxVersion = 16
	delete(expected.CompatiblePrefixes, "17")
	if expected.RuntimeMinVersion != 14 || expected.RuntimeMaxVersion != 16 || expected.MigrationTargetVersion != 14 || status.Schema != "kkai_request_ticket_v1" || (status.CurrentVersion != 15 && status.CurrentVersion != 16) || status.TargetVersion != 16 || !status.Safe || status.SourceDigest != expected.CompatiblePrefixes["15"] || status.TargetDigest != expected.CompatiblePrefixes["16"] || status.PlanChecksum != kkaimigrate.RequestTicketPlanChecksum() {
		return fmt.Errorf("request ticket controller requires the exact compiled v15/v16 contract")
	}
	if (status.CurrentVersion == 15 && (status.TicketsPresent || status.EventsPresent)) || (status.CurrentVersion == 16 && (!status.TicketsPresent || !status.EventsPresent)) {
		return fmt.Errorf("request ticket controller status has an unreceipted or incomplete schema")
	}
	if proof.SourceVersion != 15 || proof.TargetVersion != 16 || proof.SourceDigest != status.SourceDigest || proof.TargetDigest != status.TargetDigest || proof.PlanChecksum != status.PlanChecksum || proof.DatabaseName == "" || proof.DatabaseName != status.DatabaseName || proof.DatabaseSchema == "" || proof.DatabaseSchema != status.DatabaseSchema {
		return fmt.Errorf("request ticket controller proof does not match the exact database/source/compiled plan")
	}
	if len(proof.InstalledInfraSHA) != 40 || !tenantProofHex.MatchString(proof.InstalledInfraSHA) || !tenantProofImage(proof.ImageID) || len(proof.Nonce) != 32 || !tenantProofHex.MatchString(proof.Nonce) {
		return fmt.Errorf("invalid request ticket controller artifact identity")
	}
	if proof.IssuedAt <= 0 || proof.IssuedAt > now.Unix() || proof.ExpiresAt <= now.Unix() || proof.ExpiresAt <= proof.IssuedAt || proof.ExpiresAt-proof.IssuedAt > 120 {
		return fmt.Errorf("request ticket controller proof is expired or has an invalid validity window")
	}
	if len(proof.Runtimes) != 3 {
		return fmt.Errorf("request ticket controller proof requires active, rollback and writer runtime evidence")
	}
	roles, containers := map[string]bool{}, map[string]bool{}
	for _, instance := range proof.Runtimes {
		if (instance.Role != "active" && instance.Role != "rollback" && instance.Role != "writer") || roles[instance.Role] || instance.Name == "" || len(instance.ContainerID) != 64 || !tenantProofHex.MatchString(instance.ContainerID) || containers[instance.ContainerID] || !tenantProofImage(instance.ImageID) || instance.Version == "" {
			return fmt.Errorf("request ticket controller runtime identities are invalid or repeated")
		}
		if instance.TenantRoutingEnabled == nil || instance.TenantAware == nil || !*instance.TenantAware || instance.SeparateLogDatabase == nil || *instance.SeparateLogDatabase || instance.SchemaContract.SchemaManagement != "external" || !reflect.DeepEqual(instance.SchemaContract.SchemaContract, expected) {
			return fmt.Errorf("request ticket mutation requires exact v14/v15/v16-compatible runtimes and main-database logs")
		}
		if instance.Role == "active" && instance.ImageID != proof.ImageID {
			return fmt.Errorf("request ticket migration image must match the active runtime image")
		}
		roles[instance.Role], containers[instance.ContainerID] = true, true
	}
	return nil
}

func runRequestTicket(ctx context.Context, db *gorm.DB, operation, proofPath string, output io.Writer) error {
	if err := validateRequestTicketCommand(operation, proofPath, true, false); err != nil {
		return err
	}
	status, err := inspectRequestTicket(ctx, db)
	if err != nil {
		return err
	}
	if operation == "apply" {
		if db.Dialector.Name() != kkaimigrate.DialectPostgres {
			return fmt.Errorf("production request ticket mutation requires PostgreSQL >=12")
		}
		raw, err := readTenantControllerProofJSON(proofPath)
		if err != nil {
			return err
		}
		var proof requestTicketControllerProof
		if err = common.Unmarshal(raw, &proof); err != nil {
			return fmt.Errorf("invalid request ticket controller proof JSON")
		}
		if err = validateRequestTicketProof(&proof, status, time.Now()); err != nil {
			return err
		}
		guard := func(ctx context.Context, locked *gorm.DB) error {
			fresh, err := inspectRequestTicket(ctx, locked)
			if err != nil {
				return err
			}
			return validateRequestTicketProof(&proof, fresh, time.Now())
		}
		if _, err = kkaimigrate.ApplyRequestTicketExpand(ctx, db, kkaimigrate.Options{AuthorizeMutation: guard}); err != nil {
			return err
		}
		status, err = inspectRequestTicket(ctx, db)
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
