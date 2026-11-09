//go:build !kkai_bridge

package kkaimigrate

// This compatibility runtime serves final tenant schema 14 before and after
// the separately controlled application-prefix (15), request-ticket (16), invoice (17),
// wallet-payment (18), shop (19), membership-payment (20), and billing-recovery (21)
// expansions. Startup never performs these expansions.
const (
	RuntimeMinVersion      int64 = TenantOnlineSchemaVersion
	RuntimeMaxVersion      int64 = MembershipBillingRecoverySchemaVersion
	MigrationTargetVersion int64 = TenantOnlineSchemaVersion
	RequiredRuntimeVersion int64 = RuntimeMinVersion
	MaxCompatibleVersion   int64 = RuntimeMaxVersion
)
