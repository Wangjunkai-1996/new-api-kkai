//go:build !kkai_bridge

package kkaimigrate

// Reseller traffic requires the completed online contract, never a partial expand.
const (
	RuntimeMinVersion      int64 = TenantOnlineSchemaVersion
	RuntimeMaxVersion      int64 = TenantOnlineSchemaVersion
	MigrationTargetVersion int64 = TenantOnlineSchemaVersion
	RequiredRuntimeVersion int64 = RuntimeMinVersion
	MaxCompatibleVersion   int64 = RuntimeMaxVersion
)
