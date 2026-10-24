package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckSelectableUsesRealCandidatesAndQuote(t *testing.T) {
	db, input := reconcileFixture(t)
	require.NoError(t, db.AutoMigrate(&model.Option{}))
	require.NoError(t, db.Create(&model.Option{Key: "InvoicePolicy", Value: `{"enabled":true,"policy_version":"checker-v1"}`}).Error)
	t.Setenv("KKAI_INVOICE_POLICY", `{"enabled":false}`)
	t.Setenv("KKAI_INVOICE_KEY_ID", "checker-test")
	t.Setenv("KKAI_INVOICE_KEYS_FILE", "")
	t.Setenv("KKAI_INVOICE_KEYS", `{"checker-test":"`+base64.StdEncoding.EncodeToString(make([]byte, 32))+`"}`)
	directory, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, os.Chmod(directory, 0700))
	t.Setenv("KKAI_INVOICE_STORAGE_DIR", directory)
	common.OptionMapRWMutex.Lock()
	previous := common.OptionMap
	common.OptionMap = map[string]string{"InvoicePolicy": `{"enabled":false}`}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previous
		common.OptionMapRWMutex.Unlock()
	})
	ctx := context.Background()
	var output bytes.Buffer
	require.ErrorContains(t, checkSelectable(ctx, db, input, &output), "not selectable")
	assert.Empty(t, output.String())
	for _, proof := range input.Orders {
		require.NoError(t, model.ReconcileInvoiceTopUpPayment(ctx, db, proof.TenantID, proof.UserID, proof.OrderNo, model.TopUpPaymentReceipt{ProviderReceiptID: proof.ProviderReceiptID, AmountMinor: proof.AmountMinor, Currency: proof.Currency}, proof.PaidAt))
	}
	require.NoError(t, checkSelectable(ctx, db, input, &output), "the database policy overrides a disabled environment policy")
	var result struct {
		Orders []struct {
			UserID         int      `json:"user_id"`
			OrderNo        string   `json:"order_no"`
			AmountMinor    int64    `json:"amount_minor"`
			Eligible       bool     `json:"eligible"`
			AllowedActions []string `json:"allowed_actions"`
		} `json:"orders"`
		TotalMinor int64 `json:"total_minor"`
	}
	require.NoError(t, common.Unmarshal(output.Bytes(), &result))
	require.Len(t, result.Orders, 6)
	assert.EqualValues(t, 26800, result.TotalMinor)
	for i, order := range result.Orders {
		assert.Equal(t, input.Orders[i].OrderNo, order.OrderNo)
		assert.Equal(t, 140, order.UserID)
		assert.Equal(t, input.Orders[i].AmountMinor, order.AmountMinor)
		assert.True(t, order.Eligible)
		assert.Contains(t, order.AllowedActions, "select")
		assert.NotContains(t, output.String(), input.Orders[i].ProviderReceiptID)
	}
	common.OptionMapRWMutex.RLock()
	assert.Equal(t, `{"enabled":false}`, common.OptionMap["InvoicePolicy"], "checking restores the caller's policy")
	common.OptionMapRWMutex.RUnlock()
	require.NoError(t, db.Where(&model.Option{Key: "InvoicePolicy"}).Delete(&model.Option{}).Error)
	t.Setenv("KKAI_INVOICE_POLICY", `{"enabled":true,"policy_version":"environment-v1"}`)
	output.Reset()
	require.NoError(t, checkSelectable(ctx, db, input, &output), "a missing database policy falls back to the actual environment")
	require.NoError(t, common.Unmarshal(output.Bytes(), &result))
	assert.Len(t, result.Orders, 6)
	assert.EqualValues(t, 26800, result.TotalMinor)
	t.Setenv("KKAI_INVOICE_POLICY", `{"enabled":false}`)
	output.Reset()
	require.ErrorContains(t, checkSelectable(ctx, db, input, &output), "invoice policy unavailable")
	assert.Empty(t, output.String())
	t.Setenv("KKAI_INVOICE_POLICY", `{"enabled":true,"policy_version":"environment-v1"}`)
	var requestCount int64
	require.NoError(t, db.Model(&model.InvoiceRequest{}).Count(&requestCount).Error)
	assert.Zero(t, requestCount, "checking does not create invoice requests")
	require.NoError(t, db.Model(&model.InvoicePaymentReceipt{}).Where("top_up_id = (SELECT id FROM top_ups WHERE trade_no = ?)", input.Orders[0].OrderNo).Update("status", "exception").Error)
	output.Reset()
	require.ErrorContains(t, checkSelectable(ctx, db, input, &output), "not selectable")
	assert.Empty(t, output.String(), "a stored receipt alone cannot satisfy the real candidate contract")
}
