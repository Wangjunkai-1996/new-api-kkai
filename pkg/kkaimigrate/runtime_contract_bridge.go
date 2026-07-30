//go:build kkai_bridge

package kkaimigrate

const (
	RuntimeMinVersion      int64 = JobLeaseSchemaVersion
	RuntimeMaxVersion      int64 = VideoSampleCategorySchemaVersion
	MigrationTargetVersion int64 = JobLeaseSchemaVersion

	RequiredRuntimeVersion int64 = RuntimeMinVersion
	MaxCompatibleVersion   int64 = RuntimeMaxVersion
)
