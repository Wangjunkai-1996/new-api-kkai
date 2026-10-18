//go:build !kkai_bridge

package kkaimigrate

// The feature runtime requires the complete, verified membership schema.
const (
	RuntimeMinVersion      int64 = MembershipSchemaVersion
	RuntimeMaxVersion      int64 = MembershipSchemaVersion
	MigrationTargetVersion int64 = MembershipSchemaVersion

	RequiredRuntimeVersion int64 = RuntimeMinVersion
	MaxCompatibleVersion   int64 = RuntimeMaxVersion
)
