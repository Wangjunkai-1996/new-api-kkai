//go:build !kkai_bridge

package kkaimigrate

const (
	RuntimeMinVersion      int64 = VideoSampleCategorySchemaVersion
	RuntimeMaxVersion      int64 = VideoSampleCategorySchemaVersion
	MigrationTargetVersion int64 = VideoSampleCategorySchemaVersion

	RequiredRuntimeVersion int64 = RuntimeMinVersion
	MaxCompatibleVersion   int64 = RuntimeMaxVersion
)
