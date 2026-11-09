package model

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"gorm.io/gorm"
)

const MembershipBillingRecoveryControlOption = "MembershipBillingRecoveryControl"

var ErrMembershipRecoveryControl = errors.New("membership recovery control is unavailable or changed")
var ErrMembershipRecoveryControlReadOnly = errors.New("membership recovery control requires the dedicated operator controller")
var membershipRecoveryGenerationPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

type MembershipRecoveryControl struct {
	Mode       string `json:"mode"`
	Generation string `json:"generation"`
}

func ParseMembershipRecoveryControl(value string) (MembershipRecoveryControl, error) {
	parts := strings.Split(value, ":")
	if len(parts) != 2 || !membershipRecoveryGenerationPattern.MatchString(parts[1]) {
		return MembershipRecoveryControl{}, ErrMembershipRecoveryControl
	}
	switch parts[0] {
	case "off", "observe", "apply", "paused":
		return MembershipRecoveryControl{Mode: parts[0], Generation: parts[1]}, nil
	default:
		return MembershipRecoveryControl{}, ErrMembershipRecoveryControl
	}
}

func ReadMembershipRecoveryControl(ctx context.Context) (MembershipRecoveryControl, error) {
	if ctx == nil || DB == nil {
		return MembershipRecoveryControl{}, ErrMembershipRecoveryControl
	}
	var option Option
	err := DB.WithContext(ctx).Where(map[string]any{"key": MembershipBillingRecoveryControlOption}).First(&option).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return MembershipRecoveryControl{Mode: "paused"}, nil
	}
	if err != nil {
		return MembershipRecoveryControl{}, err
	}
	return ParseMembershipRecoveryControl(option.Value)
}

// The control row is locked before the lease, User and Ledger. The operator's
// update waits for already admitted transactions; after it commits, no old
// generation can move money. Ordinary foreground billing never calls this.
func RequireMembershipRecoveryControlTx(tx *gorm.DB, expected MembershipRecoveryControl) error {
	if tx == nil || expected.Mode == "paused" || expected.Generation == "" {
		return ErrMembershipRecoveryControl
	}
	// SQLite has no FOR UPDATE; acquire its writer lock before the snapshot too.
	if tx.Dialector.Name() == "sqlite" {
		if err := tx.Model(&Option{}).Where(map[string]any{"key": MembershipBillingRecoveryControlOption}).UpdateColumn("value", gorm.Expr("value")).Error; err != nil {
			return err
		}
	}
	var option Option
	if err := lockForUpdate(tx).Where(map[string]any{"key": MembershipBillingRecoveryControlOption}).First(&option).Error; err != nil {
		return errors.Join(ErrMembershipRecoveryControl, err)
	}
	actual, err := ParseMembershipRecoveryControl(option.Value)
	if err != nil || actual != expected {
		return ErrMembershipRecoveryControl
	}
	return nil
}
