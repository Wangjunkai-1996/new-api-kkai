//go:build kkai_bridge

package kkaimigrate

// The bridge serves only fully verified online-expanded v11 or the final v14.
// It cannot run on pristine/partial v11. Historical v12/v13 are not bridge states.
const (
	RuntimeMinVersion      int64 = MembershipSchemaVersion
	RuntimeMaxVersion      int64 = TenantOnlineSchemaVersion
	MigrationTargetVersion int64 = MembershipSchemaVersion
	RequiredRuntimeVersion int64 = RuntimeMinVersion
	MaxCompatibleVersion   int64 = RuntimeMaxVersion
)
