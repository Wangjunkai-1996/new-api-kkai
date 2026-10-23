package controller

import (
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWalletCreemReadinessDoesNotDisableLegacyOrCallbacks(t *testing.T) {
	for _, schema := range []string{"canonical17", "canonical18"} {
		t.Run(schema, func(t *testing.T) {
			if schema == "canonical17" {
				walletLegacyDB(t)
			} else {
				walletCallbackDB(t)
			}
			enableWalletLegacyPayments(t)
			oldWebhook := setting.CreemWebhookSecret
			t.Cleanup(func() { setting.CreemWebhookSecret = oldWebhook })
			for _, input := range []struct {
				account string
				webhook string
				ready   bool
			}{
				{"", "webhook-secret", false},
				{"   ", "webhook-secret", false},
				{"merchant-original", "", false},
				{"merchant-original", "   ", false},
				{" merchant-original ", " webhook-secret ", true},
			} {
				t.Setenv("WALLET_CREEM_ACCOUNT_REF", input.account)
				setting.CreemWebhookSecret = input.webhook
				assert.True(t, isCreemTopUpEnabled(), "legacy configuration must keep its existing purchase behavior")
				response := invokeWalletCompatibility(t, GetTopUpInfo, "")
				require.Equal(t, http.StatusOK, response.Code)
				if schema == "canonical17" || input.ready {
					assert.Contains(t, response.Body.String(), `"enable_creem_topup":true`)
				} else {
					assert.Contains(t, response.Body.String(), `"enable_creem_topup":false`)
				}
			}
			setting.CreemWebhookSecret = "existing-webhook-secret"
			t.Setenv("WALLET_CREEM_ACCOUNT_REF", "")
			assert.True(t, isCreemWebhookEnabled(), "purchase readiness cannot disable receiving earlier payments")
		})
	}
}

func TestWalletCreemOnlyChannelRequiresAccountAndWebhookBeforeCreation(t *testing.T) {
	enableWalletLegacyPayments(t)
	for target, value := range map[*string]string{
		&setting.StripeApiSecret: "", &setting.WaffoPancakeMerchantID: "",
	} {
		original := *target
		*target = value
		t.Cleanup(func() { *target = original })
	}
	// Disable all remaining purchase methods while preserving configured Creem.
	oldWaffo := setting.WaffoEnabled
	setting.WaffoEnabled = false
	t.Cleanup(func() { setting.WaffoEnabled = oldWaffo })
	// Epay configuration is owned by the shared fixture and restored afterwards.
	oldMethods := operation_setting.PayMethods
	operation_setting.PayMethods = nil
	t.Cleanup(func() { operation_setting.PayMethods = oldMethods })
	t.Setenv("WALLET_PAYMENT_SECRET", strings.Repeat("c", 32))
	oldWebhook := setting.CreemWebhookSecret
	t.Cleanup(func() { setting.CreemWebhookSecret = oldWebhook })
	for _, input := range []struct {
		account string
		webhook string
		ready   bool
	}{
		{"", "webhook-secret", false},
		{"   ", "webhook-secret", false},
		{"merchant-original", "", false},
		{"merchant-original", "   ", false},
		{"merchant-original", "webhook-secret", true},
	} {
		t.Setenv("WALLET_CREEM_ACCOUNT_REF", input.account)
		setting.CreemWebhookSecret = input.webhook
		state := walletStateDTO(&model.WalletTopUpState{})
		if !input.ready {
			assert.False(t, state.CanCreate)
			require.NotNil(t, state.CreateBlockReasonCode)
			assert.Equal(t, "CHANNEL_UNAVAILABLE", *state.CreateBlockReasonCode)
		} else {
			assert.True(t, state.CanCreate)
		}
	}
}
