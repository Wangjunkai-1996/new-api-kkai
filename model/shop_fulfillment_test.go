package model

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func shopTestPaidInventory(t *testing.T, db *gorm.DB) (ShopOrder, ShopPaymentReceipt, ShopStockUnit) {
	t.Helper()
	order, _, receipt := shopTestOrder(t, db)
	snapshot, err := shopSnapshot(&order)
	require.NoError(t, err)
	snapshot.Retention.SecretAfterDeliverySeconds = 1000
	encoded, err := common.Marshal(snapshot)
	require.NoError(t, err)
	require.NoError(t, db.Model(&order).Update("snapshot_json", string(encoded)).Error)
	stored, err := SaveShopVerifiedReceipt(db, ShopVerifiedReceiptInput{Receipt: receipt, Verified: true, Now: 200})
	require.NoError(t, err)
	stored, err = MatchShopPaymentReceipt(db, stored.ID, 200)
	require.NoError(t, err)
	require.Equal(t, "matched", stored.ProcessingState)
	require.NoError(t, db.First(&order, order.ID).Error)
	stock := ShopStockUnit{ShopRecord: ShopRecord{PublicID: "stock-available", Version: 1}, ProductID: order.ProductID, State: "available", IdentityDomainHash: "domain", FingerprintKeyVersion: "fp1", IdentityFingerprint: "identity", SecretCiphertext: "encrypted-account", SecretKeyVersion: "v1"}
	require.NoError(t, db.Create(&stock).Error)
	return order, *stored, stock
}

func TestShopConcurrentInventoryCandidatesAssignOnlyOneOrder(t *testing.T) {
	db := shopTestDB(t)
	first, receipt, stock := shopTestPaidInventory(t, db)
	second := first
	second.ShopRecord = ShopRecord{PublicID: "so_second", Version: 1}
	second.ActiveAttemptID, second.EffectiveReceiptID = nil, nil
	require.NoError(t, db.Create(&second).Error)
	secondReceipt := receipt
	secondReceipt.ShopRecord = ShopRecord{PublicID: "receipt-second", Version: 1}
	secondReceipt.ReceiptKey = "receipt-second"
	secondReceipt.ProviderTransactionID = "transaction-second"
	secondReceipt.OrderID, secondReceipt.AttemptID = &second.ID, nil
	require.NoError(t, db.Create(&secondReceipt).Error)
	require.NoError(t, db.Model(&second).Update("effective_receipt_id", secondReceipt.ID).Error)
	orders := []ShopOrder{first, second}
	inputs := make([]ShopFulfillInput, len(orders))
	for i, order := range orders {
		candidate, err := GetShopFulfillmentCandidate(db, order.PublicID)
		require.NoError(t, err)
		require.Equal(t, stock.ID, candidate.ID)
		inputs[i] = ShopFulfillInput{Command: ShopCommand{Now: 210}, OrderPublicID: order.PublicID, DeliveryPublicID: fmt.Sprintf("delivery-%d", i), StockUnitID: candidate.ID, StockVersion: candidate.Version}
	}
	type outcome struct {
		index    int
		delivery *ShopDelivery
		err      error
	}
	start, results := make(chan struct{}), make(chan outcome, 2)
	for i, input := range inputs {
		go func() {
			<-start
			delivery, err := FulfillShopOrder(db, input)
			results <- outcome{index: i, delivery: delivery, err: err}
		}()
	}
	close(start)
	winner, successes := -1, 0
	for range inputs {
		result := <-results
		if result.err == nil {
			winner, successes = result.index, successes+1
			assert.Equal(t, orders[result.index].ID, result.delivery.OrderID)
		} else {
			assert.ErrorIs(t, result.err, ErrShopVersion)
		}
	}
	require.Equal(t, 1, successes)
	require.NoError(t, db.First(&stock, stock.ID).Error)
	assert.Equal(t, "sold", stock.State)
	require.NotNil(t, stock.SoldOrderID)
	assert.Equal(t, orders[winner].ID, *stock.SoldOrderID)
	replayed, err := FulfillShopOrder(db, inputs[winner])
	require.NoError(t, err)
	assert.Equal(t, inputs[winner].DeliveryPublicID, replayed.PublicID)
	var count int64
	require.NoError(t, db.Model(&ShopDelivery{}).Count(&count).Error)
	assert.Equal(t, int64(1), count)
	require.NoError(t, db.Model(&KKAIOutboxEvent{}).Where("topic = ?", ShopTopicNotification).Count(&count).Error)
	assert.Equal(t, int64(1), count)
	loser := orders[1-winner]
	require.NoError(t, db.First(&loser, loser.ID).Error)
	assert.Nil(t, loser.CurrentDeliveryID)
	assert.Equal(t, "pending", loser.DeliveryState)
}

func TestShopDeliveryAndRevealRecheckFundsFences(t *testing.T) {
	for _, fence := range []string{"unpaid", "risk_hold", "receipt_review", "refund_approved", "adjustment_pending", "adjustment_succeeded", "adjustment_disputed"} {
		for _, delivered := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/delivered=%t", fence, delivered), func(t *testing.T) {
				db := shopTestDB(t)
				order, receipt, stock := shopTestPaidInventory(t, db)
				input := ShopFulfillInput{Command: ShopCommand{Now: 210}, OrderPublicID: order.PublicID, DeliveryPublicID: "delivery", StockUnitID: stock.ID, StockVersion: stock.Version}
				reveal := ShopRevealInput{Command: ShopCommand{ActorID: 1, TenantID: PlatformTenantID, Now: 215}, OrderPublicID: order.PublicID, DeliveryPublicID: "delivery", Intent: "view"}
				if delivered {
					_, err := FulfillShopOrder(db, input)
					require.NoError(t, err)
					grant, err := RevealShopDelivery(db, reveal)
					require.NoError(t, err)
					assert.Equal(t, stock.SecretCiphertext, grant.Secret.Ciphertext)
				}
				switch fence {
				case "unpaid":
					require.NoError(t, db.Model(&order).Update("payment_state", "unpaid").Error)
				case "risk_hold":
					require.NoError(t, db.Model(&order).Update("risk_hold", true).Error)
				case "receipt_review":
					require.NoError(t, db.Model(&receipt).Update("processing_state", "review").Error)
				case "refund_approved":
					evidence, err := RegisterShopPaymentEvidence(db, ShopEvidenceInput{Command: shopTestCommand("refund-evidence", receipt.Version, 220), ReceiptPublicID: receipt.PublicID, Evidence: ShopPaymentEvidence{Source: "merchant_console", ProviderTransactionID: receipt.ProviderTransactionID, AmountMinor: 100, Currency: "CNY", CurrencyExponent: 2, Reference: "merchant/transaction", Summary: "Not refunded", ObservedStatus: "not_refunded", ObservedAt: 220}})
					require.NoError(t, err)
					require.NoError(t, db.First(&receipt, receipt.ID).Error)
					_, err = ApproveShopRefund(db, ShopRefundApproveInput{Command: shopTestCommand("refund-approve", receipt.Version, 221), ReceiptPublicID: receipt.PublicID, Kind: "goods", Reason: "approved", EvidenceIDs: []string{evidence.PublicID}})
					require.NoError(t, err)
				default:
					state := fence[len("adjustment_"):]
					require.NoError(t, db.Create(&ShopPaymentAdjustment{ShopRecord: ShopRecord{PublicID: "adjustment", Version: 1}, ReceiptID: receipt.ID, AdjustmentKey: "adjustment", Kind: "chargeback", AmountMinor: 40, Currency: "CNY", CurrencyExponent: 2, State: state}).Error)
				}
				if delivered {
					reveal.Command.Now = 230
					grant, err := RevealShopDelivery(db, reveal)
					assert.ErrorIs(t, err, ErrShopRevealBlocked)
					assert.Empty(t, grant.Secret.Ciphertext)
				} else {
					input.Command.Now = 230
					_, err := FulfillShopOrder(db, input)
					assert.ErrorIs(t, err, ErrShopRevealBlocked)
					var count int64
					require.NoError(t, db.Model(&ShopDelivery{}).Count(&count).Error)
					assert.Zero(t, count)
					require.NoError(t, db.First(&stock, stock.ID).Error)
					assert.Equal(t, "available", stock.State)
					assert.Nil(t, stock.SoldOrderID)
				}
			})
		}
	}
}

func TestShopOrderDetailPreservesCurrentResponsibilitiesBeyondHistory(t *testing.T) {
	db := shopTestDB(t)
	order, receipt, _ := shopTestPaidInventory(t, db)
	aftersale := ShopAftersale{ShopRecord: ShopRecord{PublicID: "active-aftersale", Version: 1}, OrderID: order.ID, State: "processing"}
	require.NoError(t, db.Create(&aftersale).Error)
	require.NoError(t, db.Model(&order).Update("active_aftersale_id", aftersale.ID).Error)
	refund := ShopRefundCase{ShopRecord: ShopRecord{PublicID: "goods-refund", Version: 1}, ReceiptID: receipt.ID, OrderID: &order.ID, Kind: "goods", State: "approved"}
	require.NoError(t, db.Create(&refund).Error)
	var attempts []ShopPaymentAttempt
	var receipts []ShopPaymentReceipt
	var aftersales []ShopAftersale
	for i := range 100 {
		attempts = append(attempts, ShopPaymentAttempt{ShopRecord: ShopRecord{PublicID: fmt.Sprintf("historical-attempt-%d", i), Version: 1}, OrderID: order.ID, AttemptNo: int64(i + 2), TradeNo: fmt.Sprintf("historical-trade-%d", i), CreateState: "closed"})
		receipts = append(receipts, ShopPaymentReceipt{ShopRecord: ShopRecord{PublicID: fmt.Sprintf("extra-receipt-%d", i), Version: 1}, ReceiptKey: fmt.Sprintf("extra-receipt-%d", i), OrderID: &order.ID, ProcessingState: "extra"})
		aftersales = append(aftersales, ShopAftersale{ShopRecord: ShopRecord{PublicID: fmt.Sprintf("historical-aftersale-%d", i), Version: 1}, OrderID: order.ID, State: "closed"})
	}
	require.NoError(t, db.Create(&attempts).Error)
	require.NoError(t, db.Create(&receipts).Error)
	require.NoError(t, db.Create(&aftersales).Error)
	var refunds []ShopRefundCase
	for i, extra := range receipts {
		refunds = append(refunds, ShopRefundCase{ShopRecord: ShopRecord{PublicID: fmt.Sprintf("extra-refund-%d", i), Version: 1}, ReceiptID: extra.ID, OrderID: &order.ID, Kind: "extra", State: "cancelled"})
	}
	require.NoError(t, db.Create(&refunds).Error)
	detail, err := GetShopOrderDetail(db, ShopScope{UserID: 1, TenantID: PlatformTenantID}, order.PublicID)
	require.NoError(t, err)
	assert.True(t, slices.ContainsFunc(detail.Attempts, func(v ShopPaymentAttempt) bool { return v.ID == *order.ActiveAttemptID }))
	assert.True(t, slices.ContainsFunc(detail.Aftersales, func(v ShopAftersale) bool { return v.ID == aftersale.ID && v.State == "processing" }))
	assert.True(t, slices.ContainsFunc(detail.Receipts, func(v ShopPaymentReceipt) bool { return v.ID == receipt.ID && v.AmountMinor == 100 }))
	assert.True(t, slices.ContainsFunc(detail.Refunds, func(v ShopRefundCase) bool { return v.ID == refund.ID && v.State == "approved" }))
	assert.True(t, ShopOrderDeliveryBlocked(detail))
	require.NoError(t, db.Model(&refund).Update("state", "cancelled").Error)
	detail, err = GetShopOrderDetail(db, ShopScope{UserID: 1, TenantID: PlatformTenantID}, order.PublicID)
	require.NoError(t, err)
	assert.False(t, ShopOrderDeliveryBlocked(detail))
}

func TestShopCreateOrderSnapshotsTermsAndReservesLastImportedUnit(t *testing.T) {
	db := shopTestDB(t)
	require.NoError(t, db.Create(&User{Id: 3, Username: "second-buyer", AffCode: "second-buyer", TenantID: PlatformTenantID, Role: common.RoleCommonUser, Status: common.UserStatusEnabled}).Error)
	retention := ShopRetentionPolicy{SecretAfterDeliverySeconds: 1000, BuyerInputAfterClosureSeconds: 1000, EvidenceAfterSettlementSeconds: 1000, AuditAfterSettlementSeconds: 1000, IdempotencyAfterClosureSeconds: 1000}
	retentionJSON, err := common.Marshal(retention)
	require.NoError(t, err)
	reminderJSON, err := common.Marshal(ShopReminderPolicy{PaymentUnknownSeconds: 60, DeliveryExceptionSeconds: 60, RefundVerificationSeconds: 60})
	require.NoError(t, err)
	_, err = UpdateShopSettings(db, shopTestCommand("settings-create", 0, 100), ShopSettings{SalesEnabled: true, Provider: "epay", ProviderAccountRef: "merchant", Environment: "test", Currency: "CNY", CurrencyExponent: 2, MinAmountMinor: 1, MaxAmountMinor: 10000, ReservationSeconds: 300, MaxUnpaidOrders: 2, PolicyVersion: 1, RetentionPolicyJSON: string(retentionJSON), ReminderPolicyJSON: string(reminderJSON)}, "Open test shop")
	require.NoError(t, err)
	terms := ShopProductTerms{
		TemplateVersion:   1,
		DeliveryFields:    []ShopField{{Key: "account", Label: "Account", Required: true}, {Key: "password", Label: "Password", Required: true}},
		UsageConditions:   "Personal use",
		DeliveryPolicy:    ShopDeliveryPolicy{Mode: "automatic", PublicTerms: "Delivered after confirmed payment"},
		ViewPolicy:        ShopViewPolicy{DurationSeconds: 600, FirstStart: "delivered_at", ReplacementStart: "delivered_at", ExpiredAction: "deny_reveal", PublicTerms: "View within ten minutes"},
		WarrantyPolicy:    ShopPeriodPolicy{Mode: "none", PublicTerms: "No continuing warranty"},
		EntitlementPolicy: ShopPeriodPolicy{Mode: "described", PublicTerms: "The supplied account"},
		RefundPolicy:      ShopRefundPolicy{GoodsRefundMode: "full_only", Execution: "manual_original_channel", PublicTerms: "Full refund after review"},
	}
	termsJSON, err := common.Marshal(terms)
	require.NoError(t, err)
	product, err := SaveShopProduct(db, shopTestCommand("product-create", 0, 101), "", ShopProduct{Name: "Virtual account", Specification: "One account", ProductKind: "account", DeliveryType: "automatic", PriceMinor: 125, Currency: "CNY", CurrencyExponent: 2, TermsJSON: string(termsJSON), InventoryIdentityDomain: "account-provider", IdentityNormalizationVersion: 1})
	require.NoError(t, err)
	product, err = SetShopProductStatus(db, shopTestCommand("product-publish", product.Version, 102), product.PublicID, "published", "")
	require.NoError(t, err)
	imported, err := ImportShopInventory(db, ShopImportInput{Command: shopTestCommand("inventory-import", product.Version, 103), ProductPublicID: product.PublicID, ProductVersion: product.Version, TemplateVersion: terms.TemplateVersion, Rows: []ShopStockImportRow{{PublicID: "stock-last", Secret: ShopSecret{Ciphertext: "encrypted-account", KeyVersion: "v1"}, DomainHash: strings.Repeat("a", 64), Fingerprint: strings.Repeat("b", 64), FingerprintKeyVersion: "fp1", NormalizationVersion: 1, MaskedLabel: "a***"}}})
	require.NoError(t, err)
	assert.Equal(t, 1, imported.InsertedCount)
	command := shopTestCommand("buyer-create-order", 0, 200)
	command.ActorID, command.Admin = 1, false
	input := ShopCreateOrderInput{Command: command, ProductPublicID: product.PublicID, ProductVersion: product.Version, TermsVersion: product.TermsVersion, AcceptedTerms: true}
	order, err := CreateShopOrder(db, input)
	require.NoError(t, err)
	assert.Equal(t, "open", order.PurchaseState)
	assert.Equal(t, "unpaid", order.PaymentState)
	assert.Equal(t, 1, order.Quantity)
	assert.Equal(t, int64(125), order.AmountMinor)
	assert.Equal(t, int64(500), order.ReservationExpiresAt)
	assert.Equal(t, input.TermsVersion, order.TermsVersion)
	assert.Equal(t, input.ProductVersion, order.ProductVersion)
	assert.Equal(t, int64(200), order.TermsAcceptedAt)
	snapshot, err := shopSnapshot(order)
	require.NoError(t, err)
	assert.Equal(t, "Virtual account", snapshot.Name)
	assert.Equal(t, terms, snapshot.Terms)
	assert.Equal(t, retention, snapshot.Retention)
	require.NotNil(t, order.ReservedStockID)
	var stock ShopStockUnit
	require.NoError(t, db.First(&stock, *order.ReservedStockID).Error)
	assert.Equal(t, "reserved", stock.State)
	require.NotNil(t, stock.ReservedOrderID)
	assert.Equal(t, order.ID, *stock.ReservedOrderID)
	assert.Equal(t, order.ReservationExpiresAt, stock.ReservationExpiresAt)
	reservedVersion := stock.Version
	replayed, err := CreateShopOrder(db, input)
	require.NoError(t, err)
	assert.Equal(t, order.ID, replayed.ID)
	assert.Equal(t, order.SnapshotJSON, replayed.SnapshotJSON)
	require.NoError(t, db.First(&stock, stock.ID).Error)
	assert.Equal(t, reservedVersion, stock.Version)
	updatedTerms := terms
	updatedTerms.UsageConditions = "Updated usage terms"
	updatedJSON, err := common.Marshal(updatedTerms)
	require.NoError(t, err)
	changed := *product
	changed.TermsJSON, changed.Name, changed.PriceMinor = string(updatedJSON), "Renamed account", 250
	product, err = SaveShopProduct(db, shopTestCommand("product-change-terms", product.Version, 201), product.PublicID, changed)
	require.NoError(t, err)
	assert.Equal(t, input.TermsVersion+1, product.TermsVersion)
	second := input
	second.Command.Key, second.Command.ActorID, second.Command.Now = "second-create-order", 3, 202
	second.ProductVersion = product.Version
	_, err = CreateShopOrder(db, second)
	assert.ErrorIs(t, err, ErrShopProductChanged)
	second.TermsVersion = product.TermsVersion
	_, err = CreateShopOrder(db, second)
	assert.ErrorIs(t, err, ErrShopStock)
	require.NoError(t, db.First(order, order.ID).Error)
	assert.Equal(t, replayed.SnapshotJSON, order.SnapshotJSON)
	assert.Equal(t, int64(125), order.AmountMinor)
	var count int64
	require.NoError(t, db.Model(&ShopOrder{}).Count(&count).Error)
	assert.Equal(t, int64(1), count)
	require.NoError(t, db.Model(&ShopStockUnit{}).Count(&count).Error)
	assert.Equal(t, int64(1), count)
	require.NoError(t, db.First(&stock, stock.ID).Error)
	assert.Equal(t, reservedVersion, stock.Version)
	assert.Equal(t, order.ID, *stock.ReservedOrderID)
}
