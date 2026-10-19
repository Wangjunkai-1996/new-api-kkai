//go:build !kkai_bridge

package kkaimigrate

// This binary queries tenant columns even with tenant routing disabled. Both
// build profiles require v12; the bridge tag does not promise old-schema rollback.
const (
	RuntimeMinVersion      int64 = TenantSchemaVersion
	RuntimeMaxVersion      int64 = TenantSchemaVersion
	MigrationTargetVersion int64 = TenantSchemaVersion
	RequiredRuntimeVersion int64 = RuntimeMinVersion
	MaxCompatibleVersion   int64 = RuntimeMaxVersion
)
