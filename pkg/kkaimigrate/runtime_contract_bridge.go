//go:build kkai_bridge

package kkaimigrate

// The image async expand leaves the v9 application models unchanged. New image
// workflows stay disabled until the v10 sidecar tables have been verified.
const (
	RuntimeMinVersion      int64 = RC41ArchitectureSchemaVersion
	RuntimeMaxVersion      int64 = ImageAsyncSchemaVersion
	MigrationTargetVersion int64 = RC41ArchitectureSchemaVersion

	RequiredRuntimeVersion int64 = RuntimeMinVersion
	MaxCompatibleVersion   int64 = RuntimeMaxVersion
)
