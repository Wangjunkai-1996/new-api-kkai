//go:build kkai_bridge

package kkaimigrate

// This binary queries tenant columns even with tenant routing disabled. Both
// build profiles require v13; the bridge tag does not promise old-schema rollback.
const (
	RuntimeMinVersion      int64 = TenantCompatibilitySchemaVersion
	RuntimeMaxVersion      int64 = TenantCompatibilitySchemaVersion
	MigrationTargetVersion int64 = TenantCompatibilitySchemaVersion
	RequiredRuntimeVersion int64 = RuntimeMinVersion
	MaxCompatibleVersion   int64 = RuntimeMaxVersion
)
