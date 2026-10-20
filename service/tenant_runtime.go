package service

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/kkaimigrate"
	"gorm.io/gorm"
)

// TenantRuntimeSnapshot describes the initialized process, not a second binary
// executed inside its container. Readiness stays frozen across online DDL.
type TenantRuntimeSnapshot struct {
	Protocol               string                     `json:"protocol"`
	PlanChecksum           string                     `json:"plan_checksum"`
	TenantAware            bool                       `json:"tenant_aware"`
	Version                string                     `json:"version"`
	StartTime              int64                      `json:"start_time"`
	NodeRole               common.NodeRole            `json:"node_role"`
	SchemaManagement       string                     `json:"schema_management"`
	TenantRoutingEnabled   bool                       `json:"tenant_routing_enabled"`
	SeparateLogDatabase    bool                       `json:"separate_log_database"`
	SchemaAtStartup        string                     `json:"schema_at_startup"`
	ResellerReadyAtStartup bool                       `json:"reseller_ready_at_startup"`
	BackgroundJobsEnabled  bool                       `json:"background_jobs_enabled"`
	SchemaContract         kkaimigrate.SchemaContract `json:"schema_contract"`
}

func CaptureTenantRuntime(db *gorm.DB) TenantRuntimeSnapshot {
	snapshot := TenantRuntimeSnapshot{
		Protocol: "tenant-online-v1", PlanChecksum: kkaimigrate.TenantOnlinePlanChecksum(),
		TenantAware: true, Version: common.Version, StartTime: common.StartTime,
		NodeRole: common.CurrentNodeRole(), SchemaManagement: common.SchemaManagementMode,
		TenantRoutingEnabled: strings.EqualFold(strings.TrimSpace(os.Getenv("TENANT_ROUTING_ENABLED")), "true"),
		SeparateLogDatabase:  strings.TrimSpace(os.Getenv("LOG_SQL_DSN")) != "",
		SchemaAtStartup:      "unready", BackgroundJobsEnabled: common.CanRunWriteBackgroundJobs(),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if db != nil {
		snapshot.SchemaContract, _ = kkaimigrate.ContractForDialect(db.Dialector.Name())
	}
	if kkaimigrate.CheckTenantOnlineSchema(ctx, db) == nil {
		snapshot.SchemaAtStartup = "finalized"
		snapshot.ResellerReadyAtStartup = true
	} else if kkaimigrate.CheckTenantOnlineExpansion(ctx, db) == nil {
		snapshot.SchemaAtStartup = "expanded"
	}
	return snapshot
}
