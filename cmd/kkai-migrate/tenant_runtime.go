package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/kkaimigrate"
)

// This declares the code compiled into the inspected image. The controller
// separately verifies the live container ID, immutable image ID and HTTP version.
func writeTenantRuntimeContract(dialect string, output io.Writer) error {
	contract, err := kkaimigrate.ContractForDialect(dialect)
	if err != nil {
		return err
	}
	routing := strings.TrimSpace(os.Getenv("TENANT_ROUTING_ENABLED"))
	if routing != "" && !strings.EqualFold(routing, "false") && !strings.EqualFold(routing, "true") {
		return fmt.Errorf("invalid tenant routing environment")
	}
	result := struct {
		Schema               string `json:"schema"`
		Protocol             string `json:"protocol"`
		PlanChecksum         string `json:"plan_checksum"`
		TenantAware          bool   `json:"tenant_aware"`
		TenantRoutingEnabled bool   `json:"tenant_routing_enabled"`
		SeparateLogDatabase  bool   `json:"separate_log_database"`
		NodeRole             string `json:"node_role"`
		SchemaManagement     string `json:"schema_management"`
		kkaimigrate.SchemaContract
	}{
		Schema:               "kkai_tenant_runtime_v1",
		Protocol:             "tenant-online-v1",
		PlanChecksum:         kkaimigrate.TenantOnlinePlanChecksum(),
		TenantAware:          true,
		TenantRoutingEnabled: strings.EqualFold(routing, "true"),
		SeparateLogDatabase:  strings.TrimSpace(os.Getenv("LOG_SQL_DSN")) != "",
		NodeRole:             strings.TrimSpace(os.Getenv("KKAI_NODE_ROLE")),
		SchemaManagement:     common.SchemaManagementMode,
		SchemaContract:       contract,
	}
	encoded, err := common.Marshal(result)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(output, string(encoded))
	return err
}
