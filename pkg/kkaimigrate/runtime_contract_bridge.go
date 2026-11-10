//go:build kkai_bridge

package kkaimigrate

// Both rc49 profiles require the same canonical21 plus expansion contract.
// This build tag does not turn rc49 into the historical compatibility binary.
const (
	RuntimeMinVersion      int64 = MembershipBillingRecoverySchemaVersion
	RuntimeMaxVersion      int64 = MembershipBillingRecoverySchemaVersion
	MigrationTargetVersion int64 = MembershipBillingRecoverySchemaVersion
	RequiredRuntimeVersion int64 = RuntimeMinVersion
	MaxCompatibleVersion   int64 = RuntimeMaxVersion
)
