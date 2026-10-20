//go:build !kkai_bridge

package kkaimigrate

// The installed production controller currently accepts final tenant schema 14.
// The optional application-prefix expand is cataloged for a later controller
// transition and is never performed or advertised by this runtime.
const (
	RuntimeMinVersion      int64 = TenantOnlineSchemaVersion
	RuntimeMaxVersion      int64 = TenantOnlineSchemaVersion
	MigrationTargetVersion int64 = TenantOnlineSchemaVersion
	RequiredRuntimeVersion int64 = RuntimeMinVersion
	MaxCompatibleVersion   int64 = RuntimeMaxVersion
)
