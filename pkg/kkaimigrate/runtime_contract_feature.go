//go:build !kkai_bridge

package kkaimigrate

// This compatibility runtime serves final tenant schema 14 before and after
// the separately controlled nullable application-prefix expansion to schema 15.
// Startup never performs that expansion.
const (
	RuntimeMinVersion      int64 = TenantOnlineSchemaVersion
	RuntimeMaxVersion      int64 = ResellerApplicationSchemaVersion
	MigrationTargetVersion int64 = TenantOnlineSchemaVersion
	RequiredRuntimeVersion int64 = RuntimeMinVersion
	MaxCompatibleVersion   int64 = RuntimeMaxVersion
)
