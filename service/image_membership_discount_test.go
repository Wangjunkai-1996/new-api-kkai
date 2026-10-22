package service

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImageMembershipSignedQuoteRequiresCurrentPublishedDiscount(t *testing.T) {
	_, user := setupMembershipServiceTest(t)
	imageDB, profile := newImageSubmissionTestDB(t)
	normalized, err := NormalizeImageStudioSubmission(context.Background(), imageDB, user.Id, ImageStudioSubmissionRequest{
		TokenID: 4, Model: profile.Model, Prompt: "a lighthouse",
	})
	require.NoError(t, err)
	imagePricing := imageStudioQuoteTestSnapshot(normalized)
	imagePricing.UnitPrice, imagePricing.GroupRatio = 1, 0.2
	info := &relaycommon.RelayInfo{
		UserId: user.Id, TokenId: normalized.TokenID, UsingGroup: "gpt", IsPlayground: true,
		OriginModelName: normalized.Model, ImagePricingSnapshot: imagePricing,
		PriceData: types.PriceData{GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 0.2}},
	}
	input, err := PrepareMembershipReservation(info, 100)
	require.NoError(t, err)
	var pricing types.MembershipPricing
	require.NoError(t, common.UnmarshalJsonStr(input.WalletSnapshot, &pricing))
	quote, err := NewImageStudioQuote(normalized, int(input.WalletQuota), map[string]float64{"n": 1}, imagePricing, &pricing)
	require.NoError(t, err)

	catalog, err := model.GetMembershipCatalog()
	require.NoError(t, err)
	catalog.Tiers[0].Discounts["gpt"] = 0.25
	catalog, err = model.PublishMembershipCatalog(*catalog, catalog.ReleaseID, 1)
	require.NoError(t, err)

	normalized.QuoteToken = quote.QuoteToken
	claims, err := ValidateImageStudioQuote(normalized, time.Now())
	require.NoError(t, err, "the original signature remains valid until its expiry")
	input, err = imageStudioMembershipReservation(normalized, claims)
	require.NoError(t, err)
	input.RequestID = "image-discount-publication"
	_, err = model.ReserveMembershipBilling(input)
	require.ErrorIs(t, err, model.ErrMembershipQuoteChanged, "an unaccepted signed quote cannot authorize the old discount after publication")

	input, err = PrepareMembershipReservation(info, 100)
	require.NoError(t, err)
	require.NoError(t, common.UnmarshalJsonStr(input.WalletSnapshot, &pricing))
	quote, err = NewImageStudioQuote(normalized, int(input.WalletQuota), map[string]float64{"n": 1}, imagePricing, &pricing)
	require.NoError(t, err)
	normalized.QuoteToken = quote.QuoteToken
	claims, err = ValidateImageStudioQuote(normalized, time.Now())
	require.NoError(t, err)
	input, err = imageStudioMembershipReservation(normalized, claims)
	require.NoError(t, err)
	input.RequestID = "image-discount-publication"
	record, err := model.ReserveMembershipBilling(input)
	require.NoError(t, err)
	assert.Equal(t, model.MembershipSource, record.Source)
	assert.EqualValues(t, 5, record.ReservedQuota)
	require.NoError(t, common.UnmarshalJsonStr(record.Snapshot, &pricing))
	assert.Equal(t, catalog.ReleaseID, pricing.ReleaseID)
	assert.Equal(t, 0.25, pricing.Discount)
	self, err := model.GetMembershipSelf(user.Id)
	require.NoError(t, err)
	require.NotNil(t, self.Account)
	assert.EqualValues(t, 5, self.Account.AmountReserved)
	assert.Zero(t, self.Account.AmountUsed)
}
