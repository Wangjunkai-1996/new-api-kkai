package controller

import (
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func shopAftersaleDTO(row model.ShopAftersale, order model.ShopOrder, admin bool) gin.H {
	actions := []string{"message"}
	if admin && (row.State == "pending" || row.State == "processing") {
		actions = append(actions, "decide")
	}
	return gin.H{"id": row.PublicID, "version": shopID(row.Version), "order_no": order.PublicID, "order_version": shopID(order.Version), "state": row.State, "issue_type": row.IssueType, "desired_resolution": row.RequestedResolution, "description": row.Description, "created_at": shopTime(row.CreatedAt), "allowed_actions": actions, "messages": []gin.H{}}
}

func shopRespondAftersale(c *gin.Context, id string, status int) {
	row, err := model.GetShopAftersale(shopDB(c), shopScope(c), id)
	if err != nil {
		shopModelError(c, err)
		return
	}
	var order model.ShopOrder
	if err = shopDB(c).Select("public_id").First(&order, row.OrderID).Error; err != nil {
		shopModelError(c, err)
		return
	}
	detail, err := model.GetShopOrderDetail(shopDB(c), shopScope(c), order.PublicID)
	if err != nil {
		shopModelError(c, err)
		return
	}
	out := shopAftersaleDTO(*row, detail.Order, shopScope(c).Admin)
	if shopScope(c).Admin && row.State == "processing" && detail.Order.CurrentDeliveryID != nil && !model.ShopOrderDeliveryBlocked(detail) {
		out["allowed_actions"] = append(out["allowed_actions"].([]string), "replace")
	}
	messages, _, err := model.ListShopAftersaleMessages(shopDB(c), shopScope(c), id, 1, 100)
	if err != nil {
		shopModelError(c, err)
		return
	}
	items := []gin.H{}
	for _, e := range messages {
		items = append(items, gin.H{"id": e.PublicID, "body": e.Note, "visibility": e.Visibility, "created_at": shopTime(e.CreatedAt)})
	}
	out["messages"] = items
	shopSuccess(c, status, out)
}

func ShopGetAftersale(c *gin.Context) { shopRespondAftersale(c, c.Param("case_id"), 200) }
func ShopListAftersales(c *gin.Context) {
	page, size, ok := shopPagination(c)
	if !ok {
		return
	}
	orderNo := c.Param("order_no")
	if orderNo == "" {
		orderNo = c.Query("order_no")
	}
	rows, total, err := model.ListShopAftersales(shopDB(c), shopScope(c), orderNo, c.Query("state"), page, size)
	if err != nil {
		shopModelError(c, err)
		return
	}
	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.OrderID)
	}
	var orders []model.ShopOrder
	if len(ids) > 0 {
		if err = shopDB(c).Select("id", "public_id", "version").Where("id IN ? AND tenant_id = ?", ids, model.PlatformTenantID).Find(&orders).Error; err != nil {
			shopModelError(c, err)
			return
		}
	}
	byID := make(map[int64]model.ShopOrder, len(orders))
	for _, order := range orders {
		byID[order.ID] = order
	}
	items := []gin.H{}
	for _, row := range rows {
		order, exists := byID[row.OrderID]
		if !exists {
			shopModelError(c, model.ErrShopNotFound)
			return
		}
		items = append(items, shopAftersaleDTO(row, order, shopScope(c).Admin))
	}
	shopSuccess(c, 200, shopPage(items, page, size, total))
}

func ShopCreateAftersale(c *gin.Context) {
	var in struct {
		IssueType         string `json:"issue_type"`
		DesiredResolution string `json:"desired_resolution"`
		Description       string `json:"description"`
	}
	if !shopBind(c, &in, 16384) {
		return
	}
	id := c.Param("order_no")
	if _, err := model.GetShopOrder(shopDB(c), shopScope(c), id); err != nil {
		shopModelError(c, err)
		return
	}
	cmd, ok := shopCommand(c, in, "create-aftersale", id, "")
	if !ok {
		return
	}
	row, err := model.CreateShopAftersale(shopDB(c), model.ShopAftersaleInput{Command: cmd, OrderPublicID: id, IssueType: in.IssueType, DesiredResolution: in.DesiredResolution, Description: in.Description})
	if err != nil {
		shopModelError(c, err)
		return
	}
	shopRespondAftersale(c, row.PublicID, 201)
}

func ShopAftersaleMessage(c *gin.Context) {
	var in struct {
		ExpectedVersion string `json:"expected_version"`
		Body            string `json:"body"`
		Visibility      string `json:"visibility"`
	}
	if !shopBind(c, &in, 16384) {
		return
	}
	id := c.Param("case_id")
	if _, err := model.GetShopAftersale(shopDB(c), shopScope(c), id); err != nil {
		shopModelError(c, err)
		return
	}
	if !shopScope(c).Admin && in.Visibility != "" && in.Visibility != "public" {
		shopModelError(c, model.ErrShopInvalid)
		return
	}
	cmd, ok := shopCommand(c, in, "aftersale-message", id, in.ExpectedVersion)
	if !ok {
		return
	}
	_, err := model.AddShopAftersaleMessage(shopDB(c), model.ShopAftersaleMessageInput{Command: cmd, AftersalePublicID: id, Body: in.Body, Visibility: in.Visibility})
	if err != nil {
		shopModelError(c, err)
		return
	}
	shopRespondAftersale(c, id, 200)
}

func ShopDecideAftersale(c *gin.Context) {
	var in struct {
		ExpectedVersion string `json:"expected_version"`
		Decision        string `json:"decision"`
		PublicReason    string `json:"public_reason"`
		InternalReason  string `json:"internal_reason"`
	}
	if !shopBind(c, &in, 16384) {
		return
	}
	id := c.Param("case_id")
	if _, err := model.GetShopAftersale(shopDB(c), shopScope(c), id); err != nil {
		shopModelError(c, err)
		return
	}
	cmd, ok := shopCommand(c, in, "aftersale-decision", id, in.ExpectedVersion)
	if !ok {
		return
	}
	_, err := model.DecideShopAftersale(shopDB(c), model.ShopAftersaleDecisionInput{Command: cmd, AftersalePublicID: id, Decision: in.Decision, PublicReason: in.PublicReason, InternalReason: in.InternalReason})
	if err != nil {
		shopModelError(c, err)
		return
	}
	shopRespondAftersale(c, id, 200)
}
