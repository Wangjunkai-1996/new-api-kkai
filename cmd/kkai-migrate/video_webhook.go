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
	"gorm.io/gorm/logger"
)

const videoWebhookProofPath = "/run/video-webhook-expansion/proof.json"

func runVideoWebhookExpansion(ctx context.Context, dsn, operation, proofPath string, output io.Writer) error {
	switch operation {
	case "plan", "check":
		if proofPath != "" {
			return fmt.Errorf("read-only video webhook inspection does not accept a controller proof")
		}
	case "apply":
		if proofPath != videoWebhookProofPath || common.SchemaManagementMode != common.SchemaManagementExternal {
			return fmt.Errorf("video webhook apply requires the external-schema image and fixed controller proof mount")
		}
	default:
		return fmt.Errorf("--video-webhook-expand expects plan, check, or apply")
	}
	db, err := openDatabase(dsn)
	if err != nil {
		return fmt.Errorf("failed to open migration database")
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	db = db.WithContext(ctx).Session(&gorm.Session{Logger: db.Logger.LogMode(logger.Silent)})
	dialect := db.Dialector.Name()
	state, err := kkaimigrate.PlanVideoWebhookExpansion(db)
	if err != nil {
		return err
	}
	status := map[string]any{"schema": kkaimigrate.VideoWebhookExpansion, "dialect": dialect, "state": state}
	planChecksum, err := kkaimigrate.VideoWebhookPlanChecksum(db)
	if err != nil {
		return err
	}
	status["plan_checksum"] = planChecksum
	if dialect == kkaimigrate.DialectPostgres {
		name, schema, err := kkaimigrate.TenantOnlineDatabaseIdentity(ctx, db)
		if err != nil {
			return err
		}
		expected, err := kkaimigrate.ContractForDialect(dialect)
		if err != nil {
			return err
		}
		status["database_name"], status["database_schema"] = name, schema
		status["required_source_digest"] = expected.MigrationSetDigest
		status["expansion_digest"] = "sha256:" + planChecksum
		if operation == "apply" {
			raw, err := readRootControllerProofJSON(proofPath, videoWebhookProofPath)
			if err != nil {
				return err
			}
			var proof canonical21ExpansionProof
			if err := common.Unmarshal(raw, &proof); err != nil {
				return fmt.Errorf("invalid video webhook controller proof JSON")
			}
			if proof.Schema != "kkai_video_webhook_expansion_proof_v1" || proof.Protocol != kkaimigrate.VideoWebhookExpansion ||
				proof.Operation != operation || proof.SourceVersion != kkaimigrate.MembershipBillingRecoverySchemaVersion || proof.TargetVersion != kkaimigrate.MembershipBillingRecoverySchemaVersion ||
				proof.SourceDigest != expected.MigrationSetDigest || proof.PlanChecksum != planChecksum ||
				proof.ExpansionDigest != "sha256:"+planChecksum {
				return fmt.Errorf("video webhook proof does not match the compiled plan")
			}
			authorize := func(ctx context.Context, db *gorm.DB) error {
				name, schema, err := kkaimigrate.TenantOnlineDatabaseIdentity(ctx, db)
				if err != nil {
					return err
				}
				if name == "" || name != proof.DatabaseName || schema == "" || schema != proof.DatabaseSchema {
					return fmt.Errorf("video webhook proof does not match the database")
				}
				return validateCanonical21ExpansionRuntimeEvidence(&proof, time.Now(), func(runtime kkaimigrate.SchemaContract) error {
					if !reflect.DeepEqual(runtime, expected) {
						return kkaimigrate.ErrUnsafeMigration
					}
					return nil
				})
			}
			if err := authorize(ctx, db); err != nil {
				return err
			}
			if err := kkaimigrate.ApplyVideoWebhookExpansion(ctx, db, kkaimigrate.Options{AuthorizeMutation: authorize}); err != nil {
				return err
			}
			status["state"] = "ready"
		}
	} else if operation == "apply" {
		return kkaimigrate.ErrUnsafeMigration
	}
	if operation == "check" && state != "ready" {
		return kkaimigrate.ErrSchemaNotReady
	}
	encoded, err := common.Marshal(status)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(output, string(encoded))
	return err
}
