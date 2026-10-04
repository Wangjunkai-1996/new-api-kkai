//go:build kkai_bridge

package kkaimigrate

// rc.41 has no v7/v8 runtime fallback. The legacy build tag must not advertise
// a schema that the new authentication and task-plugin runtime cannot serve.
const (
	RuntimeMinVersion      int64 = RC41ArchitectureSchemaVersion
	RuntimeMaxVersion      int64 = RC41ArchitectureSchemaVersion
	MigrationTargetVersion int64 = RC41ArchitectureSchemaVersion

	RequiredRuntimeVersion int64 = RuntimeMinVersion
	MaxCompatibleVersion   int64 = RuntimeMaxVersion
)
