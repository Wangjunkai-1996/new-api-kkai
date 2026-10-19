package service

import (
	"context"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

// TenantPricingService keeps tenant price selection and immutable quotes out
// of relay adapters and the platform's global ratio settings.
type TenantPricingService struct {
	db *gorm.DB
}

func NewTenantPricingService(db *gorm.DB) *TenantPricingService {
	return &TenantPricingService{db: db}
}

func (s *TenantPricingService) CreateQuote(ctx context.Context, input model.ResellerPricingQuoteInput) (*model.PricingQuote, error) {
	if s == nil || s.db == nil {
		return nil, model.ErrResellerFinanceInvalidInput
	}
	var quote *model.PricingQuote
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		quote, err = model.CreateResellerPricingQuoteTx(tx, input)
		return err
	})
	return quote, err
}

// ResellerFinanceService owns the transaction boundaries for wallet and
// append-only ledger mutations. Callers should pass a request-scoped context
// and never construct wallet updates directly.
type ResellerFinanceService struct {
	db *gorm.DB
}

func NewResellerFinanceService(db *gorm.DB) *ResellerFinanceService {
	return &ResellerFinanceService{db: db}
}

func (s *ResellerFinanceService) RecordPaymentLot(ctx context.Context, input model.ResellerPaymentLotInput) (*model.CustomerPaymentLot, error) {
	if s == nil || s.db == nil {
		return nil, model.ErrResellerFinanceInvalidInput
	}
	var lot *model.CustomerPaymentLot
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		lot, err = model.CreateCustomerPaymentLotTx(tx, input)
		return err
	})
	return lot, err
}

func (s *ResellerFinanceService) SettleTenantConsumption(ctx context.Context, input model.ResellerSettlementInput) (*model.ResellerLedgerEntry, error) {
	if s == nil || s.db == nil {
		return nil, model.ErrResellerFinanceInvalidInput
	}
	var entry *model.ResellerLedgerEntry
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		entry, err = model.SettleResellerConsumptionTx(tx, input)
		return err
	})
	return entry, err
}

func (s *ResellerFinanceService) ReleasePending(ctx context.Context, tenantID int64, now int64, limit int) (int, error) {
	if s == nil || s.db == nil {
		return 0, model.ErrResellerFinanceInvalidInput
	}
	var released int
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		released, err = model.ReleaseResellerPendingTx(tx, tenantID, now, limit)
		return err
	})
	return released, err
}

func (s *ResellerFinanceService) ReverseSettlement(ctx context.Context, input model.ResellerChargebackInput) (*model.ResellerLedgerEntry, error) {
	if s == nil || s.db == nil {
		return nil, model.ErrResellerFinanceInvalidInput
	}
	var entry *model.ResellerLedgerEntry
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		entry, err = model.ReverseResellerSettlementTx(tx, input)
		return err
	})
	return entry, err
}

func (s *ResellerFinanceService) RequestWithdrawal(ctx context.Context, input model.ResellerWithdrawalInput) (*model.ResellerWithdrawal, error) {
	if s == nil || s.db == nil {
		return nil, model.ErrResellerFinanceInvalidInput
	}
	var withdrawal *model.ResellerWithdrawal
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		withdrawal, err = model.RequestResellerWithdrawalTx(tx, input)
		return err
	})
	return withdrawal, err
}

func (s *ResellerFinanceService) ReviewWithdrawal(ctx context.Context, input model.ResellerWithdrawalReviewInput) (*model.ResellerWithdrawal, error) {
	if s == nil || s.db == nil {
		return nil, model.ErrResellerFinanceInvalidInput
	}
	var withdrawal *model.ResellerWithdrawal
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		withdrawal, err = model.ReviewResellerWithdrawalTx(tx, input)
		return err
	})
	return withdrawal, err
}

func (s *ResellerFinanceService) ReversePayment(ctx context.Context, input model.CustomerPaymentReversalInput) (*model.CustomerPaymentReversal, error) {
	if s == nil || s.db == nil {
		return nil, model.ErrResellerFinanceInvalidInput
	}
	var reversal *model.CustomerPaymentReversal
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		reversal, err = model.ReverseCustomerPaymentTx(tx, input)
		return err
	})
	if err == nil {
		if cacheErr := model.InvalidateUserCache(reversal.CustomerID); cacheErr != nil {
			common.SysError("payment reversal user cache invalidation: " + cacheErr.Error())
		}
	}
	return reversal, err
}

func (s *ResellerFinanceService) ReconcileConsumption(ctx context.Context, input model.TenantConsumptionReconciliationInput) (*model.ResellerLedgerEntry, error) {
	if s == nil || s.db == nil {
		return nil, model.ErrResellerFinanceInvalidInput
	}
	var entry *model.ResellerLedgerEntry
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		entry, err = model.ReconcileTenantConsumptionTx(tx, input)
		return err
	})
	return entry, err
}
