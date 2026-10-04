//go:build !kkai_bridge

package kkaimigrate

const (
	RuntimeMinVersion      int64 = RC41ArchitectureSchemaVersion
	RuntimeMaxVersion      int64 = RC41ArchitectureSchemaVersion
	MigrationTargetVersion int64 = RC41ArchitectureSchemaVersion

	RequiredRuntimeVersion int64 = RuntimeMinVersion
	MaxCompatibleVersion   int64 = RuntimeMaxVersion
)
