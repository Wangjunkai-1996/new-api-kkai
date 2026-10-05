package model

// KKAIInternalBalanceAdjustment is the immutable ledger used by trusted KKAI
// services to apply invitation credits and exact reversals.
type KKAIInternalBalanceAdjustment struct {
	ID                  int64   `json:"id" gorm:"primaryKey;autoIncrement"`
	OperationID         string  `json:"operation_id" gorm:"type:varchar(128);not null;uniqueIndex"`
	UserID              int     `json:"user_id" gorm:"not null;index"`
	Delta               int64   `json:"delta" gorm:"type:bigint;not null"`
	Reason              string  `json:"reason" gorm:"type:varchar(64);not null"`
	Metadata            string  `json:"metadata" gorm:"type:text;not null"`
	PayloadSHA256       string  `json:"-" gorm:"type:char(64);not null"`
	OriginalOperationID *string `json:"original_operation_id,omitempty" gorm:"type:varchar(128);uniqueIndex"`
	BalanceBefore       int64   `json:"balance_before" gorm:"type:bigint;not null"`
	BalanceAfter        int64   `json:"balance_after" gorm:"type:bigint;not null"`
	CreatedAt           int64   `json:"created_at" gorm:"type:bigint;not null;index"`
	WalletDelta         *int64  `json:"wallet_delta,omitempty" gorm:"type:bigint"`
	SourceEpoch         string  `json:"source_epoch,omitempty" gorm:"type:varchar(32)"`
}

func (KKAIInternalBalanceAdjustment) TableName() string {
	return "kkai_internal_balance_adjustments"
}
