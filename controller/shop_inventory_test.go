package controller

import (
	"math"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestShopInventoryProjectionRespectsFulfillmentHolds(t *testing.T) {
	// Projection-only fixture; it never invokes a commerce mutation or bypasses
	// the canonical production migration gate.
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "inventory.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.ShopProduct{}, &model.ShopOrder{}, &model.ShopStockUnit{}, &model.ShopDelivery{}, &model.ShopPaymentReceipt{}, &model.ShopRefundCase{}, &model.ShopPaymentAdjustment{}))
	previous := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previous })
	now := time.Now().Unix()
	id := int64(1)
	product := model.ShopProduct{ShopRecord: model.ShopRecord{ID: 1, PublicID: "product"}, TenantID: model.PlatformTenantID}
	order := model.ShopOrder{ShopRecord: model.ShopRecord{ID: 1, PublicID: "order"}, TenantID: model.PlatformTenantID, ProductID: 1, PaymentState: "paid", CurrentDeliveryID: &id, EffectiveReceiptID: &id}
	stock := model.ShopStockUnit{ShopRecord: model.ShopRecord{ID: 1, PublicID: "stock", Version: 1}, TenantID: model.PlatformTenantID, ProductID: 1, State: "sold", SoldOrderID: &id, SecretCiphertext: "must-not-leak", IdentityFingerprint: "must-not-leak", MaskedLabel: "••••"}
	delivery := model.ShopDelivery{ShopRecord: model.ShopRecord{ID: 1, PublicID: "delivery"}, OrderID: 1, StockUnitID: &id, ViewExpiresAt: now + 3600}
	receipt := model.ShopPaymentReceipt{ShopRecord: model.ShopRecord{ID: 1, PublicID: "receipt"}, ReceiptKey: "receipt", OrderID: &id, ProcessingState: "matched"}
	for _, row := range []any{&product, &order, &stock, &delivery, &receipt} {
		require.NoError(t, db.Create(row).Error)
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/inventory", nil)
	items, err := shopInventoryDTOs(c, []model.ShopStockUnit{stock})
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "product", items[0]["product_id"])
	assert.Equal(t, "order", items[0]["order_no"])
	assert.Equal(t, "delivered", items[0]["state"])
	assert.Contains(t, items[0]["allowed_actions"], "reveal")
	raw, err := common.Marshal(items)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "must-not-leak")
	require.NoError(t, db.Model(&order).Update("risk_hold", true).Error)
	items, err = shopInventoryDTOs(c, []model.ShopStockUnit{stock})
	require.NoError(t, err)
	assert.NotContains(t, items[0]["allowed_actions"], "reveal")
	require.NoError(t, db.Model(&order).Update("risk_hold", false).Error)
	refund := model.ShopRefundCase{ShopRecord: model.ShopRecord{PublicID: "refund"}, ReceiptID: 1, State: "approved"}
	require.NoError(t, db.Create(&refund).Error)
	items, err = shopInventoryDTOs(c, []model.ShopStockUnit{stock})
	require.NoError(t, err)
	assert.NotContains(t, items[0]["allowed_actions"], "reveal")
}

func TestShopMoneyTotalRejectsOverflowWithoutRounding(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "totals.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.ShopPaymentReceipt{}))
	rows := []model.ShopPaymentReceipt{{ShopRecord: model.ShopRecord{PublicID: "one"}, ReceiptKey: "one", AmountMinor: math.MaxInt64 - 1}, {ShopRecord: model.ShopRecord{PublicID: "two"}, ReceiptKey: "two", AmountMinor: 1}}
	require.NoError(t, db.Create(&rows).Error)
	total, err := shopMoneyTotal(db.Model(&model.ShopPaymentReceipt{}), "CNY", 2)
	require.NoError(t, err)
	assert.Equal(t, "9223372036854775807", total["amount_minor"])
	require.NoError(t, db.Create(&model.ShopPaymentReceipt{ShopRecord: model.ShopRecord{PublicID: "three"}, ReceiptKey: "three", AmountMinor: 1}).Error)
	_, err = shopMoneyTotal(db.Model(&model.ShopPaymentReceipt{}), "CNY", 2)
	assert.ErrorIs(t, err, errShopAmountLimit)
}
