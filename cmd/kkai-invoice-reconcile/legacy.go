package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// reconcileLegacy records the owner's explicit financial approval. Its receipt
// identity deliberately does not represent an authenticated gateway transaction.
func reconcileLegacy(ctx context.Context, db *gorm.DB, input manifest, ownerUserID int, apply, approved bool) (*report, error) {
	if input.EvidenceSource != "owner_approved_legacy" || ownerUserID < 0 || len(input.Orders) == 0 || len(input.Orders) > 100 || (apply && !approved) {
		return nil, errors.New("legacy approval requires valid ownership and both --approve-legacy and --apply to write")
	}
	seen := map[string]bool{}
	trades := make([]string, 0, len(input.Orders))
	for _, proof := range input.Orders {
		if proof.TenantID != model.PlatformTenantID || proof.UserID <= 0 || (ownerUserID > 0 && proof.UserID != ownerUserID) || !validText(proof.OrderNo, 255) || proof.ProviderReceiptID != "" || proof.AmountMinor <= 0 || proof.Currency != "CNY" || proof.PaidAt < 0 || seen[proof.OrderNo] {
			return nil, errors.New("invalid owner-approved legacy manifest; provider_receipt_id must be empty")
		}
		seen[proof.OrderNo] = true
		trades = append(trades, proof.OrderNo)
	}
	type preparedOrder struct {
		order   model.TopUp
		receipt model.InvoicePaymentReceipt
		audit   model.WalletPaymentEvent
		key     string
	}
	result := &report{Mode: "preview", EvidenceSHA256: input.EvidenceSHA256, Orders: []orderResult{}}
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var locked []model.TopUp
		if err := model.InvoiceLockForUpdate(tx).Where("tenant_id = ? AND trade_no IN ?", model.PlatformTenantID, trades).Order("id ASC").Find(&locked).Error; err != nil || len(locked) != len(trades) {
			return errors.New("legacy order lookup or ownership validation failed")
		}
		byTrade := make(map[string]model.TopUp, len(locked))
		for _, order := range locked {
			byTrade[order.TradeNo] = order
		}
		prepared := make([]preparedOrder, 0, len(input.Orders))
		// Complete every check under the order locks before creating any receipt.
		for _, proof := range input.Orders {
			order := byTrade[proof.OrderNo]
			if order.UserId != proof.UserID || order.Status != common.TopUpStatusSuccess || order.PaymentProvider != model.PaymentProviderEpay || order.WalletLifecycleVersion != 0 || order.PaidMinor < 0 || (order.PaidMinor > 0 && order.PaidMinor != proof.AmountMinor) || (order.Currency != "" && order.Currency != "CNY") || (order.ProviderPaymentState != "" && order.ProviderPaymentState != "paid" && order.ProviderPaymentState != "success") {
				return errors.New("order is not a matching successful legacy CNY Epay payment")
			}
			// Match the historical checkout/display contract: stored CNY Money
			// rounded to two decimals, never credited Amount or a recharge ratio.
			var cash struct{ Money string }
			if err := tx.Model(&model.TopUp{}).Select("money").Where("id = ?", order.Id).Take(&cash).Error; err != nil {
				return errors.New("legacy payment amount cannot be read")
			}
			money, err := decimal.NewFromString(cash.Money)
			if err != nil || !money.IsPositive() || money.Exponent() < -64 || money.Exponent() > 18 || money.GreaterThan(decimal.NewFromInt(math.MaxInt64).Shift(-2)) {
				return errors.New("legacy CNY payment amount is outside the supported range")
			}
			rounded := money.Round(2)
			minor, err := model.ParseCNYMinor(rounded.StringFixed(2))
			if err != nil || minor != proof.AmountMinor {
				return errors.New("approved amount must equal stored legacy Money rounded to two CNY decimals")
			}
			var members, subscriptions int64
			if tx.Model(&model.MembershipOrder{}).Where("trade_no = ?", order.TradeNo).Count(&members).Error != nil || tx.Model(&model.SubscriptionOrder{}).Where("trade_no = ?", order.TradeNo).Count(&subscriptions).Error != nil || members != 0 || subscriptions != 0 {
				return errors.New("legacy order aliases could not be excluded")
			}
			identity := sha256.Sum256([]byte(fmt.Sprintf("invoice:owner-approved-legacy:v1:%d:%d:%s", order.TenantID, order.Id, order.TradeNo)))
			key := hex.EncodeToString(identity[:])
			var receipt model.InvoicePaymentReceipt
			if err := model.InvoiceLockForUpdate(tx).Where("top_up_id = ?", order.Id).Limit(1).Find(&receipt).Error; err != nil {
				return errors.New("legacy invoice receipt lookup failed")
			}
			if receipt.ID != 0 && (receipt.Status != "verified" || receipt.ExceptionReceiptKey != "" || receipt.UserID != proof.UserID || receipt.TenantID != model.PlatformTenantID || receipt.Provider != model.PaymentProviderEpay || receipt.AmountMinor != minor || receipt.Currency != "CNY" || len(receipt.ReceiptKey) != 64 || receipt.PaidAt <= 0) {
				return errors.New("existing invoice evidence is anomalous; owner approval cannot replace it")
			}
			legacyClaim := sha256.Sum256([]byte("payment:v1:0:" + model.PaymentProviderEpay + ":" + order.TradeNo))
			approvedClaim := sha256.Sum256([]byte("payment:receipt:v1:0:" + key))
			claimKeys := []string{hex.EncodeToString(legacyClaim[:]), hex.EncodeToString(approvedClaim[:])}
			if receipt.ID != 0 {
				claim := sha256.Sum256([]byte("payment:receipt:v1:0:" + receipt.ReceiptKey))
				claimKeys = append(claimKeys, hex.EncodeToString(claim[:]))
			}
			var items []model.InvoiceRequestItem
			if err := model.InvoiceLockForUpdate(tx).Where("tenant_id = ? AND user_id = ? AND order_no = ? AND payment_label = ?", model.PlatformTenantID, proof.UserID, order.TradeNo, model.PaymentProviderEpay).Find(&items).Error; err != nil {
				return errors.New("legacy invoice item lookup failed")
			}
			for _, item := range items {
				claimKeys = append(claimKeys, item.CanonicalPaymentKey)
			}
			var claim model.InvoiceOrderClaim
			if err := model.InvoiceLockForUpdate(tx).Where("tenant_id = ? AND canonical_payment_key IN ?", model.PlatformTenantID, claimKeys).Limit(1).Find(&claim).Error; err != nil || claim.ID != 0 {
				return errors.New("existing invoice claim prevents legacy approval")
			}
			var priorAudit model.WalletPaymentEvent
			if err := tx.Where("top_up_id = ? AND kind = ? AND source = ? AND status = ?", order.Id, "invoice_reconcile", input.EvidenceSource, "approved").Order("id ASC").Limit(1).Find(&priorAudit).Error; err != nil {
				return errors.New("legacy approval audit lookup failed")
			}
			paidAt, estimated := order.CompleteTime, false
			if paidAt <= 0 {
				paidAt, estimated = order.CreateTime, true
			}
			if paidAt <= 0 || paidAt > common.GetTimestamp() || paidAt < order.CreateTime || (proof.PaidAt != 0 && proof.PaidAt != paidAt) {
				return errors.New("legacy paid_at must be zero or the existing completion/create time")
			}
			if priorAudit.ReasonCode == "HISTORICAL_PAYMENT_TIME_ESTIMATED" {
				estimated = true
			}
			status, reason := "ready_for_owner_approval", "OWNER_APPROVED_LEGACY_PAYMENT"
			if receipt.ID != 0 {
				status = "preserved_verified"
				if receipt.ReceiptKey != key {
					paidAt, estimated = receipt.PaidAt, false
					reason = "EXISTING_VERIFIED_RECEIPT_PRESERVED"
				}
			}
			if estimated {
				reason = "HISTORICAL_PAYMENT_TIME_ESTIMATED"
			}
			amountRounded := !money.Equal(rounded)
			result.Orders = append(result.Orders, orderResult{UserID: proof.UserID, OrderNo: order.TradeNo, AmountMinor: minor, Currency: "CNY", PaidAt: paidAt, EstimatedPaidAt: estimated, AmountRounded: amountRounded, Status: status})
			prepared = append(prepared, preparedOrder{order: order, receipt: receipt, key: key, audit: model.WalletPaymentEvent{TopUpID: order.Id, Source: input.EvidenceSource, EvidenceRef: fmt.Sprintf("sha256:%s;operator:%s;estimated_paid_at:%t;amount_rounded:%t", input.EvidenceSHA256[:16], input.Operator, estimated, amountRounded), EvidenceHash: input.EvidenceSHA256, Kind: "invoice_reconcile", Status: "approved", ReasonCode: reason, CreatedAt: common.GetTimestamp()}})
		}
		if !apply {
			return nil
		}
		for i, row := range prepared {
			payment := result.Orders[i]
			updates := map[string]any{}
			if row.order.PaidMinor == 0 {
				updates["paid_minor"] = payment.AmountMinor
			}
			if row.order.Currency == "" {
				updates["currency"] = "CNY"
			}
			if row.order.CompleteTime <= 0 {
				updates["complete_time"] = payment.PaidAt
			}
			if len(updates) > 0 {
				if err := tx.Model(&model.TopUp{}).Where("id = ?", row.order.Id).Updates(updates).Error; err != nil {
					return errors.New("legacy payment snapshot update failed")
				}
			}
			if row.receipt.ID == 0 {
				receipt := model.InvoicePaymentReceipt{TenantID: model.PlatformTenantID, UserID: row.order.UserId, TopUpID: int64(row.order.Id), Provider: model.PaymentProviderEpay, ReceiptKey: row.key, AmountMinor: payment.AmountMinor, Currency: "CNY", Status: "verified", PaidAt: payment.PaidAt, CreatedAt: common.GetTimestamp()}
				if err := tx.Create(&receipt).Error; err != nil {
					return errors.New("owner-approved legacy receipt creation failed")
				}
				result.Orders[i].Status = "owner_approved"
			}
			var audit model.WalletPaymentEvent
			if err := tx.Where("top_up_id = ? AND kind = ? AND source = ? AND evidence_hash = ? AND status = ?", row.order.Id, row.audit.Kind, row.audit.Source, row.audit.EvidenceHash, row.audit.Status).Limit(1).Find(&audit).Error; err != nil {
				return errors.New("legacy audit lookup failed")
			}
			if audit.ID == 0 {
				if err := tx.Create(&row.audit).Error; err != nil {
					return errors.New("legacy approval audit creation failed")
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("legacy approval rolled back; no payments changed: %w", err)
	}
	if apply {
		result.Mode = "apply"
	}
	return result, nil
}
