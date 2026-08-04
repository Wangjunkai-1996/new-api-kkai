//go:build kkai_bridge

package kkaimigrate

const (
	RuntimeMinVersion      int64 = ImageStudioSchemaVersion
	RuntimeMaxVersion      int64 = AuthenticationSchemaVersion
	MigrationTargetVersion int64 = ImageStudioSchemaVersion

	RequiredRuntimeVersion int64 = RuntimeMinVersion
	MaxCompatibleVersion   int64 = RuntimeMaxVersion
)
