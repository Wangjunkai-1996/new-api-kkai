package model

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestTenantConsumptionSettlementSurvivesKeyLimitChanges(t *testing.T) {
	for _, lowerDuringRequest := range []bool{false, true} {
		name := "reservation_exhausts_key_limit"
		if lowerDuringRequest {
			name = "owner_lowers_key_limit"
		}
		t.Run(name, func(t *testing.T) {
			db := newResellerFinanceTestDB(t)
			user, token, input := tenantConsumptionFixture(t, db)
			if !lowerDuringRequest {
				require.NoError(t, db.Model(&token).Update("remain_quota", input.QuotedQuota).Error)
			}
			record, err := ReserveTenantConsumption(context.Background(), db, input, token.Id)
			require.NoError(t, err)
			if lowerDuringRequest {
				require.NoError(t, db.Model(&token).Update("remain_quota", 0).Error)
			}
			require.NoError(t, RecordTenantConsumptionResult(db, record.RequestID, 150, 0, false))
			require.NoError(t, SettleTenantConsumption(context.Background(), db, record.RequestID))
			require.NoError(t, SettleTenantConsumption(context.Background(), db, record.RequestID))
			require.NoError(t, db.First(record, record.ID).Error)
			assert.Equal(t, "settled", record.Status)
			assert.Empty(t, record.LastError)
			require.NoError(t, db.First(&user, user.Id).Error)
			assert.EqualValues(t, 850, user.Quota)
			assert.EqualValues(t, 150, user.UsedQuota)
			assert.Equal(t, 1, user.RequestCount)
			require.NoError(t, db.First(&token, token.Id).Error)
			assert.Equal(t, -50, token.RemainQuota)
			assert.Equal(t, 150, token.UsedQuota)
			var lot CustomerPaymentLot
			require.NoError(t, db.First(&lot).Error)
			assert.EqualValues(t, 850, lot.RemainingQuota)
			var count int64
			require.NoError(t, db.Model(&ResellerLedgerEntry{}).Count(&count).Error)
			assert.EqualValues(t, 1, count)
			input.RequestID = "next-request"
			_, err = ReserveTenantConsumption(context.Background(), db, input, token.Id)
			require.ErrorIs(t, err, ErrResellerInsufficientFunds, "the next request must still pass the Key limit")
			require.NoError(t, db.Model(&token).Update("remain_quota", 200).Error)
			_, err = ReserveTenantConsumption(context.Background(), db, input, token.Id)
			require.NoError(t, err, "the completed request must not leave a pending settlement blocking this customer")
		})
	}
}

func TestTenantAccountDeletionWaitsForFinancialSettlement(t *testing.T) {
	previousRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = previousRedisEnabled })
	for _, hardDelete := range []bool{false, true} {
		for _, refund := range []bool{false, true} {
			name := "soft_delete/settle"
			if hardDelete {
				name = "hard_delete/settle"
			}
			if refund {
				name += "_refund"
			}
			t.Run(name, func(t *testing.T) {
				db := newResellerFinanceTestDB(t)
				user, token, input := tenantConsumptionFixture(t, db)
				require.NoError(t, db.AutoMigrate(&UserAccessToken{}, &UserSession{}, &ExternalIdentityClaim{}, &TwoFABackupCode{}, &TwoFA{}, &AuthFlow{}, &PasskeyCredential{}, &UserOAuthBinding{}))
				accessToken := UserAccessToken{UserId: user.Id, TokenHash: "account-deletion-token"}
				require.NoError(t, db.Create(&accessToken).Error)
				record, err := ReserveTenantConsumption(context.Background(), db, input, token.Id)
				require.NoError(t, err)
				for _, pending := range []bool{false, true} {
					if pending {
						actual := int64(150)
						if refund {
							actual = 0
						}
						require.NoError(t, RecordTenantConsumptionResult(db, record.RequestID, actual, 0, refund))
					}
					if hardDelete {
						_, err = user.HardDelete()
					} else {
						_, err = user.Delete()
					}
					require.ErrorIs(t, err, ErrTenantAccountSettlementPending)
					var stored User
					require.NoError(t, db.First(&stored, user.Id).Error)
					assert.Equal(t, user.AuthVersion, stored.AuthVersion, "a rejected delete must not revoke the account")
					require.NoError(t, db.First(&accessToken, accessToken.Id).Error)
					require.NoError(t, db.First(&token, token.Id).Error)
				}
				require.NoError(t, SettleTenantConsumption(context.Background(), db, record.RequestID))
				if hardDelete {
					_, err = user.HardDelete()
				} else {
					_, err = user.Delete()
				}
				require.NoError(t, err)
				var deleted User
				require.ErrorIs(t, db.First(&deleted, user.Id).Error, gorm.ErrRecordNotFound)
				if !hardDelete {
					_, err = user.HardDelete()
					require.NoError(t, err, "an already soft-deleted account remains eligible for permanent deletion")
				}
				require.ErrorIs(t, db.Unscoped().First(&deleted, user.Id).Error, gorm.ErrRecordNotFound)
			})
		}
	}
}
