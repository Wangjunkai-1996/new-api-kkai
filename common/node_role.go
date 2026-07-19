package common

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type NodeRole string

const (
	NodeRoleEnvironmentVariable = "KKAI_NODE_ROLE"
	SchemaManagementRuntime     = "runtime"
	SchemaManagementExternal    = "external"

	NodeRoleStandbyReadonly NodeRole = "standby-readonly"
	NodeRoleServing         NodeRole = "serving"
	NodeRoleLeader          NodeRole = "leader"
)

var (
	ErrInvalidNodeRole = errors.New("invalid node role configuration")

	SchemaManagementMode         = SchemaManagementRuntime
	nodeRole                     = NodeRoleLeader
	writeBackgroundTasksDisabled bool
)

func InitNodeRoleFromEnvironment() error {
	rawRole := strings.TrimSpace(os.Getenv(NodeRoleEnvironmentVariable))
	role := NodeRole(strings.ToLower(rawRole))
	if role == "" {
		if strings.EqualFold(strings.TrimSpace(os.Getenv("NODE_TYPE")), "slave") {
			role = NodeRoleStandbyReadonly
		} else {
			role = NodeRoleLeader
		}
	}
	switch role {
	case NodeRoleStandbyReadonly, NodeRoleServing, NodeRoleLeader:
	default:
		return fmt.Errorf("%w: %s", ErrInvalidNodeRole, role)
	}

	disabled := false
	if raw := strings.TrimSpace(os.Getenv("DISABLE_BACKGROUND_TASKS")); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			return fmt.Errorf("%w: DISABLE_BACKGROUND_TASKS", ErrInvalidNodeRole)
		}
		disabled = parsed
	}

	nodeRole = role
	writeBackgroundTasksDisabled = disabled
	IsMasterNode = role != NodeRoleStandbyReadonly
	return nil
}

func CurrentNodeRole() NodeRole {
	return nodeRole
}

func CanRunReadOnlyBackgroundJobs() bool {
	return true
}

func CanRunWriteBackgroundJobs() bool {
	return nodeRole == NodeRoleLeader && !writeBackgroundTasksDisabled
}

func CanRunSchemaMigrations() bool {
	return IsMasterNode && nodeRole == NodeRoleLeader
}

func CanRunRuntimeAutoMigrate() bool {
	if SchemaManagementMode != SchemaManagementRuntime {
		return false
	}
	return CanRunSchemaMigrations()
}

func IsStandbyReadonly() bool {
	return nodeRole == NodeRoleStandbyReadonly
}

func WriteBackgroundTasksDisabled() bool {
	return writeBackgroundTasksDisabled
}
