//go:build !kkai_bridge

package kkaimigrate

// The feature runtime requires the complete, verified image async schema.
const (
	RuntimeMinVersion      int64 = ImageAsyncSchemaVersion
	RuntimeMaxVersion      int64 = ImageAsyncSchemaVersion
	MigrationTargetVersion int64 = ImageAsyncSchemaVersion

	RequiredRuntimeVersion int64 = RuntimeMinVersion
	MaxCompatibleVersion   int64 = RuntimeMaxVersion
)
