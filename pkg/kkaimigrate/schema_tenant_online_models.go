package kkaimigrate

import (
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// Aggregate rational denominators grow across immutable tariff versions. These
// v14 snapshots deliberately leave the historical v12/v13 column shapes intact.
type tenantOnlineExactAggregate string

func (tenantOnlineExactAggregate) GormDataType() string { return "string" }

func (tenantOnlineExactAggregate) GormDBDataType(db *gorm.DB, _ *schema.Field) string {
	if db.Dialector.Name() == DialectMySQL {
		return "longtext"
	}
	return "text"
}

type tenantOnlineResellerWallet struct {
	ID               int64                      `gorm:"primaryKey"`
	TenantID         int64                      `gorm:"not null;uniqueIndex"`
	PendingMinor     int64                      `gorm:"not null"`
	AvailableMinor   int64                      `gorm:"not null"`
	FrozenMinor      int64                      `gorm:"not null"`
	DebtMinor        int64                      `gorm:"not null"`
	ExactProfitCarry tenantOnlineExactAggregate `gorm:"not null"`
	Version          int64                      `gorm:"not null"`
	CreatedAt        int64                      `gorm:"not null"`
	UpdatedAt        int64                      `gorm:"not null"`
}

func (tenantOnlineResellerWallet) TableName() string { return "reseller_wallets" }

type tenantOnlineCustomerPaymentReversal struct {
	ID                       int64                      `gorm:"primaryKey"`
	TenantID                 int64                      `gorm:"not null;index;uniqueIndex:idx_customer_payment_reversal_idempotency,priority:1"`
	IdempotencyKey           string                     `gorm:"type:varchar(128);not null;uniqueIndex:idx_customer_payment_reversal_idempotency,priority:2"`
	PaymentLotID             int64                      `gorm:"not null;uniqueIndex"`
	TopUpID                  int64                      `gorm:"not null;uniqueIndex"`
	CustomerID               int                        `gorm:"not null;index"`
	Kind                     string                     `gorm:"type:varchar(16);not null"`
	PaidMinor                int64                      `gorm:"not null"`
	PaymentFeeMinor          int64                      `gorm:"not null"`
	RemovedQuota             int64                      `gorm:"not null"`
	ReversedProfitMinorExact tenantOnlineExactAggregate `gorm:"not null"`
	LedgerEntryIDs           string                     `gorm:"type:text;not null"`
	ActorID                  int                        `gorm:"not null"`
	Reason                   string                     `gorm:"type:text;not null"`
	PaymentReference         string                     `gorm:"type:varchar(192);not null"`
	PayloadHash              string                     `gorm:"type:char(64);not null"`
	CreatedAt                int64                      `gorm:"not null"`
}

func (tenantOnlineCustomerPaymentReversal) TableName() string { return "customer_payment_reversals" }

func tenantOnlineNewModels() []any {
	models := tenantV12NewModels()
	for i, value := range models {
		switch value.(type) {
		case *tenantV12ResellerWallet:
			models[i] = &tenantOnlineResellerWallet{}
		case *tenantV12CustomerPaymentReversal:
			models[i] = &tenantOnlineCustomerPaymentReversal{}
		}
	}
	return models
}
