//go:build !kkai_bridge

package kkaimigrate

// This compatibility runtime serves final tenant schema 14 before and after
// the separately controlled application-prefix (15) and request-ticket (16)
// expansions. Startup never performs either expansion.
const (
	RuntimeMinVersion      int64 = TenantOnlineSchemaVersion
	RuntimeMaxVersion      int64 = RequestTicketSchemaVersion
	MigrationTargetVersion int64 = TenantOnlineSchemaVersion
	RequiredRuntimeVersion int64 = RuntimeMinVersion
	MaxCompatibleVersion   int64 = RuntimeMaxVersion
)
