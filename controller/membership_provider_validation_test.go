package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stripe/stripe-go/v81"
)

func updateMembershipTerms(t *testing.T, order *model.MembershipOrder, terms model.MembershipOrderTerms) {
	t.Helper()
	payload, err := common.Marshal(terms)
	require.NoError(t, err)
	require.NoError(t, model.DB.Model(order).Update("terms", string(payload)).Error)
}

func TestMembershipStripeCallbackRejectsFrozenMetadataMismatch(t *testing.T) {
	db, order := membershipControllerFixture(t, model.PaymentProviderStripe, "USD")
	require.NoError(t, model.DB.First(&model.MembershipPricingRelease{}, order.ReleaseID).Error)
	catalog, err := model.GetMembershipCatalog()
	require.NoError(t, err)
	updateMembershipTerms(t, order, model.MembershipOrderTerms{
		Tier: catalog.Tiers[0], ReleaseID: catalog.ReleaseID, DurationSeconds: model.MembershipDurationSeconds,
		Action: "purchase", VerificationVersion: 1, ProviderEnvironment: "test",
	})
	event := stripe.Event{Type: stripe.EventTypeCheckoutSessionCompleted, Data: &stripe.EventData{Object: map[string]any{
		"client_reference_id": order.TradeNo, "payment_status": "paid", "mode": "payment", "currency": "usd",
		"amount_total": "9900", "payment_intent": "pi-mismatch", "livemode": "false",
		"metadata": map[string]any{"membership_tier_id": "wrong", "membership_release_id": "1"},
	}}}
	require.NoError(t, completeMembershipStripe(event))
	require.NoError(t, db.First(order, order.ID).Error)
	assert.Equal(t, model.MembershipOrderException, order.Status)
	assert.Contains(t, order.ExceptionReason, "metadata mismatch")
}

func TestMembershipCreemCallbackRejectsFrozenProductMismatch(t *testing.T) {
	db, order := membershipControllerFixture(t, model.PaymentProviderCreem, "USD")
	catalog, err := model.GetMembershipCatalog()
	require.NoError(t, err)
	catalog.Tiers[0].PaymentProducts = map[string]string{model.PaymentProviderCreem: "creem_product_expected"}
	updateMembershipTerms(t, order, model.MembershipOrderTerms{
		Tier: catalog.Tiers[0], ReleaseID: catalog.ReleaseID, DurationSeconds: model.MembershipDurationSeconds,
		Action: "purchase", VerificationVersion: 1, ProviderEnvironment: "prod", ProviderProductID: "creem_product_expected",
	})
	event := &CreemWebhookEvent{}
	event.Object.RequestId = order.TradeNo
	event.Object.Order.Id = "creem-order-1"
	event.Object.Order.Product = "creem_product_wrong"
	event.Object.Order.AmountPaid = 9900
	event.Object.Order.Currency = "USD"
	event.Object.Order.Status = "paid"
	event.Object.Order.Type = "onetime"
	event.Object.Order.Mode = "prod"
	event.Object.Product.Id = "creem_product_wrong"
	event.Object.Product.BillingType = "onetime"
	event.Object.Mode = "prod"
	event.Object.Units = 1
	require.NoError(t, completeMembershipCreem(event))
	require.NoError(t, db.First(order, order.ID).Error)
	assert.Equal(t, model.MembershipOrderException, order.Status)
	assert.Contains(t, order.ExceptionReason, "product mismatch")
}

func TestMembershipPancakeCallbackRequiresFrozenMetadata(t *testing.T) {
	db, order := membershipControllerFixture(t, model.PaymentProviderWaffoPancake, "USD")
	catalog, err := model.GetMembershipCatalog()
	require.NoError(t, err)
	oldStore := setting.WaffoPancakeStoreID
	setting.WaffoPancakeStoreID = "store_expected"
	t.Cleanup(func() { setting.WaffoPancakeStoreID = oldStore })
	updateMembershipTerms(t, order, model.MembershipOrderTerms{
		Tier: catalog.Tiers[0], ReleaseID: catalog.ReleaseID, DurationSeconds: model.MembershipDurationSeconds,
		Action: "purchase", VerificationVersion: 1, ProviderEnvironment: "prod", ProviderProductID: "pancake_product_expected", ProviderStoreID: "store_expected",
	})
	event := &service.WaffoPancakeWebhookEvent{EventType: "order.completed", StoreID: "store_expected", Mode: "prod", Data: service.WaffoPancakeWebhookData{
		OrderMerchantExternalID: order.TradeNo, OrderID: "pancake-order-1", Currency: "USD", Amount: "99.00",
		MerchantProvidedBuyerIdentity: service.WaffoPancakeBuyerIdentityFromUserID(order.UserID),
		OrderMetadata:                 map[string]string{"membership_product_id": "pancake_product_wrong", "membership_tier_id": order.TierID, "membership_release_id": "1"},
	}}
	require.NoError(t, completeMembershipPancake(event))
	require.NoError(t, db.First(order, order.ID).Error)
	assert.Equal(t, model.MembershipOrderException, order.Status)
	assert.Contains(t, order.ExceptionReason, "metadata mismatch")
}
