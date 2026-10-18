//go:build kkai_bridge

package kkaimigrate

// Optional image async (v10) and membership (v11) expands leave the v9 application
// models unchanged. Each feature stays disabled until its schema is verified.
const (
	RuntimeMinVersion      int64 = RC41ArchitectureSchemaVersion
	RuntimeMaxVersion      int64 = MembershipSchemaVersion
	MigrationTargetVersion int64 = RC41ArchitectureSchemaVersion

	RequiredRuntimeVersion int64 = RuntimeMinVersion
	MaxCompatibleVersion   int64 = RuntimeMaxVersion
)
