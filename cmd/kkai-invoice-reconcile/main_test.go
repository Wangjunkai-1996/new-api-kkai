package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func reconcileFixture(t *testing.T) (*gorm.DB, manifest) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "payments.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, raw.Close()) })
	previousDialect := common.MainDatabaseType()
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	t.Cleanup(func() { common.SetMainDatabaseType(previousDialect) })
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.TopUp{}, &model.InvoicePaymentReceipt{}, &model.WalletPaymentEvent{},
		&model.MembershipOrder{}, &model.SubscriptionOrder{}, &model.InvoiceRequest{}, &model.InvoiceRequestItem{},
		&model.InvoiceOrderClaim{}, &model.InvoiceAttachment{}, &model.InvoiceDelivery{}, &model.InvoiceDispute{},
		&model.InvoiceEvent{}, &model.InvoiceEventRead{}, &model.InvoiceOperation{}, &model.InvoiceProfile{}, &model.KKAIOutboxEvent{}))
	require.NoError(t, db.Create(&model.User{Id: 140, Username: "invoice-customer", Quota: 55}).Error)
	input := manifest{Operator: "finance-operator", EvidenceSource: "merchant_statement", EvidenceSHA256: strings.Repeat("a", 64)}
	for i, cents := range []int64{7275, 7275, 3750, 3750, 3750, 1000} {
		proof := orderProof{UserID: 140, OrderNo: fmt.Sprintf("historical-%d", i), AmountMinor: cents, Currency: "CNY", PaidAt: common.GetTimestamp() - 60, ProviderReceiptID: fmt.Sprintf("provider-transaction-%d", i)}
		input.Orders = append(input.Orders, proof)
		require.NoError(t, db.Create(&model.TopUp{UserId: proof.UserID, TradeNo: proof.OrderNo, PaymentProvider: model.PaymentProviderEpay, Status: common.TopUpStatusSuccess, CreateTime: proof.PaidAt - 60, Amount: 1333, Money: 999}).Error)
	}
	return db, input
}

func TestPreviewAndApplyUseOnlyExactEvidenceWithoutCrediting(t *testing.T) {
	db, input := reconcileFixture(t)
	preview, err := reconcile(context.Background(), db, input, false)
	require.NoError(t, err)
	assert.Equal(t, "preview", preview.Mode)
	require.Len(t, preview.Orders, 6)
	var receipts, events int64
	require.NoError(t, db.Model(&model.InvoicePaymentReceipt{}).Count(&receipts).Error)
	require.NoError(t, db.Model(&model.WalletPaymentEvent{}).Count(&events).Error)
	assert.Zero(t, receipts)
	assert.Zero(t, events)
	var original []model.TopUp
	require.NoError(t, db.Order("id ASC").Find(&original).Error)
	for _, order := range original {
		assert.Zero(t, order.PaidMinor)
		assert.Zero(t, order.CompleteTime)
	}
	for range 2 {
		applied, err := reconcile(context.Background(), db, input, true)
		require.NoError(t, err)
		assert.Equal(t, "apply", applied.Mode)
		for _, order := range applied.Orders {
			assert.Equal(t, "verified", order.Status)
		}
	}
	var stored []model.TopUp
	require.NoError(t, db.Order("id ASC").Find(&stored).Error)
	var total int64
	for i, order := range stored {
		total += order.PaidMinor
		assert.Equal(t, input.Orders[i].AmountMinor, order.PaidMinor)
		assert.Equal(t, "CNY", order.Currency)
		assert.Equal(t, input.Orders[i].PaidAt, order.CompleteTime)
		assert.Equal(t, original[i].Money, order.Money)
		assert.Equal(t, original[i].Amount, order.Amount)
	}
	assert.EqualValues(t, 26800, total, "credited amount and the historical multiplier never determine invoice value")
	require.NoError(t, db.Model(&model.InvoicePaymentReceipt{}).Count(&receipts).Error)
	require.NoError(t, db.Model(&model.WalletPaymentEvent{}).Count(&events).Error)
	assert.EqualValues(t, 6, receipts)
	assert.EqualValues(t, 6, events)
	var audit model.WalletPaymentEvent
	require.NoError(t, db.First(&audit).Error)
	assert.Equal(t, "invoice_reconcile", audit.Kind)
	assert.Equal(t, input.EvidenceSHA256, audit.EvidenceHash)
	assert.Contains(t, audit.EvidenceRef, input.Operator)
	assert.Equal(t, input.EvidenceSource, audit.Source)
	var user model.User
	require.NoError(t, db.First(&user, 140).Error)
	assert.EqualValues(t, 55, user.Quota)
	var outbox int64
	require.NoError(t, db.Model(&model.KKAIOutboxEvent{}).Count(&outbox).Error)
	assert.Zero(t, outbox)
}

func TestInvalidManifestRejectsWholeBatchBeforeWriting(t *testing.T) {
	for _, name := range []string{"wrong owner", "missing provider receipt", "negative amount", "different exact amount", "duplicate receipt"} {
		t.Run(name, func(t *testing.T) {
			db, input := reconcileFixture(t)
			switch name {
			case "wrong owner":
				input.Orders[3].UserID++
			case "missing provider receipt":
				input.Orders[3].ProviderReceiptID = ""
			case "negative amount":
				input.Orders[3].AmountMinor = -1
			case "different exact amount":
				require.NoError(t, db.Model(&model.TopUp{}).Where("trade_no = ?", input.Orders[3].OrderNo).Update("paid_minor", 5000).Error)
			case "duplicate receipt":
				input.Orders[3].ProviderReceiptID = input.Orders[0].ProviderReceiptID
			}
			var before []model.TopUp
			require.NoError(t, db.Order("id ASC").Find(&before).Error)
			_, err := reconcile(context.Background(), db, input, true)
			require.Error(t, err)
			var after []model.TopUp
			require.NoError(t, db.Order("id ASC").Find(&after).Error)
			assert.Equal(t, before, after)
			var receipts, events int64
			require.NoError(t, db.Model(&model.InvoicePaymentReceipt{}).Count(&receipts).Error)
			require.NoError(t, db.Model(&model.WalletPaymentEvent{}).Count(&events).Error)
			assert.Zero(t, receipts)
			assert.Zero(t, events)
		})
	}
}

func TestConflictingReplayCommitsFinancialHoldAndStopsBatch(t *testing.T) {
	db, input := reconcileFixture(t)
	_, err := reconcile(context.Background(), db, input, true)
	require.NoError(t, err)
	var receipt model.InvoicePaymentReceipt
	require.NoError(t, db.Where("user_id = ?", 140).Order("id ASC").First(&receipt).Error)
	request := model.InvoiceRequest{PublicID: "invoice-claimed", UserID: 140, Status: model.InvoiceDelivered, Version: 1, CurrentDeliveryVersion: 1, AmountMinor: receipt.AmountMinor, Currency: "CNY"}
	require.NoError(t, db.Create(&request).Error)
	canonical := sha256.Sum256([]byte("payment:receipt:v1:0:" + receipt.ReceiptKey))
	require.NoError(t, db.Create(&model.InvoiceOrderClaim{CanonicalPaymentKey: hex.EncodeToString(canonical[:]), RequestID: request.ID}).Error)
	input.Orders[0].ProviderReceiptID = "different-provider-transaction"
	result, err := reconcile(context.Background(), db, input, true)
	require.Error(t, err)
	assert.Equal(t, "payment_exception", result.Orders[0].Status)
	assert.Equal(t, "not_applied", result.Orders[1].Status)
	require.NoError(t, db.First(&receipt, receipt.ID).Error)
	assert.Equal(t, "exception", receipt.Status)
	require.NoError(t, db.First(&request, request.ID).Error)
	assert.Equal(t, model.InvoiceFinanceHold, request.Status)
	var count int64
	require.NoError(t, db.Model(&model.WalletPaymentEvent{}).Where("status = ?", "payment_exception").Count(&count).Error)
	assert.EqualValues(t, 1, count)
	_, err = reconcile(context.Background(), db, input, true)
	require.Error(t, err)
	require.NoError(t, db.Model(&model.WalletPaymentEvent{}).Where("status = ?", "payment_exception").Count(&count).Error)
	assert.EqualValues(t, 1, count, "exception replay does not duplicate audit or clear the hold")
}

func TestOriginalEvidenceFileMustExistAndMatchManifestDigest(t *testing.T) {
	for _, name := range []string{"valid", "owner approval", "missing file", "wrong hash", "empty file"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			evidence := filepath.Join(dir, "provider-evidence.txt")
			raw := []byte("original test provider evidence")
			digest := sha256.Sum256(raw)
			input := manifest{Operator: "reviewer", EvidenceSource: "signed_callback", EvidenceSHA256: hex.EncodeToString(digest[:])}
			switch name {
			case "owner approval":
				input.EvidenceSource = "owner_approved_legacy"
			case "wrong hash":
				input.EvidenceSHA256 = strings.Repeat("0", 64)
			case "empty file":
				raw = nil
			}
			if name != "missing file" {
				require.NoError(t, os.WriteFile(evidence, raw, 0600))
			}
			encoded, err := common.Marshal(input)
			require.NoError(t, err)
			manifestPath := filepath.Join(dir, "manifest.json")
			require.NoError(t, os.WriteFile(manifestPath, encoded, 0600))
			loaded, err := loadManifest(manifestPath, evidence)
			if name == "valid" || name == "owner approval" {
				require.NoError(t, err)
				assert.Equal(t, input, loaded)
			} else {
				require.Error(t, err)
				assert.NotContains(t, err.Error(), evidence)
			}
		})
	}
}

func TestConnectionErrorsDoNotExposeDSN(t *testing.T) {
	t.Setenv("KKAI_INVOICE_RECONCILE_DSN", "")
	t.Setenv("SQL_DSN", "")
	dir := t.TempDir()
	raw := []byte("test evidence")
	digest := sha256.Sum256(raw)
	evidence := filepath.Join(dir, "evidence.txt")
	require.NoError(t, os.WriteFile(evidence, raw, 0600))
	encoded, err := common.Marshal(manifest{Operator: "reviewer", EvidenceSource: "merchant_statement", EvidenceSHA256: hex.EncodeToString(digest[:])})
	require.NoError(t, err)
	manifestPath := filepath.Join(dir, "manifest.json")
	require.NoError(t, os.WriteFile(manifestPath, encoded, 0600))
	secret := "not-a-database-password-should-not-appear"
	var output bytes.Buffer
	err = run([]string{"--manifest", manifestPath, "--evidence", evidence, "--dsn", "invalid-dsn-" + secret}, strings.NewReader(""), &output)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), secret)
	assert.NotContains(t, output.String(), secret)
}
