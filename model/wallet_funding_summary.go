package model

import (
	"context"
	"math/big"
	"sort"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"gorm.io/gorm"
)

// GetWalletFundingSummary reads settled cash evidence and net invitation credits.
// Expected checkout amounts and wallet balances cannot establish cash paid.
func GetWalletFundingSummary(ctx context.Context, scope WalletTopUpScope) (*dto.WalletFundingSummary, error) {
	if err := validateWalletScope(scope); err != nil {
		return nil, err
	}
	if !WalletPaymentSchemaAvailable() {
		return nil, ErrWalletPaymentSchema
	}
	epoch, err := common.CurrentCreditEpoch()
	if err != nil {
		return nil, err
	}
	if epoch != nil {
		if _, exists := epoch.LegacyMaxIDs["kkai_internal_balance_adjustments"]; !exists {
			return nil, common.ErrCreditEpochInvalid
		}
	}
	result := &dto.WalletFundingSummary{CashPayments: []dto.WalletMoney{}, RebateCreditedQuota: "0", QuotaEpoch: common.CreditEpochLegacy, ServerTime: common.GetTimestamp()}
	if epoch != nil {
		result.QuotaEpoch = common.CreditEpochUSD
	}
	err = DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Platform operators can authenticate on tenant hosts. Their main-site
		// ledger must never become a tenant's personal funding summary.
		var user User
		if err := tx.Select("id").Where("id = ? AND tenant_id = ?", scope.UserID, scope.TenantID).First(&user).Error; err != nil {
			return err
		}
		q := tx.Model(&TopUp{}).Where("top_ups.user_id = ? AND top_ups.tenant_id = ?", scope.UserID, scope.TenantID)
		rows, err := walletConfirmedPaymentQuery(q, result.ServerTime).Rows()
		if err != nil {
			return err
		}
		type currencyKey struct {
			currency string
			exponent int
		}
		totals := make(map[currencyKey]*big.Int)
		for rows.Next() {
			var row walletConfirmedPayment
			if err := tx.ScanRows(rows, &row); err != nil {
				rows.Close()
				return err
			}
			if row.AmountMinor == nil {
				result.UnknownPaymentCount++
				continue
			}
			if !row.hasValidCurrency() {
				rows.Close()
				return ErrWalletPaymentEvidence
			}
			if *row.AmountMinor == 0 {
				continue // A verified zero-payment checkout is known, but is not cash.
			}
			key := currencyKey{row.Currency, row.CurrencyExponent}
			if totals[key] == nil {
				totals[key] = new(big.Int)
			}
			totals[key].Add(totals[key], big.NewInt(*row.AmountMinor))
			result.ConfirmedPaymentCount++
		}
		readErr := rows.Err()
		if err := rows.Close(); err != nil {
			return err
		}
		if readErr != nil {
			return readErr
		}
		for key, total := range totals {
			result.CashPayments = append(result.CashPayments, dto.WalletMoney{AmountMinor: total.String(), Currency: key.currency, Exponent: key.exponent})
		}
		sort.Slice(result.CashPayments, func(i, j int) bool {
			a, b := result.CashPayments[i], result.CashPayments[j]
			if a.Currency == b.Currency {
				return a.Exponent < b.Exponent
			}
			return a.Currency < b.Currency
		})
		credits, err := tx.Model(&KKAIInternalBalanceAdjustment{}).
			Select("id", "delta", "wallet_delta", "source_epoch").
			Where("user_id = ? AND reason IN ?", scope.UserID, []string{KKAIBalanceAdjustmentReasonCredit, KKAIBalanceAdjustmentReasonReversal}).Rows()
		if err != nil {
			return err
		}
		defer credits.Close()
		net := new(big.Int)
		for credits.Next() {
			var row KKAIInternalBalanceAdjustment
			if err := tx.ScanRows(credits, &row); err != nil {
				return err
			}
			value := row.Delta
			if row.WalletDelta != nil {
				value = *row.WalletDelta
			}
			if epoch != nil && epoch.IsLegacy("kkai_internal_balance_adjustments", row.ID) {
				value = common.LegacyCreditToUSD(value)
			} else if epoch != nil {
				if row.WalletDelta == nil || row.SourceEpoch != common.CreditEpochLegacy && row.SourceEpoch != common.CreditEpochUSD {
					return common.ErrCreditEpochInvalid
				}
			} else if row.SourceEpoch == common.CreditEpochUSD {
				return common.ErrCreditEpochInvalid
			}
			net.Add(net, big.NewInt(value))
		}
		result.RebateCreditedQuota = net.String()
		return credits.Err()
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
