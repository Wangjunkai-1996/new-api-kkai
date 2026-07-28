//go:build !kkai_bridge

package kkaimigrate

const (
	RuntimeMinVersion      int64 = VideoStudioSchemaVersion
	RuntimeMaxVersion      int64 = VideoStudioSchemaVersion
	MigrationTargetVersion int64 = VideoStudioSchemaVersion

	RequiredRuntimeVersion int64 = RuntimeMinVersion
	MaxCompatibleVersion   int64 = RuntimeMaxVersion
)
