package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func invoiceSetTestPolicy(t *testing.T, policy InvoicePolicy) {
	t.Helper()
	raw, err := common.Marshal(policy)
	require.NoError(t, err)
	common.OptionMapRWMutex.Lock()
	common.OptionMap["InvoicePolicy"] = string(raw)
	common.OptionMapRWMutex.Unlock()
}

func TestInvoiceMinimumAmountPolicy(t *testing.T) {
	s, _ := invoiceFixture(t)
	for _, tc := range []struct {
		name, value, amount string
		enabled             bool
	}{
		{"default", "", "20000", true},
		{"floor", `"20000"`, "20000", true},
		{"higher", `"60000"`, "60000", true},
		{"maximum", `"9223372036854775807"`, "9223372036854775807", true},
		{"below floor", `"19999"`, "", false},
		{"negative", `"-20000"`, "", false},
		{"decimal", `"20000.0"`, "", false},
		{"signed", `"+20000"`, "", false},
		{"whitespace", `" 20000"`, "", false},
		{"overflow", `"9223372036854775808"`, "", false},
		{"empty", `""`, "", false},
		{"number", `20000`, "", false},
		{"null", `null`, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := `{"enabled":true}`
			if tc.value != "" {
				raw = `{"enabled":true,"minimum_amount_minor":` + tc.value + `}`
			}
			common.OptionMapRWMutex.Lock()
			common.OptionMap["InvoicePolicy"] = raw
			common.OptionMapRWMutex.Unlock()
			policy := s.Policy()
			assert.Equal(t, tc.enabled, policy.Enabled)
			assert.Equal(t, "manual-v1", policy.PolicyVersion)
			if tc.enabled {
				assert.Equal(t, tc.amount, policy.MinimumAmountMinor)
			} else {
				assert.Equal(t, "POLICY_UNCONFIGURED", policy.DisabledReason)
			}
		})
	}
}

func TestInvoiceCreateMinimumAmount(t *testing.T) {
	for _, tc := range []struct {
		name    string
		amounts []int64
		minimum string
		allowed bool
	}{
		{"below floor", []int64{19999}, "20000", false},
		{"at floor", []int64{20000}, "20000", true},
		{"merged below floor", []int64{10000, 9999}, "20000", false},
		{"merged at floor", []int64{10000, 10000}, "20000", true},
		{"raised minimum rejects", []int64{50000}, "60000", false},
		{"raised minimum accepts", []int64{60000}, "60000", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, user := invoiceFixture(t)
			policy := s.Policy()
			policy.MinimumAmountMinor = tc.minimum
			invoiceSetTestPolicy(t, policy)
			input := invoiceCreateInput()
			input.Items = nil
			var total int64
			for index, amount := range tc.amounts {
				id := index + 1
				trade := "payment-a"
				if index == 0 {
					require.NoError(t, s.DB.Model(&model.TopUp{}).Where("id = ?", id).Update("paid_minor", amount).Error)
					require.NoError(t, s.DB.Model(&model.InvoicePaymentReceipt{}).Where("top_up_id = ?", id).Update("amount_minor", amount).Error)
				} else {
					trade = fmt.Sprintf("payment-%d", id)
					now := time.Now().Unix() - 100
					require.NoError(t, s.DB.Create(&model.TopUp{Id: id, UserId: user.UserID, TradeNo: trade, PaidMinor: amount, Currency: "CNY", PaymentProvider: "epay", Status: common.TopUpStatusSuccess, CompleteTime: now}).Error)
					require.NoError(t, s.DB.Create(&model.InvoicePaymentReceipt{TopUpID: int64(id), UserID: user.UserID, Provider: "epay", ReceiptKey: invoiceHash(trade), AmountMinor: amount, Currency: "CNY", Status: "verified", PaidAt: now, CreatedAt: now}).Error)
				}
				input.Items = append(input.Items, InvoiceOrderRef{SourceType: "topup", OrderID: trade})
				total += amount
			}
			quote, err := s.Quote(context.Background(), user, input.InvoiceQuoteInput)
			require.NoError(t, err)
			assert.True(t, quote.Eligible, "small payments remain selectable for merging")
			assert.Equal(t, strconv.FormatInt(total, 10), quote.AmountMinor)
			result, err := s.Create(context.Background(), user, "minimum-create", input)
			if tc.allowed {
				require.NoError(t, err)
				assert.Equal(t, strconv.FormatInt(total, 10), result.AmountMinor)
			} else {
				invoiceRequireCode(t, err, "INVOICE_BELOW_MINIMUM")
				var business *InvoiceError
				require.ErrorAs(t, err, &business)
				assert.Equal(t, 422, business.Status)
				var count int64
				require.NoError(t, s.DB.Model(&model.InvoiceOrderClaim{}).Count(&count).Error)
				assert.Zero(t, count)
			}
		})
	}
}

func TestInvoiceMinimumIncreasePreservesReplayAndInFlightRequest(t *testing.T) {
	s, user := invoiceFixture(t)
	ctx := context.Background()
	admin := InvoiceScope{UserID: 2, Admin: true}
	r, err := s.Create(ctx, user, "minimum-replay", invoiceCreateInput())
	require.NoError(t, err)
	policy := s.Policy()
	policy.MinimumAmountMinor = "60000"
	invoiceSetTestPolicy(t, policy)
	replay, err := s.Create(ctx, user, "minimum-replay", invoiceCreateInput())
	require.NoError(t, err)
	assert.Equal(t, r.PublicID, replay.PublicID)
	r, err = s.Mutate(ctx, admin, r.PublicID, "approve", "minimum-approve", InvoiceActionInput{Version: r.Version})
	require.NoError(t, err)
	require.NoError(t, s.DB.Create(&model.InvoiceAttachment{PublicID: "minimum-file", RequestID: 1, UploaderID: 2, Purpose: "invoice", ObjectKey: "minimum", FileName: "invoice.pdf", MIMEType: "application/pdf", Size: 100, SHA256: strings.Repeat("a", 64), Status: "validated", UploadKey: "minimum", ExpiresAt: time.Now().Unix() + 3600}).Error)
	r, err = s.Mutate(ctx, admin, r.PublicID, "deliver", "minimum-deliver", InvoiceActionInput{Version: r.Version, AttachmentID: "minimum-file", InvoiceNo: "minimum-invoice", IssuedOn: time.Now().UTC().Add(-24 * time.Hour).Format("2006-01-02"), AmountMinor: r.AmountMinor, Currency: r.Currency})
	require.NoError(t, err)
	assert.Equal(t, model.InvoiceDelivered, r.Status)
}

func TestInvoiceResolvedPrimaryMembershipReceipt(t *testing.T) {
	grant := `{"action":"grant","actor_id":2,"operation_id":"grant-1","reason":"Verified late payment","at":1234}`
	dismiss := `{"action":"dismiss","actor_id":2,"operation_id":"dismiss-extra","reason":"Extra payment handled","at":1235}`
	for _, tc := range []struct {
		name, primary, latest, extra string
		resolvedAt                   int64
		wrongAmount, allowed         bool
	}{
		{name: "audited grant", primary: grant, latest: grant, resolvedAt: 1234, allowed: true},
		{name: "grant then dismiss extra", primary: grant, latest: dismiss, extra: dismiss, resolvedAt: 1235, allowed: true},
		{name: "dismiss predates grant", primary: grant, latest: strings.Replace(dismiss, "1235", "1233", 1), extra: strings.Replace(dismiss, "1235", "1233", 1), resolvedAt: 1233},
		{name: "dismiss without matching extra", primary: grant, latest: dismiss, extra: grant, resolvedAt: 1235},
		{name: "different grant", primary: grant, latest: strings.Replace(grant, "grant-1", "grant-2", 1), resolvedAt: 1234},
		{name: "primary dismiss", primary: dismiss, latest: dismiss, resolvedAt: 1235},
		{name: "missing primary audit", latest: grant, resolvedAt: 1234},
		{name: "malformed audit", primary: "{", latest: "{", resolvedAt: 1234},
		{name: "missing order audit", primary: grant, resolvedAt: 1234},
		{name: "invalid actor", primary: strings.Replace(grant, `"actor_id":2`, `"actor_id":0`, 1), latest: grant, resolvedAt: 1234},
		{name: "invalid timestamp", primary: strings.Replace(grant, `"at":1234`, `"at":0`, 1), latest: grant, resolvedAt: 1234},
		{name: "grant predates payment", primary: strings.Replace(grant, "1234", "999", 1), latest: strings.Replace(grant, "1234", "999", 1), resolvedAt: 999},
		{name: "missing operation", primary: strings.Replace(grant, `"grant-1"`, `""`, 1), latest: grant, resolvedAt: 1234},
		{name: "blank reason", primary: strings.Replace(grant, "Verified late payment", " ", 1), latest: grant, resolvedAt: 1234},
		{name: "timestamp mismatch", primary: grant, latest: grant, resolvedAt: 1235},
		{name: "payment facts mismatch", primary: grant, latest: grant, resolvedAt: 1234, wrongAmount: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, user := invoiceFixture(t)
			key := "epay:late-primary"
			order := model.MembershipOrder{ID: 10, TradeNo: "late-member", UserID: user.UserID, AmountMinor: 69900, Currency: "CNY", Provider: "epay", Status: model.MembershipOrderSuccess, ReceiptKey: &key, ReceiptID: "late-primary", ActualAmountMinor: 69900, ActualCurrency: "CNY", ActualProvider: "epay", ActualOneTime: true, PaidAt: 1000, Resolution: tc.latest, ResolvedAt: tc.resolvedAt}
			require.NoError(t, s.DB.Create(&order).Error)
			receipt := model.MembershipPaymentReceipt{OrderID: order.ID, ReceiptKey: key, ReceiptID: order.ReceiptID, Provider: "epay", AmountMinor: order.AmountMinor, Currency: "CNY", OneTime: true, Status: "resolved", Resolution: tc.primary}
			if tc.wrongAmount {
				receipt.AmountMinor++
			}
			require.NoError(t, s.DB.Create(&receipt).Error)
			if tc.extra != "" {
				require.NoError(t, s.DB.Create(&model.MembershipPaymentReceipt{OrderID: order.ID, ReceiptKey: "epay:extra", ReceiptID: "extra", Provider: "epay", AmountMinor: order.AmountMinor, Currency: "CNY", OneTime: true, Status: "resolved", Resolution: tc.extra}).Error)
			}
			input := invoiceCreateInput()
			input.Items = []InvoiceOrderRef{{SourceType: "membership", OrderID: order.TradeNo}}
			quote, err := s.Quote(context.Background(), user, input.InvoiceQuoteInput)
			require.NoError(t, err)
			assert.Equal(t, tc.allowed, quote.Eligible)
			if tc.allowed {
				r, err := s.Create(context.Background(), user, "resolved-primary-create", input)
				require.NoError(t, err)
				assert.Equal(t, "69900", r.AmountMinor)
			} else {
				require.Len(t, quote.Items, 1)
				assert.Contains(t, quote.Items[0].ReasonCodes, "PAYMENT_UNVERIFIED")
			}
		})
	}
}

func TestInvoicePolicyReviewResumesIssuedRequest(t *testing.T) {
	s, user := invoiceFixture(t)
	ctx := context.Background()
	admin := InvoiceScope{UserID: 2, Admin: true}
	r, err := s.Create(ctx, user, "policy-review-create", invoiceCreateInput())
	require.NoError(t, err)
	r, err = s.Mutate(ctx, admin, r.PublicID, "approve", "policy-review-approve", InvoiceActionInput{Version: r.Version})
	require.NoError(t, err)
	require.NoError(t, s.DB.Model(&model.InvoiceRequest{}).Where("public_id = ?", r.PublicID).Update("issued_evidence", true).Error)
	var original model.InvoiceRequest
	require.NoError(t, s.DB.First(&original).Error)
	policy := s.Policy()
	policy.PolicyVersion = "test-v2"
	policy.MinimumAmountMinor = "60000"
	invoiceSetTestPolicy(t, policy)
	_, err = s.Mutate(ctx, admin, r.PublicID, "deliver", "policy-review-blocked", InvoiceActionInput{Version: r.Version})
	invoiceRequireCode(t, err, "POLICY_CHANGED")
	r, err = s.Mutate(ctx, admin, r.PublicID, "hold", "policy-review-hold", InvoiceActionInput{Version: r.Version, Reason: "Review policy compatibility"})
	require.NoError(t, err)
	assert.NotContains(t, r.AllowedActions, "reject")
	r, err = s.Mutate(ctx, admin, r.PublicID, "resume", "policy-review-resume", InvoiceActionInput{Version: r.Version, Reason: "Existing issuance remains compatible"})
	require.NoError(t, err)
	assert.Equal(t, "test-v2", r.PolicyVersion)
	assert.Equal(t, model.InvoiceApprovedForIssue, r.Status)
	require.NotEmpty(t, r.Events)
	assert.Contains(t, r.Events[len(r.Events)-1].Description, "test-v1 -> test-v2")
	var updated model.InvoiceRequest
	require.NoError(t, s.DB.First(&updated).Error)
	assert.Equal(t, original.ProfileCipher, updated.ProfileCipher)
	assert.Equal(t, original.AmountMinor, updated.AmountMinor)
	assert.Equal(t, original.SellerName, updated.SellerName)
	require.NoError(t, s.DB.Create(&model.InvoiceAttachment{PublicID: "review-file", RequestID: original.ID, UploaderID: 2, Purpose: "invoice", ObjectKey: "review", FileName: "invoice.pdf", MIMEType: "application/pdf", Size: 100, SHA256: strings.Repeat("a", 64), Status: "validated", UploadKey: "review", ExpiresAt: time.Now().Unix() + 3600}).Error)
	r, err = s.Mutate(ctx, admin, r.PublicID, "deliver", "policy-review-deliver", InvoiceActionInput{Version: r.Version, AttachmentID: "review-file", InvoiceNo: "review-invoice", IssuedOn: time.Now().UTC().Add(-24 * time.Hour).Format("2006-01-02"), AmountMinor: r.AmountMinor, Currency: r.Currency})
	require.NoError(t, err)
	assert.Equal(t, model.InvoiceDelivered, r.Status)
	r, err = s.Mutate(ctx, user, r.PublicID, "dispute", "policy-review-dispute", InvoiceActionInput{Version: r.Version, DeliveryVersion: r.CurrentDeliveryVersion, Category: "attachment", Description: "Please correct the delivered document"})
	require.NoError(t, err)
	require.NotNil(t, r.ActiveDispute)
	policy.PolicyVersion = "test-v3"
	invoiceSetTestPolicy(t, policy)
	_, err = s.Mutate(ctx, admin, r.PublicID, "reissue", "policy-review-block-reissue", InvoiceActionInput{Version: r.Version, DisputeID: r.ActiveDispute.ID})
	invoiceRequireCode(t, err, "POLICY_CHANGED")
	r, err = s.Mutate(ctx, admin, r.PublicID, "hold", "policy-review-reissue-hold", InvoiceActionInput{Version: r.Version, Reason: "Review correction under updated policy"})
	require.NoError(t, err)
	r, err = s.Mutate(ctx, admin, r.PublicID, "resume", "policy-review-reissue-resume", InvoiceActionInput{Version: r.Version, Reason: "Correction remains compatible"})
	require.NoError(t, err)
	assert.Equal(t, model.InvoiceDisputed, r.Status)
	assert.Equal(t, "test-v3", r.PolicyVersion)
	require.NoError(t, s.DB.Create(&model.InvoiceAttachment{PublicID: "reissue-file", RequestID: original.ID, UploaderID: 2, Purpose: "invoice", ObjectKey: "reissue", FileName: "corrected.pdf", MIMEType: "application/pdf", Size: 100, SHA256: strings.Repeat("a", 64), Status: "validated", UploadKey: "reissue", ExpiresAt: time.Now().Unix() + 3600}).Error)
	r, err = s.Mutate(ctx, admin, r.PublicID, "reissue", "policy-review-reissue", InvoiceActionInput{Version: r.Version, DisputeID: r.ActiveDispute.ID, AttachmentID: "reissue-file", InvoiceNo: "review-corrected", IssuedOn: time.Now().UTC().Add(-24 * time.Hour).Format("2006-01-02"), AmountMinor: r.AmountMinor, Currency: r.Currency})
	require.NoError(t, err)
	assert.EqualValues(t, 2, r.CurrentDeliveryVersion)
}

func TestInvoicePolicyReviewRejectsIncompatibleChanges(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		change     func(*InvoicePolicy)
	}{
		{"disabled", "FEATURE_DISABLED", func(p *InvoicePolicy) { p.Enabled = false }},
		{"seller ID", "POLICY_CHANGED", func(p *InvoicePolicy) { p.SellerID = "another-seller" }},
		{"seller name", "POLICY_CHANGED", func(p *InvoicePolicy) { p.SellerName = "Another Seller" }},
		{"tax group", "POLICY_CHANGED", func(p *InvoicePolicy) { p.TaxGroup = "another-tax-group" }},
		{"removed type", "INVOICE_TYPE_UNSUPPORTED", func(p *InvoicePolicy) { p.InvoiceTypes[0].ID = "different" }},
		{"required field", "VALIDATION_FAILED", func(p *InvoicePolicy) { p.InvoiceTypes[0].RequiredFields = []string{"bank_account"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, user := invoiceFixture(t)
			ctx := context.Background()
			admin := InvoiceScope{UserID: 2, Admin: true}
			r, err := s.Create(ctx, user, "policy-incompatible-create", invoiceCreateInput())
			require.NoError(t, err)
			r, err = s.Mutate(ctx, admin, r.PublicID, "hold", "policy-incompatible-hold", InvoiceActionInput{Version: r.Version, Reason: "Review policy"})
			require.NoError(t, err)
			policy := s.Policy()
			policy.PolicyVersion = "test-v2"
			tc.change(&policy)
			invoiceSetTestPolicy(t, policy)
			_, err = s.Mutate(ctx, admin, r.PublicID, "resume", "policy-incompatible-resume", InvoiceActionInput{Version: r.Version, Reason: "Attempt review"})
			invoiceRequireCode(t, err, tc.code)
			current, err := s.Get(ctx, user, r.PublicID)
			require.NoError(t, err)
			assert.Equal(t, model.InvoiceFinanceHold, current.Status)
			assert.Equal(t, "test-v1", current.PolicyVersion)
		})
	}
}

func TestInvoicePolicyReviewKeepsOldVersionOnPaymentFailure(t *testing.T) {
	s, user := invoiceFixture(t)
	ctx := context.Background()
	admin := InvoiceScope{UserID: 2, Admin: true}
	r, err := s.Create(ctx, user, "policy-payment-create", invoiceCreateInput())
	require.NoError(t, err)
	r, err = s.Mutate(ctx, admin, r.PublicID, "hold", "policy-payment-hold", InvoiceActionInput{Version: r.Version, Reason: "Review policy and payment"})
	require.NoError(t, err)
	policy := s.Policy()
	policy.PolicyVersion = "test-v2"
	invoiceSetTestPolicy(t, policy)
	require.NoError(t, s.DB.Model(&model.InvoicePaymentReceipt{}).Where("top_up_id = ?", 1).Update("status", "exception").Error)
	_, err = s.Mutate(ctx, admin, r.PublicID, "resume", "policy-payment-resume", InvoiceActionInput{Version: r.Version, Reason: "Review"})
	invoiceRequireCode(t, err, "PAYMENT_CHANGED")
	r, err = s.Get(ctx, user, r.PublicID)
	require.NoError(t, err)
	assert.Equal(t, model.InvoiceFinanceHold, r.Status)
	assert.Equal(t, "test-v1", r.PolicyVersion)
	assert.NotContains(t, r.Events[len(r.Events)-1].Description, "Policy reviewed")
}
