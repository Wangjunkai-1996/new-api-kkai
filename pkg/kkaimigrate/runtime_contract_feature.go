//go:build !kkai_bridge

package kkaimigrate

// This preparation runtime serves the completed tenant contract before and after
// the optional application-prefix expand. It never performs that expand at startup.
const (
	RuntimeMinVersion      int64 = TenantOnlineSchemaVersion
	RuntimeMaxVersion      int64 = ResellerApplicationSchemaVersion
	MigrationTargetVersion int64 = TenantOnlineSchemaVersion
	RequiredRuntimeVersion int64 = RuntimeMinVersion
	MaxCompatibleVersion   int64 = RuntimeMaxVersion
)
