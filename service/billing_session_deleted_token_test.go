package service

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestBillingSessionSurvivesDeletedToken(t *testing.T) {
	for _, tc := range []struct {
		name      string
		unlimited bool
		actual    int
	}{
		{name: "finite supplement", actual: 150},
		{name: "finite return difference", actual: 50},
		{name: "unlimited supplement", unlimited: true, actual: 150},
		{name: "unlimited return difference", unlimited: true, actual: 50},
	} {
		t.Run(tc.name, func(t *testing.T) {
			truncate(t)
			const userID, tokenID = 41001, 41001
			const initialQuota, reservedQuota = 1000, 100
			const tokenKey = "audit-session-deleted-token"
			seedUser(t, userID, initialQuota)
			seedToken(t, tokenID, userID, tokenKey, initialQuota)
			require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", tokenID).
				Update("unlimited_quota", tc.unlimited).Error)
			_, err := model.GetTokenById(tokenID)
			require.NoError(t, err)

			task := makeTask(userID, 0, 0, tokenID, "", 0)
			ctx, info := newDurableTaskBillingContext(t, task, tokenKey, "wallet_only", false)
			info.ForcePreConsume = true
			info.TokenUnlimited = tc.unlimited
			require.Nil(t, PreConsumeBilling(ctx, reservedQuota, info))
			require.NotNil(t, info.Billing)
			assert.EqualValues(t, initialQuota-reservedQuota, getUserQuota(t, userID))

			require.NoError(t, model.DeleteTokenById(tokenID, userID))
			_, err = model.GetTokenById(tokenID)
			require.ErrorIs(t, err, gorm.ErrRecordNotFound)
			require.NoError(t, info.Billing.Settle(tc.actual))
			assert.EqualValues(t, initialQuota-tc.actual, getUserQuota(t, userID))
			require.NoError(t, info.Billing.Settle(tc.actual))
			info.Billing.Refund(ctx)
			assert.EqualValues(t, initialQuota-tc.actual, getUserQuota(t, userID), "settlement replay and refund must not change a settled charge")

			var deleted model.Token
			require.NoError(t, model.DB.Unscoped().First(&deleted, tokenID).Error)
			assert.True(t, deleted.DeletedAt.Valid)
			_, err = model.GetTokenById(tokenID)
			assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
		})
	}
}
