package service

import (
	"context"
	"net/url"
	"testing"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func membershipLegacyCheckout(t *testing.T, db *gorm.DB, order *model.MembershipOrder) map[string]any {
	t.Helper()
	previousAddress, previousID, previousKey := operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey
	operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey = "https://original.example.test/pay", "merchant-original", "merchant-key-original"
	t.Cleanup(func() {
		operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey = previousAddress, previousID, previousKey
	})
	client, err := epay.NewClient(&epay.Config{PartnerID: operation_setting.EpayId, Key: operation_setting.EpayKey}, operation_setting.PayAddress)
	require.NoError(t, err)
	notifyURL, err := url.Parse("https://example.test/api/membership/notify")
	require.NoError(t, err)
	returnURL, err := url.Parse("https://example.test/membership")
	require.NoError(t, err)
	submit, fields, err := client.Purchase(&epay.PurchaseArgs{Type: order.PaymentMethod, ServiceTradeNo: order.TradeNo, Name: "Membership: Gold", Money: "99.00", Device: epay.PC, NotifyUrl: notifyURL, ReturnUrl: returnURL})
	require.NoError(t, err)
	checkout := map[string]any{"submit_url": submit, "form_fields": fields}
	payload, err := common.Marshal(checkout)
	require.NoError(t, err)
	order.CheckoutState, order.CheckoutData = "ready", string(payload)
	require.NoError(t, db.Model(order).Updates(map[string]any{"checkout_state": order.CheckoutState, "checkout_data": order.CheckoutData}).Error)
	require.NoError(t, db.Where("order_id = ?", order.ID).Delete(&model.MembershipPaymentCheck{}).Error)
	return checkout
}

func TestMembershipPaymentLegacyCheckoutOnlyEstablishesOriginalMerchant(t *testing.T) {
	db, order := membershipPaymentRecoveryFixture(t, "https://original.example.test/pay")
	checkout := membershipLegacyCheckout(t, db, order)
	valid := order.CheckoutData
	for _, tc := range []struct {
		name string
		edit func()
	}{
		{"unknown current key", func() { operation_setting.EpayKey = "other-key" }},
		{"unknown current merchant", func() { operation_setting.EpayId = "other-merchant" }},
		{"different gateway", func() { operation_setting.PayAddress = "https://other.example.test/pay" }},
		{"tampered signed amount", func() {
			checkout["form_fields"].(map[string]string)["money"] = "100.00"
			body, err := common.Marshal(checkout)
			require.NoError(t, err)
			order.CheckoutData = string(body)
		}},
		{"unknown signed paid claim", func() {
			fields := checkout["form_fields"].(map[string]string)
			fields["money"], fields["trade_status"] = "99.00", epay.StatusTradeSuccess
			_, err := epay.GenerateParamsWithError(fields, "merchant-key-original")
			require.NoError(t, err)
			body, err := common.Marshal(checkout)
			require.NoError(t, err)
			order.CheckoutData = string(body)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			order.CheckoutData = valid
			operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey = "https://original.example.test/pay", "merchant-original", "merchant-key-original"
			tc.edit()
			_, err := EnsureMembershipEpayRecovery(context.Background(), order)
			require.Error(t, err)
			var count int64
			require.NoError(t, db.Model(&model.MembershipPaymentCheck{}).Count(&count).Error)
			assert.Zero(t, count, "unproven original credentials cannot be frozen")
		})
	}
	order.CheckoutData = valid
	operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey = "https://original.example.test/pay", "merchant-original", "unavailable-original-key"
	imported, review, err := discoverLegacyMembershipEpayPayments(context.Background(), 1)
	require.NoError(t, err)
	assert.Zero(t, imported)
	assert.Equal(t, 1, review)
	require.NoError(t, db.First(order, order.ID).Error)
	assert.Equal(t, model.MembershipLegacyCredentialReview, order.ExceptionReason)
	assert.Equal(t, model.MembershipOrderPending, order.Status)
	assert.Zero(t, order.PaidAt)
	adminReview, err := model.ListMembershipOrders(0, model.MembershipOrderException, 20)
	require.NoError(t, err)
	require.Len(t, adminReview, 1)
	assert.Equal(t, order.ID, adminReview[0].ID)
	operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey = "https://original.example.test/pay", "merchant-original", "merchant-key-original"
	check, err := EnsureMembershipEpayRecovery(context.Background(), order)
	require.NoError(t, err)
	require.NoError(t, db.First(order, order.ID).Error)
	assert.Empty(t, order.ExceptionReason, "proving the original merchant clears only the operational marker")
	assert.Equal(t, model.MembershipPaymentPending, check.Status)
	assert.Equal(t, "merchant-original", check.MerchantID)
	assert.NotContains(t, check.EncryptedSnapshot, "merchant-key-original")
	operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey = "https://new.example.test/pay", "new-merchant", "new-key"
	snapshot, err := MembershipEpayGateway(context.Background(), order)
	require.NoError(t, err)
	assert.Equal(t, "https://original.example.test/pay", snapshot.APIURL)
	assert.Equal(t, "merchant-original", snapshot.MerchantID)
	assert.Equal(t, "merchant-key-original", snapshot.APIKey)
	for _, value := range []any{&model.MembershipPaymentEvidence{}, &model.MembershipPaymentReceipt{}, &model.MembershipAccount{}} {
		var count int64
		require.NoError(t, db.Model(value).Count(&count).Error)
		assert.Zero(t, count, "a valid historical checkout still does not prove payment")
	}
}

func TestMembershipPaymentLegacyDiscoveryAdvancesPastInvalidOldCheckout(t *testing.T) {
	for _, invalid := range []string{"unproven checkout", "deleted owner"} {
		t.Run(invalid, func(t *testing.T) {
			db, first := membershipPaymentRecoveryFixture(t, "https://original.example.test/pay")
			membershipLegacyCheckout(t, db, first)
			second := *first
			second.ID, second.TradeNo, second.UserID = 0, "MEMBER-later", 702
			require.NoError(t, db.Create(&model.User{Id: 702, Username: "later-member"}).Error)
			require.NoError(t, db.Create(&second).Error)
			membershipLegacyCheckout(t, db, &second)
			if invalid == "deleted owner" {
				require.NoError(t, db.Delete(&model.User{}, first.UserID).Error)
			} else {
				require.NoError(t, db.Model(first).Update("checkout_data", `{"submit_url":"https://unproven.example.test/submit.php"}`).Error)
			}
			imported, review, err := discoverLegacyMembershipEpayPayments(context.Background(), 1)
			require.NoError(t, err)
			assert.Equal(t, 0, imported)
			assert.Equal(t, 1, review)
			imported, review, err = discoverLegacyMembershipEpayPayments(context.Background(), 1)
			require.NoError(t, err)
			assert.Equal(t, 1, imported)
			assert.Zero(t, review)
			check, err := model.GetMembershipPaymentCheck(context.Background(), second.ID)
			require.NoError(t, err)
			assert.Equal(t, model.MembershipPaymentPending, check.Status)
			assert.Equal(t, second.TradeNo, check.TradeNo)
		})
	}
}
