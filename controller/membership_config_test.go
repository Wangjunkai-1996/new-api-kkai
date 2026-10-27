package controller

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestAdminGetMembershipConfigHandlesUnpreparedSchemaWithoutHidingFailures(t *testing.T) {
	for _, name := range []string{"missing_schema", "ready_schema", "closed_database", "uninitialized_database"} {
		t.Run(name, func(t *testing.T) {
			previousDB := model.DB
			t.Cleanup(func() { model.DB = previousDB })
			model.DB = nil
			if name != "uninitialized_database" {
				db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "membership.db")), &gorm.Config{})
				require.NoError(t, err)
				model.DB = db
				sqlDB, err := db.DB()
				require.NoError(t, err)
				t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
				if name == "ready_schema" {
					require.NoError(t, db.AutoMigrate(model.MembershipModels()...))
					payload, err := common.Marshal(model.MembershipCatalog{Tiers: []model.MembershipTier{{ID: "gold", Name: "Gold", Rank: 1, PriceMinor: 60000, Quota: 300000000, Currency: "CNY"}}, Groups: []model.MembershipGroup{}})
					require.NoError(t, err)
					release := model.MembershipPricingRelease{Payload: string(payload), ActorID: 1}
					require.NoError(t, db.Create(&release).Error)
					require.NoError(t, db.Create(&model.MembershipCatalogState{ID: 1, ReleaseID: release.ID, TopupDiscountsDisabled: true}).Error)
				}
				if name == "closed_database" {
					require.NoError(t, sqlDB.Close())
				}
			}

			router := gin.New()
			router.GET("/config", AdminGetMembershipConfig)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/config", nil))
			var response struct {
				Success bool   `json:"success"`
				Code    string `json:"code"`
				Data    struct {
					model.MembershipCatalog
					RuntimeEnabled *bool `json:"runtime_enabled"`
					SchemaReady    *bool `json:"schema_ready"`
				} `json:"data"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))

			switch {
			case name == "missing_schema" && !model.MembershipEnabled():
				assert.Equal(t, http.StatusOK, recorder.Code)
				require.True(t, response.Success)
				require.NotNil(t, response.Data.RuntimeEnabled)
				assert.False(t, *response.Data.RuntimeEnabled)
				require.NotNil(t, response.Data.SchemaReady)
				assert.False(t, *response.Data.SchemaReady)
				assert.False(t, response.Data.SalesEnabled)
				assert.False(t, response.Data.TopupDiscountsDisabled)
				assert.Zero(t, response.Data.ReleaseID)
				assert.NotNil(t, response.Data.ModelLimits)
				assert.NotNil(t, response.Data.Tiers)
				assert.Empty(t, response.Data.Tiers)
				assert.NotNil(t, response.Data.Groups)
				assert.Empty(t, response.Data.Groups)
			case name == "ready_schema":
				assert.Equal(t, http.StatusOK, recorder.Code)
				require.True(t, response.Success)
				require.NotNil(t, response.Data.RuntimeEnabled)
				assert.Equal(t, model.MembershipEnabled(), *response.Data.RuntimeEnabled)
				require.NotNil(t, response.Data.SchemaReady)
				assert.True(t, *response.Data.SchemaReady)
				assert.Positive(t, response.Data.ReleaseID)
				assert.False(t, response.Data.TopupDiscountsDisabled, "payment settings must remain editable with the historical disable flag")
				require.Len(t, response.Data.Tiers, 1)
				assert.Equal(t, "gold", response.Data.Tiers[0].ID)
				assert.EqualValues(t, 60000, response.Data.Tiers[0].PriceMinor)
			default:
				assert.GreaterOrEqual(t, recorder.Code, http.StatusBadRequest)
				assert.False(t, response.Success)
				assert.Nil(t, response.Data.RuntimeEnabled)
				if name != "closed_database" {
					assert.Equal(t, "membership_unavailable", response.Code)
				}
			}
		})
	}
}
