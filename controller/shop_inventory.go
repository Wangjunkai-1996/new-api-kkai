package controller

import (
	"slices"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

type shopInventoryImportInput struct {
	ProductID       string                     `json:"product_id"`
	ProductVersion  string                     `json:"product_version"`
	TemplateVersion string                     `json:"template_version"`
	Rows            []service.ShopInventoryRow `json:"rows"`
	PreviewDigest   string                     `json:"preview_digest,omitempty"`
}

// Inventory lists only project public references and a masked label. In
// particular neither ciphertext nor deduplication fingerprints leave the API.
func shopInventoryDTOs(c *gin.Context, rows []model.ShopStockUnit) ([]gin.H, error) {
	db := shopDB(c)
	productIDs, orderIDs, stockIDs := []int64{}, []int64{}, []int64{}
	for _, row := range rows {
		productIDs = append(productIDs, row.ProductID)
		stockIDs = append(stockIDs, row.ID)
		if row.ReservedOrderID != nil {
			orderIDs = append(orderIDs, *row.ReservedOrderID)
		}
		if row.SoldOrderID != nil {
			orderIDs = append(orderIDs, *row.SoldOrderID)
		}
	}
	products := map[int64]string{}
	orders := map[int64]string{}
	canReveal := map[int64]bool{}
	if len(rows) > 0 {
		var productRows []model.ShopProduct
		if err := db.Select("id", "public_id").Where("tenant_id = ? AND id IN ?", model.PlatformTenantID, productIDs).Find(&productRows).Error; err != nil {
			return nil, err
		}
		for _, row := range productRows {
			products[row.ID] = row.PublicID
		}
		if len(orderIDs) > 0 {
			var orderRows []model.ShopOrder
			if err := db.Select("id", "public_id").Where("tenant_id = ? AND id IN ?", model.PlatformTenantID, orderIDs).Find(&orderRows).Error; err != nil {
				return nil, err
			}
			for _, row := range orderRows {
				orders[row.ID] = row.PublicID
			}
			refunds := db.Model(&model.ShopRefundCase{}).Select("receipt_id").Where("state <> ?", "cancelled")
			adjustments := db.Model(&model.ShopPaymentAdjustment{}).Select("receipt_id").Where("state IN ?", []string{"pending", "succeeded", "disputed"})
			receipts := db.Model(&model.ShopPaymentReceipt{}).Select("id").Where("processing_state = ? AND order_id = shop_orders.id AND id NOT IN (?) AND id NOT IN (?)", "matched", refunds, adjustments)
			eligible := db.Model(&model.ShopOrder{}).Select("current_delivery_id").Where("tenant_id = ? AND id IN ? AND payment_state = ? AND risk_hold = ? AND current_delivery_id IS NOT NULL AND effective_receipt_id IN (?)", model.PlatformTenantID, orderIDs, "paid", false, receipts)
			var deliveries []model.ShopDelivery
			if err := db.Select("stock_unit_id").Where("id IN (?) AND stock_unit_id IN ? AND reveal_revoked_at = 0 AND secret_purged_at = 0 AND view_expires_at > ?", eligible, stockIDs, time.Now().Unix()).Find(&deliveries).Error; err != nil {
				return nil, err
			}
			for _, row := range deliveries {
				if row.StockUnitID != nil {
					canReveal[*row.StockUnitID] = true
				}
			}
		}
	}
	items := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		actions := []string{}
		if row.State != "quarantined" {
			actions = append(actions, "quarantine")
		}
		if row.SecretPurgedAt == 0 && row.ReservedOrderID == nil && (row.SoldOrderID == nil && slices.Contains([]string{"available", "quarantined"}, row.State) || row.State == "sold" && canReveal[row.ID]) {
			actions = append(actions, "reveal")
		}
		state := row.State
		if state == "sold" {
			state = "delivered"
		}
		item := gin.H{"id": row.PublicID, "version": shopID(row.Version), "product_id": products[row.ProductID], "state": state, "masked_identifier": row.MaskedLabel, "batch_id": row.ImportBatchPublicID, "order_no": nil, "created_at": shopTime(row.CreatedAt), "allowed_actions": actions}
		orderID := row.ReservedOrderID
		if orderID == nil {
			orderID = row.SoldOrderID
		}
		if orderID != nil && orders[*orderID] != "" {
			item["order_no"] = orders[*orderID]
		}
		items = append(items, item)
	}
	return items, nil
}

func ListShopInventory(c *gin.Context) {
	page, size, ok := shopPagination(c)
	if !ok {
		return
	}
	state := c.Query("state")
	if !slices.Contains([]string{"", "available", "reserved", "delivered", "quarantined"}, state) {
		shopModelError(c, model.ErrShopInvalid)
		return
	}
	if state == "delivered" {
		state = "sold"
	}
	rows, total, err := model.ListShopInventory(shopDB(c), shopScope(c), model.ShopRecordFilter{Page: page, Limit: size, ProductPublicID: c.Query("product_id"), BatchPublicID: c.Query("batch_id"), State: state})
	if err != nil {
		shopModelError(c, err)
		return
	}
	items, err := shopInventoryDTOs(c, rows)
	if err != nil {
		shopModelError(c, err)
		return
	}
	shopSuccess(c, 200, shopPage(items, page, size, total))
}

func PreviewShopInventory(c *gin.Context) { shopImportInventory(c, false) }
func CommitShopInventory(c *gin.Context)  { shopImportInventory(c, true) }

func shopImportInventory(c *gin.Context, commit bool) {
	var in shopInventoryImportInput
	if !shopBind(c, &in, 1<<20) {
		return
	}
	version, err := shopVersion(in.ProductVersion)
	if err != nil {
		shopModelError(c, err)
		return
	}
	template, err := shopVersion(in.TemplateVersion)
	if err != nil || !commit && in.PreviewDigest != "" {
		shopModelError(c, model.ErrShopInvalid)
		return
	}
	scope := shopScope(c)
	command := model.ShopCommand{ActorID: scope.UserID, TenantID: scope.TenantID, Admin: scope.Admin, ExpectedVersion: version, Now: time.Now().Unix()}
	request := service.ShopInventoryImportRequest{ProductID: in.ProductID, ProductVersion: version, TemplateVersion: template, Rows: in.Rows}
	if !commit {
		preview, err := service.PreviewShopInventory(c.Request.Context(), shopDB(c), command, request)
		if err != nil {
			shopModelError(c, err)
			return
		}
		shopSuccess(c, 200, gin.H{"preview_digest": preview.PreviewDigest, "valid_count": preview.ValidCount, "duplicate_count": preview.DuplicateCount, "invalid_count": preview.InvalidCount, "errors": preview.Issues})
		return
	}
	command, ok := shopCommand(c, in, "inventory-import", in.ProductID, in.ProductVersion)
	if !ok {
		return
	}
	result, err := service.CommitShopInventory(c.Request.Context(), shopDB(c), command, request, in.PreviewDigest)
	if err != nil {
		shopModelError(c, err)
		return
	}
	shopSuccess(c, 200, gin.H{"batch_id": result.BatchID, "inserted_count": result.InsertedCount})
}

func RecoverShopInventoryImport(c *gin.Context) {
	key, product := c.Param("key"), c.Query("product_id")
	if !shopKeyPattern.MatchString(key) || product == "" {
		shopModelError(c, model.ErrShopInvalid)
		return
	}
	previous, err := model.GetShopIdempotency(shopDB(c), shopScope(c), "inventory-import", product, key)
	if err != nil {
		shopModelError(c, err)
		return
	}
	result := gin.H{"status": "not_observed", "batch_id": nil, "inserted_count": nil}
	if previous != nil {
		var value model.ShopImportResult
		if common.UnmarshalJsonStr(previous.ResultMetadataJSON, &value) != nil || value.BatchID == "" || value.InsertedCount <= 0 {
			shopModelError(c, model.ErrShopInvalid)
			return
		}
		result = gin.H{"status": "recorded", "batch_id": value.BatchID, "inserted_count": value.InsertedCount}
	}
	shopSuccess(c, 200, result)
}

func QuarantineShopInventory(c *gin.Context) {
	var in struct {
		ExpectedVersion string `json:"expected_version"`
		Reason          string `json:"reason"`
	}
	if !shopBind(c, &in, 16384) {
		return
	}
	if in.ExpectedVersion == "" {
		shopModelError(c, model.ErrShopInvalid)
		return
	}
	id := c.Param("unit_id")
	command, ok := shopCommand(c, in, "inventory-quarantine", id, in.ExpectedVersion)
	if !ok {
		return
	}
	row, err := model.QuarantineShopInventory(shopDB(c), command, id, in.Reason)
	if err != nil {
		shopModelError(c, err)
		return
	}
	items, err := shopInventoryDTOs(c, []model.ShopStockUnit{*row})
	if err != nil {
		shopModelError(c, err)
		return
	}
	shopSuccess(c, 200, items[0])
}

func RevealShopInventory(c *gin.Context) {
	var in struct {
		ExpectedVersion string `json:"expected_version"`
		Reason          string `json:"reason"`
	}
	if !shopBind(c, &in, 16384) {
		return
	}
	version, err := shopVersion(in.ExpectedVersion)
	if err != nil {
		shopModelError(c, err)
		return
	}
	id := c.Param("unit_id")
	row, err := model.GetShopInventory(shopDB(c), shopScope(c), id)
	if err != nil {
		shopModelError(c, err)
		return
	}
	if row.Version != version {
		shopModelError(c, model.ErrShopVersion)
		return
	}
	if !shopConsumeProof(c, service.VerificationScopeShopInventoryReveal, service.ShopVerificationContext{Action: "reveal", TargetID: id, ExpectedVersion: in.ExpectedVersion, Details: service.ShopVerificationDetails{Reason: in.Reason}}) {
		return
	}
	scope, now := shopScope(c), time.Now().Unix()
	command := model.ShopCommand{ActorID: scope.UserID, TenantID: scope.TenantID, Admin: scope.Admin, ExpectedVersion: version, Now: now}
	fields, err := service.RevealShopInventory(c.Request.Context(), shopDB(c), command, id, in.Reason)
	if err != nil {
		shopModelError(c, err)
		return
	}
	// The administrator's transient view is capped at one minute. Sold stock
	// also keeps the order's original viewing deadline.
	expires := now + 60
	var delivery model.ShopDelivery
	stock := shopDB(c).Model(&model.ShopStockUnit{}).Select("id").Where("public_id = ? AND tenant_id = ?", id, model.PlatformTenantID)
	if err := shopDB(c).Select("view_expires_at").Where("stock_unit_id IN (?)", stock).Find(&delivery).Error; err != nil {
		shopModelError(c, err)
		return
	}
	if delivery.ViewExpiresAt > 0 {
		expires = min(expires, delivery.ViewExpiresAt)
	}
	shopSuccess(c, 200, gin.H{"fields": fields, "view_expires_at": shopTime(expires), "server_time": shopTime(now)})
}
