package model

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMembershipRelayStatisticsCommitOnceWithCappedFunding(t *testing.T) {
	user, catalog := setupMembershipTest(t)
	_, account := buyMembershipTest(t, user.Id, catalog, "gold", "relay-membership")
	require.NoError(t, DB.AutoMigrate(&Channel{}))
	channel := Channel{Name: "membership-relay", Key: "test", Status: 1, Type: 1}
	require.NoError(t, DB.Create(&channel).Error)
	_, err := ReserveMembershipBilling(membershipReservation(t, user.Id, "relay-statistics", 70, 80))
	require.NoError(t, err)
	record, err := SettleMembershipRelayBilling("relay-statistics", 90, channel.Id, true)
	require.NoError(t, err)
	assert.EqualValues(t, 70, record.ChargedQuota)
	assert.EqualValues(t, 20, record.ExcessQuota)
	_, err = SettleMembershipRelayBilling("relay-statistics", 90, channel.Id, true)
	require.NoError(t, err)
	require.NoError(t, DB.First(&user, user.Id).Error)
	assert.EqualValues(t, 70, user.UsedQuota)
	assert.Equal(t, 1, user.RequestCount)
	require.NoError(t, DB.First(&channel, channel.Id).Error)
	assert.EqualValues(t, 70, channel.UsedQuota)
	require.NoError(t, DB.First(account, account.ID).Error)
	assert.EqualValues(t, 70, account.AmountUsed)
	assert.Zero(t, account.AmountReserved)
}

func TestMembershipImageSettlementSharesDurableAccountingTransaction(t *testing.T) {
	user, catalog := setupMembershipTest(t)
	_, account := buyMembershipTest(t, user.Id, catalog, "gold", "image-membership")
	require.NoError(t, DB.AutoMigrate(&Channel{}, &KKAIImageGeneration{}, &KKAIImageAsset{}, &KKAIOutboxEvent{}))
	channel := Channel{Id: 1, Name: "membership-image", Key: "test", Status: 1, Type: 1}
	require.NoError(t, DB.Create(&channel).Error)
	generation := seedImageBillingGeneration(t, DB, user.Id)
	require.NoError(t, DB.Create(&Token{Id: generation.TokenID, UserId: user.Id, Key: "image-member-token", Status: 1, ExpiredTime: -1, RemainQuota: 500}).Error)
	input := membershipReservation(t, user.Id, "unused", 60, 70)
	input.TokenID = generation.TokenID
	input.SkipToken = false
	record, err := ReserveMembershipImageGenerationBilling(context.Background(), DB, generation.ID, input)
	require.NoError(t, err)
	assert.Equal(t, MembershipSource, record.Source)
	_, err = SettleImageGenerationBilling(context.Background(), DB, generation.ID, 60)
	require.Error(t, err, "settlement cannot precede durable delivery accounting")
	payload := ImageGenerationAccountingPayload{TargetQuota: 60, AssessedQuota: 75, CountStatistics: true, OutputCount: 1, LogParams: RecordConsumeLogParams{ChannelId: 1, TokenId: generation.TokenID, ModelName: generation.Model, Quota: 60}}
	require.NoError(t, PrepareImageGenerationAccounting(context.Background(), DB, generation.ID, payload))
	settled, err := SettleImageGenerationBilling(context.Background(), DB, generation.ID, 60)
	require.NoError(t, err)
	assert.EqualValues(t, 15, settled.MembershipRecord.ExcessQuota)
	_, err = SettleImageGenerationBilling(context.Background(), DB, generation.ID, 60)
	require.NoError(t, err)
	_, err = RefundImageGenerationBilling(context.Background(), DB, generation.ID)
	assert.ErrorIs(t, err, ErrImageBillingStateConflict)
	require.NoError(t, DB.First(account, account.ID).Error)
	assert.EqualValues(t, 60, account.AmountUsed)
	assert.Zero(t, account.AmountReserved)
	require.NoError(t, DB.First(&user, user.Id).Error)
	assert.EqualValues(t, 60, user.UsedQuota)
	assert.Equal(t, 1, user.RequestCount)
}

func TestMembershipImageWalletFallbackRefundIsIdempotent(t *testing.T) {
	user, catalog := setupMembershipTest(t)
	buyMembershipTest(t, user.Id, catalog, "gold", "image-fallback")
	require.NoError(t, DB.AutoMigrate(&KKAIImageGeneration{}, &KKAIOutboxEvent{}))
	generation := seedImageBillingGeneration(t, DB, user.Id)
	require.NoError(t, DB.Create(&Token{Id: generation.TokenID, UserId: user.Id, Key: "image-member-token", Status: 1, ExpiredTime: -1, RemainQuota: 500}).Error)
	input := membershipReservation(t, user.Id, "unused", 110, 120)
	input.TokenID = generation.TokenID
	input.SkipToken = false
	record, err := ReserveMembershipImageGenerationBilling(context.Background(), DB, generation.ID, input)
	require.NoError(t, err)
	assert.Equal(t, MembershipWalletSource, record.Source)
	require.NoError(t, DB.First(&generation, generation.ID).Error)
	assert.Equal(t, ImageMembershipWalletSource, generation.BillingSource)
	_, err = RefundImageGenerationBilling(context.Background(), DB, generation.ID)
	require.NoError(t, err)
	_, err = RefundImageGenerationBilling(context.Background(), DB, generation.ID)
	require.NoError(t, err)
	require.NoError(t, DB.First(&user, user.Id).Error)
	assert.EqualValues(t, 500, user.Quota)
	record, err = GetMembershipBillingRecord(MembershipImageBillingKey(generation.ID))
	require.NoError(t, err)
	assert.Equal(t, "released", record.State)
}
