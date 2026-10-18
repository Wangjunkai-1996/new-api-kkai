//go:build kkai_bridge

package kkaimigrate

// Membership adds companion tables without changing the v10 application schema.
// The bridge accepts either version but never implicitly applies the v11 expand.
const (
	RuntimeMinVersion      int64 = ImageAsyncSchemaVersion
	RuntimeMaxVersion      int64 = MembershipSchemaVersion
	MigrationTargetVersion int64 = ImageAsyncSchemaVersion

	RequiredRuntimeVersion int64 = RuntimeMinVersion
	MaxCompatibleVersion   int64 = RuntimeMaxVersion
)
