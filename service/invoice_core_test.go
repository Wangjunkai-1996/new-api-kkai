package service

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func invoiceFixture(t *testing.T) (InvoiceService, InvoiceScope) {
	t.Helper()
	t.Setenv("KKAI_INVOICE_KEY_ID", "test-key")
	t.Setenv("KKAI_INVOICE_KEYS", `{"test-key":"`+base64.StdEncoding.EncodeToString(make([]byte, 32))+`"}`)
	t.Setenv("KKAI_INVOICE_S3_PRIVATE", "true")
	t.Setenv("KKAI_INVOICE_S3_ENDPOINT", "https://storage.example.com")
	t.Setenv("KKAI_INVOICE_S3_BUCKET", "private-invoices")
	t.Setenv("KKAI_INVOICE_S3_REGION", "auto")
	t.Setenv("KKAI_INVOICE_S3_ACCESS_KEY_ID", "test")
	t.Setenv("KKAI_INVOICE_S3_SECRET_ACCESS_KEY", "test-secret")
	common.OptionMapRWMutex.Lock()
	prior := common.OptionMap["InvoicePolicy"]
	if common.OptionMap == nil {
		common.OptionMap = map[string]string{}
	}
	common.OptionMap["InvoicePolicy"] = `{"enabled":true,"policy_version":"test-v1","seller_id":"seller","seller_name":"Example Seller","tax_group":"service","invoice_types":[{"id":"ordinary","label":"Ordinary","required_fields":[]}],"currencies":[{"currency":"CNY","exponent":2}],"sources":["topup","membership"],"providers":["epay"],"timezone":"Asia/Shanghai"}`
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap["InvoicePolicy"] = prior
		common.OptionMapRWMutex.Unlock()
	})
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:invoice-%d?mode=memory&cache=shared", time.Now().UnixNano())), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	require.NoError(t, db.AutoMigrate(&model.TopUp{}, &model.MembershipOrder{}, &model.MembershipPaymentReceipt{}, &model.SubscriptionOrder{}, &model.InvoiceRequest{}, &model.InvoiceRequestItem{}, &model.InvoiceOrderClaim{}, &model.InvoiceAttachment{}, &model.InvoiceDelivery{}, &model.InvoiceDispute{}, &model.InvoiceEvent{}, &model.InvoiceEventRead{}, &model.InvoiceOperation{}, &model.InvoiceProfile{}, &model.InvoicePaymentReceipt{}, &model.KKAIOutboxEvent{}))
	require.NoError(t, db.Exec("CREATE TABLE users (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, username TEXT, email TEXT, deleted_at DATETIME)").Error)
	require.NoError(t, db.Exec("INSERT INTO users (id,tenant_id,username,email) VALUES (1,0,'customer','customer@example.com'), (2,0,'admin','admin@example.com')").Error)
	require.NoError(t, db.Create(&model.TopUp{Id: 1, UserId: 1, TenantID: 0, TradeNo: "payment-a", PaidMinor: 50000, Currency: "CNY", PaymentProvider: "epay", Status: common.TopUpStatusSuccess, CompleteTime: time.Now().Unix() - 100}).Error)
	require.NoError(t, db.Create(&model.InvoicePaymentReceipt{TopUpID: 1, UserID: 1, Provider: "epay", ReceiptKey: strings.Repeat("a", 64), AmountMinor: 50000, Currency: "CNY", Status: "verified", PaidAt: time.Now().Unix() - 100, CreatedAt: time.Now().Unix() - 100}).Error)
	return InvoiceService{DB: db}, InvoiceScope{TenantID: 0, UserID: 1}
}
func invoiceCreateInput() InvoiceCreateInput {
	return InvoiceCreateInput{InvoiceQuoteInput: InvoiceQuoteInput{Items: []InvoiceOrderRef{{SourceType: "topup", OrderID: "payment-a"}}, InvoiceType: "ordinary", PolicyVersion: "test-v1"}, Profile: &InvoiceProfileData{Kind: "company", Title: "Example Company", TaxID: "example-tax-id", Email: "billing@example.com"}, UserConfirmed: true}
}
func invoiceRequireCode(t *testing.T, err error, code string) {
	t.Helper()
	var business *InvoiceError
	require.ErrorAs(t, err, &business)
	assert.Equal(t, code, business.Code)
}

func TestInvoiceClaimIdempotencyAndSafeRejection(t *testing.T) {
	s, user := invoiceFixture(t)
	ctx := context.Background()
	require.True(t, s.Policy().Enabled, "policy must be configured for the test")
	created, err := s.Create(ctx, user, "create-key-one", invoiceCreateInput())
	require.NoError(t, err)
	assert.Equal(t, "50000", created.AmountMinor)
	assert.Equal(t, model.InvoicePendingReview, created.Status)
	replay, err := s.Create(ctx, user, "create-key-one", invoiceCreateInput())
	require.NoError(t, err)
	assert.Equal(t, created.PublicID, replay.PublicID)
	_, err = s.Create(ctx, user, "create-key-two", invoiceCreateInput())
	invoiceRequireCode(t, err, "PAYMENT_CHANGED")
	var row model.InvoiceRequest
	require.NoError(t, s.DB.First(&row).Error)
	assert.NotContains(t, row.ProfileCipher, "billing@example.com")
	assert.NotContains(t, row.ProfileCipher, "Example Company")
	_, err = s.Get(ctx, InvoiceScope{TenantID: 0, UserID: 9}, created.PublicID)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	admin := InvoiceScope{TenantID: 0, UserID: 2, Admin: true}
	rejected, err := s.Mutate(ctx, admin, created.PublicID, "reject", "reject-key", InvoiceActionInput{Version: created.Version, Reason: "Correct the invoice title", NotIssuedConfirmed: true})
	require.NoError(t, err)
	assert.Equal(t, model.InvoiceRejected, rejected.Status)
	var claimCount int64
	require.NoError(t, s.DB.Model(&model.InvoiceOrderClaim{}).Count(&claimCount).Error)
	assert.Zero(t, claimCount)
	_, err = s.Create(ctx, user, "create-key-three", invoiceCreateInput())
	require.NoError(t, err)
}
func TestInvoiceDeliveryReplyReissueAndHistoricalAccess(t *testing.T) {
	s, user := invoiceFixture(t)
	ctx := context.Background()
	admin := InvoiceScope{TenantID: 0, UserID: 2, Admin: true}
	r, err := s.Create(ctx, user, "flow-create", invoiceCreateInput())
	require.NoError(t, err)
	r, err = s.Mutate(ctx, admin, r.PublicID, "approve", "flow-approve", InvoiceActionInput{Version: r.Version})
	require.NoError(t, err)
	assert.Equal(t, model.InvoiceApprovedForIssue, r.Status)
	assert.Nil(t, r.CurrentDelivery)
	addFile := func(id string) {
		require.NoError(t, s.DB.Create(&model.InvoiceAttachment{PublicID: id, TenantID: 0, RequestID: 1, UploaderID: 2, Purpose: "invoice", ObjectKey: id, FileName: "invoice.pdf", MIMEType: "application/pdf", Size: 100, SHA256: strings.Repeat("a", 64), Status: "validated", UploadKey: id, ExpiresAt: time.Now().Unix() + 3600, CreatedAt: time.Now().Unix()}).Error)
	}
	addFile("file-one")
	deliveryInput := InvoiceActionInput{Version: r.Version, AttachmentID: "file-one", InvoiceNo: "invoice-number", IssuedOn: time.Now().Add(-time.Hour).Format("2006-01-02"), AmountMinor: "50000", Currency: "CNY"}
	r, err = s.Mutate(ctx, admin, r.PublicID, "deliver", "flow-deliver", deliveryInput)
	require.NoError(t, err)
	require.NotNil(t, r.CurrentDelivery)
	assert.EqualValues(t, 1, r.CurrentDeliveryVersion)
	r, err = s.Mutate(ctx, user, r.PublicID, "dispute", "flow-dispute", InvoiceActionInput{Version: r.Version, DeliveryVersion: 1, Category: "title_tax", Description: "Please correct the title"})
	require.NoError(t, err)
	require.NotNil(t, r.ActiveDispute)
	r, err = s.Mutate(ctx, admin, r.PublicID, "reply", "flow-reply", InvoiceActionInput{Version: r.Version, DisputeID: r.ActiveDispute.ID, Message: "Please verify the existing invoice"})
	require.NoError(t, err)
	assert.Equal(t, model.InvoiceAwaitingReconfirmation, r.Status)
	assert.EqualValues(t, 1, r.CurrentDeliveryVersion)
	r, err = s.Mutate(ctx, user, r.PublicID, "confirm", "flow-confirm", InvoiceActionInput{Version: r.Version, DeliveryVersion: 1})
	require.NoError(t, err)
	assert.Equal(t, model.InvoiceConfirmed, r.Status)
	r, err = s.Mutate(ctx, user, r.PublicID, "dispute", "flow-second-dispute", InvoiceActionInput{Version: r.Version, DeliveryVersion: 1, Category: "other", Description: "There is a further problem"})
	require.NoError(t, err)
	addFile("file-two")
	deliveryInput.Version = r.Version
	deliveryInput.AttachmentID = "file-two"
	deliveryInput.DisputeID = r.ActiveDispute.ID
	r, err = s.Mutate(ctx, admin, r.PublicID, "reissue", "flow-reissue", deliveryInput)
	require.NoError(t, err)
	assert.EqualValues(t, 2, r.CurrentDeliveryVersion)
	assert.Len(t, r.DeliveredVersions, 2)
	assert.Equal(t, model.InvoiceDelivered, r.Status)
	_, err = s.Mutate(ctx, user, r.PublicID, "confirm", "flow-old-confirm", InvoiceActionInput{Version: r.Version, DeliveryVersion: 1})
	invoiceRequireCode(t, err, "DELIVERY_VERSION_CHANGED")
	r, err = s.Mutate(ctx, admin, r.PublicID, "hold", "flow-hold", InvoiceActionInput{Version: r.Version, Reason: "Finance verification"})
	require.NoError(t, err)
	assert.Equal(t, model.InvoiceFinanceHold, r.Status)
	require.NotNil(t, r.CurrentDelivery)
	_, err = s.Mutate(ctx, admin, r.PublicID, "reject", "flow-illegal-reject", InvoiceActionInput{Version: r.Version, Reason: "Not allowed", NotIssuedConfirmed: true})
	invoiceRequireCode(t, err, "INVALID_STATE")
}
func TestInvoiceMembershipReceiptsLegacyAliasesAndMissingAmount(t *testing.T) {
	s, user := invoiceFixture(t)
	ctx := context.Background()
	key := "epay:receipt"
	order := model.MembershipOrder{ID: 10, TradeNo: "member-paid", UserID: 1, AmountMinor: 69900, Currency: "CNY", Provider: "epay", Status: model.MembershipOrderSuccess, ReceiptKey: &key, ReceiptID: "receipt", ActualAmountMinor: 69900, ActualCurrency: "CNY", ActualProvider: "epay", ActualOneTime: true, PaidAt: time.Now().Unix() - 100}
	require.NoError(t, s.DB.Create(&order).Error)
	require.NoError(t, s.DB.Create(&model.MembershipPaymentReceipt{OrderID: 10, ReceiptKey: key, ReceiptID: "receipt", Provider: "epay", AmountMinor: 69900, Currency: "CNY", OneTime: true, Status: model.MembershipOrderSuccess}).Error)
	require.NoError(t, s.DB.Create(&model.TopUp{Id: 2, UserId: 1, TradeNo: "member-paid", PaidMinor: 69900, Currency: "CNY", PaymentProvider: "epay", Status: common.TopUpStatusSuccess, CompleteTime: time.Now().Unix() - 100}).Error)
	require.NoError(t, s.DB.Create(&model.TopUp{Id: 3, UserId: 1, TradeNo: "unknown-money", Money: 999, PaymentProvider: "stripe", Status: common.TopUpStatusSuccess, CompleteTime: time.Now().Unix() - 100}).Error)
	eligible, err := s.Candidates(ctx, user, InvoiceFilter{Eligibility: "eligible"})
	require.NoError(t, err)
	assert.EqualValues(t, 2, eligible.Total)
	forged := invoiceCreateInput()
	forged.Items = []InvoiceOrderRef{{SourceType: "topup", OrderID: "member-paid"}}
	_, err = s.Create(ctx, user, "forged-membership-mirror", forged)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound, "a membership payment's TopUp mirror must never be separately invoiced")
	unavailable, err := s.Candidates(ctx, user, InvoiceFilter{Eligibility: "unavailable"})
	require.NoError(t, err)
	require.Len(t, unavailable.Items, 1)
	assert.Nil(t, unavailable.Items[0].AmountMinor)
	require.NoError(t, s.DB.Model(&model.MembershipPaymentReceipt{}).Where("order_id = ?", 10).Update("status", model.MembershipOrderException).Error)
	eligible, err = s.Candidates(ctx, user, InvoiceFilter{Eligibility: "eligible"})
	require.NoError(t, err)
	assert.EqualValues(t, 1, eligible.Total)
}

func TestInvoiceTopUpRequiresVerifiedReceipt(t *testing.T) {
	s, user := invoiceFixture(t)
	ctx := context.Background()
	for _, tradeNo := range []string{"historical-paid", "manual-completed"} {
		require.NoError(t, s.DB.Create(&model.TopUp{UserId: 1, TradeNo: tradeNo, PaidMinor: 50000, Currency: "CNY", PaymentProvider: "epay", Status: common.TopUpStatusSuccess, CompleteTime: time.Now().Unix() - 100}).Error)
	}
	eligible, err := s.Candidates(ctx, user, InvoiceFilter{Eligibility: "eligible"})
	require.NoError(t, err)
	assert.EqualValues(t, 1, eligible.Total, "checkout snapshots or manual status alone are never payment evidence")
	unavailable, err := s.Candidates(ctx, user, InvoiceFilter{Eligibility: "unavailable"})
	require.NoError(t, err)
	require.Len(t, unavailable.Items, 2)
	for _, order := range unavailable.Items {
		assert.Nil(t, order.AmountMinor)
		assert.Contains(t, order.ReasonCodes, "PAYMENT_UNVERIFIED")
	}
	require.NoError(t, s.DB.Model(&model.InvoicePaymentReceipt{}).Where("top_up_id = ?", 1).Update("status", "exception").Error)
	quote, err := s.Quote(ctx, user, invoiceCreateInput().InvoiceQuoteInput)
	require.NoError(t, err)
	assert.False(t, quote.Eligible)
	assert.Equal(t, "0", quote.AmountMinor)
}

func TestInvoiceReceiptClaimIsUniqueAcrossSourcesAndOwners(t *testing.T) {
	for _, tc := range []struct {
		name       string
		memberUser int
		first      string
	}{
		{"same owner topup first", 1, "topup"},
		{"same owner membership first", 1, "membership"},
		{"different owner topup first", 2, "topup"},
		{"different owner membership first", 2, "membership"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, user := invoiceFixture(t)
			ctx := context.Background()
			key := "epay:cross-source-receipt"
			require.NoError(t, s.DB.Model(&model.InvoicePaymentReceipt{}).Where("top_up_id = ?", 1).Update("receipt_key", invoiceHash(key)).Error)
			order := model.MembershipOrder{ID: 10, TradeNo: "member-cross-source", UserID: tc.memberUser, AmountMinor: 50000, Currency: "CNY", Provider: "epay", Status: model.MembershipOrderSuccess, ReceiptKey: &key, ReceiptID: "cross-source-receipt", ActualAmountMinor: 50000, ActualCurrency: "CNY", ActualProvider: "epay", ActualOneTime: true, PaidAt: time.Now().Unix() - 100}
			require.NoError(t, s.DB.Create(&order).Error)
			require.NoError(t, s.DB.Create(&model.MembershipPaymentReceipt{OrderID: 10, ReceiptKey: key, ReceiptID: order.ReceiptID, Provider: "epay", AmountMinor: 50000, Currency: "CNY", OneTime: true, Status: model.MembershipOrderSuccess}).Error)
			topupInput := invoiceCreateInput()
			memberInput := invoiceCreateInput()
			memberInput.Items = []InvoiceOrderRef{{SourceType: "membership", OrderID: order.TradeNo}}
			if tc.memberUser == 1 {
				both := topupInput
				both.Items = append([]InvoiceOrderRef{topupInput.Items[0]}, memberInput.Items[0])
				_, err := s.Create(ctx, user, "cross-source-both", both)
				invoiceRequireCode(t, err, "DUPLICATE_PAYMENT")
			}
			firstInput, secondInput := topupInput, memberInput
			firstUser, secondUser := user, InvoiceScope{TenantID: 0, UserID: tc.memberUser}
			if tc.first == "membership" {
				firstInput, secondInput = secondInput, firstInput
				firstUser, secondUser = secondUser, firstUser
			}
			created, err := s.Create(ctx, firstUser, "cross-source-first", firstInput)
			require.NoError(t, err)
			quote, err := s.Quote(ctx, secondUser, secondInput.InvoiceQuoteInput)
			require.NoError(t, err)
			assert.False(t, quote.Eligible)
			require.Len(t, quote.Items, 1)
			assert.Contains(t, quote.Items[0].ReasonCodes, "ORDER_CLAIMED")
			if firstUser.UserID != secondUser.UserID {
				assert.Empty(t, quote.Items[0].ClaimedRequestPublicID, "another owner's invoice identity stays private")
			} else {
				assert.Equal(t, created.PublicID, quote.Items[0].ClaimedRequestPublicID)
			}
			_, err = s.Create(ctx, secondUser, "cross-source-second", secondInput)
			invoiceRequireCode(t, err, "PAYMENT_CHANGED")
			var count int64
			require.NoError(t, s.DB.Model(&model.InvoiceOrderClaim{}).Count(&count).Error)
			assert.EqualValues(t, 1, count)
			require.NoError(t, s.DB.Model(&model.InvoiceRequest{}).Count(&count).Error)
			assert.EqualValues(t, 1, count)
		})
	}
}

func TestInvoicePolicyCannotEnableUnauditedPaymentProvider(t *testing.T) {
	s, _ := invoiceFixture(t)
	common.OptionMapRWMutex.RLock()
	raw := common.OptionMap["InvoicePolicy"]
	common.OptionMapRWMutex.RUnlock()
	for _, providers := range []string{`[]`, `["stripe"]`, `["epay","stripe"]`} {
		common.OptionMapRWMutex.Lock()
		common.OptionMap["InvoicePolicy"] = strings.Replace(raw, `["epay"]`, providers, 1)
		common.OptionMapRWMutex.Unlock()
		policy := s.Policy()
		assert.False(t, policy.Enabled)
		assert.Equal(t, "POLICY_UNCONFIGURED", policy.DisabledReason)
	}
}

func TestInvoiceProfilesReplayPolicyAndNotificationIsolation(t *testing.T) {
	s, user := invoiceFixture(t)
	ctx := context.Background()
	admin := InvoiceScope{TenantID: 0, UserID: 2, Admin: true}
	saved, err := s.SaveProfile(ctx, user, "", "profile-create", InvoiceProfileInput{InvoiceProfileData: *invoiceCreateInput().Profile})
	require.NoError(t, err)
	assert.EqualValues(t, 1, saved.Version)
	list, err := s.Profiles(ctx, user, InvoiceFilter{})
	require.NoError(t, err)
	require.Len(t, list.Items, 1)
	assert.NotEqual(t, saved.TaxID, list.Items[0].TaxID)
	_, err = s.GetProfile(ctx, admin, saved.ID)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	r, err := s.Create(ctx, user, "notification-create", invoiceCreateInput())
	require.NoError(t, err)
	notifications, err := s.Notifications(ctx, admin, InvoiceFilter{})
	require.NoError(t, err)
	assert.EqualValues(t, 1, notifications.UnreadCount)
	require.NoError(t, s.ReadNotifications(ctx, admin, ""))
	notifications, err = s.Notifications(ctx, admin, InvoiceFilter{})
	require.NoError(t, err)
	assert.Zero(t, notifications.UnreadCount)
	_, err = s.Mutate(ctx, admin, r.PublicID, "approve", "notification-approve", InvoiceActionInput{Version: r.Version})
	require.NoError(t, err)
	notifications, err = s.Notifications(ctx, user, InvoiceFilter{})
	require.NoError(t, err)
	assert.EqualValues(t, 1, notifications.UnreadCount)
	require.Len(t, notifications.Items, 1)
	require.NoError(t, s.ReadNotifications(ctx, user, notifications.Items[0].ID))
	t.Setenv("KKAI_INVOICE_S3_PRIVATE", "false")
	replay, err := s.Create(ctx, user, "notification-create", invoiceCreateInput())
	require.NoError(t, err)
	assert.Equal(t, r.PublicID, replay.PublicID)
	require.NoError(t, s.DeleteProfile(ctx, user, saved.ID, "profile-delete", saved.Version))
	_, err = s.GetProfile(ctx, user, saved.ID)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	current, err := s.Get(ctx, user, r.PublicID)
	require.NoError(t, err)
	assert.Equal(t, "Example Company", current.Profile.Title)
}

func TestInvoiceMembershipFinanceResolutionAllowsResume(t *testing.T) {
	s, user := invoiceFixture(t)
	ctx := context.Background()
	admin := InvoiceScope{TenantID: 0, UserID: 2, Admin: true}
	key := "epay:primary"
	require.NoError(t, s.DB.Create(&model.MembershipOrder{ID: 10, TradeNo: "member-paid", UserID: 1, AmountMinor: 69900, Currency: "CNY", Provider: "epay", Status: model.MembershipOrderSuccess, ReceiptKey: &key, ReceiptID: "primary", ActualAmountMinor: 69900, ActualCurrency: "CNY", ActualProvider: "epay", ActualOneTime: true, PaidAt: time.Now().Unix() - 100}).Error)
	require.NoError(t, s.DB.Create(&model.MembershipPaymentReceipt{OrderID: 10, ReceiptKey: key, ReceiptID: "primary", Provider: "epay", AmountMinor: 69900, Currency: "CNY", OneTime: true, Status: model.MembershipOrderSuccess}).Error)
	input := invoiceCreateInput()
	input.Items = []InvoiceOrderRef{{SourceType: "membership", OrderID: "member-paid"}}
	r, err := s.Create(ctx, user, "membership-resolution-create", input)
	require.NoError(t, err)
	require.NoError(t, s.DB.Create(&model.MembershipPaymentReceipt{OrderID: 10, ReceiptKey: "epay:additional", ReceiptID: "additional", Provider: "epay", AmountMinor: 69900, Currency: "CNY", OneTime: true, Status: model.MembershipOrderException, Reason: "additional payment"}).Error)
	require.NoError(t, s.DB.Model(&model.MembershipOrder{}).Where("id = ?", 10).Update("exception_reason", "additional payment").Error)
	_, err = s.Mutate(ctx, admin, r.PublicID, "approve", "membership-resolution-approve", InvoiceActionInput{Version: r.Version})
	invoiceRequireCode(t, err, "PAYMENT_CHANGED")
	for _, step := range []struct {
		name       string
		additional map[string]any
		exception  string
	}{
		{"unresolved", nil, ""},
		{"missing-audit", map[string]any{"status": "resolved"}, ""},
		{"order-anomaly", map[string]any{"resolution": `{"action":"dismiss","actor_id":2,"reason":"Duplicate payment handled","operation_id":"finance-review"}`}, "additional payment"},
	} {
		require.NoError(t, s.DB.Model(&model.MembershipOrder{}).Where("id = ?", 10).Update("exception_reason", step.exception).Error)
		if step.additional != nil {
			require.NoError(t, s.DB.Model(&model.MembershipPaymentReceipt{}).Where("receipt_key = ?", "epay:additional").Updates(step.additional).Error)
		}
		r, err = s.Get(ctx, user, r.PublicID)
		require.NoError(t, err)
		assert.Equal(t, model.InvoiceFinanceHold, r.Status)
		_, err = s.Mutate(ctx, admin, r.PublicID, "resume", "membership-resume-"+step.name, InvoiceActionInput{Version: r.Version, Reason: "Finance review"})
		invoiceRequireCode(t, err, "PAYMENT_CHANGED")
	}
	// ResolveMembershipOrder(dismiss) records the audit on the additional receipt
	// and clears the order anomaly without changing the successful primary receipt.
	require.NoError(t, s.DB.Model(&model.MembershipOrder{}).Where("id = ?", 10).Update("exception_reason", "").Error)
	r, err = s.Get(ctx, user, r.PublicID)
	require.NoError(t, err)
	r, err = s.Mutate(ctx, admin, r.PublicID, "resume", "membership-resume-resolved", InvoiceActionInput{Version: r.Version, Reason: "Finance review completed"})
	require.NoError(t, err)
	assert.Equal(t, model.InvoicePendingReview, r.Status)
	assert.Equal(t, "69900", r.AmountMinor, "resolved additional receipts must not increase the invoice amount")
	r, err = s.Mutate(ctx, admin, r.PublicID, "approve", "membership-approve-resolved", InvoiceActionInput{Version: r.Version})
	require.NoError(t, err)
	assert.Equal(t, model.InvoiceApprovedForIssue, r.Status)
	var claims int64
	require.NoError(t, s.DB.Model(&model.InvoiceOrderClaim{}).Count(&claims).Error)
	assert.EqualValues(t, 1, claims)
}
