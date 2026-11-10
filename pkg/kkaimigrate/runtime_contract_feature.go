//go:build !kkai_bridge

package kkaimigrate

// rc49 requires canonical21 plus the separately receipted online expansion.
// The expansion leaves canonical history unchanged so accepted max21 binaries
// remain usable for rollback. ContractForDialect declares the extra requirement.
const (
	RuntimeMinVersion      int64 = MembershipBillingRecoverySchemaVersion
	RuntimeMaxVersion      int64 = MembershipBillingRecoverySchemaVersion
	MigrationTargetVersion int64 = MembershipBillingRecoverySchemaVersion
	RequiredRuntimeVersion int64 = RuntimeMinVersion
	MaxCompatibleVersion   int64 = RuntimeMaxVersion
)
