package controller

import (
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func ShopAdminOrderAction(action string) gin.HandlerFunc {
	return func(c *gin.Context) {
		var in struct {
			ExpectedVersion string   `json:"expected_version"`
			Reason          string   `json:"reason"`
			InternalReason  string   `json:"internal_reason"`
			PublicReason    string   `json:"public_reason"`
			EvidenceRefs    []string `json:"evidence_refs"`
			MissingKeys     []string `json:"missing_field_keys"`
			Visibility      string   `json:"visibility"`
			Body            string   `json:"body"`
		}
		if !shopBind(c, &in, 16384) {
			return
		}
		id := c.Param("order_no")
		if _, err := model.GetShopOrder(shopDB(c), shopScope(c), id); err != nil {
			shopModelError(c, err)
			return
		}
		cmd, ok := shopCommand(c, in, action, id, in.ExpectedVersion)
		if !ok {
			return
		}
		previous, ok := shopCommitted(c, cmd, action, id)
		if !ok {
			return
		}
		if previous != nil {
			shopRespondOrder(c, id, 200)
			return
		}
		if action == "risk-release" && !shopConsumeProof(c, service.VerificationScopeShopRiskRelease, service.ShopVerificationContext{
			Action: "release", TargetID: id, ExpectedVersion: in.ExpectedVersion, IdempotencyKey: cmd.Key,
			Details: service.ShopVerificationDetails{Reason: in.Reason, EvidenceIDs: in.EvidenceRefs},
		}) {
			return
		}
		reason := in.Reason
		switch action {
		case "request-input":
			reason = in.PublicReason
		case "risk-hold":
			reason = in.InternalReason
		case "order-note":
			reason = in.Body
		}
		_, err := model.MutateShopOrder(shopDB(c), model.ShopOrderActionInput{Command: cmd, OrderPublicID: id, Action: action, Reason: reason, PublicReason: in.PublicReason, EvidenceIDs: in.EvidenceRefs, MissingFieldKeys: in.MissingKeys, Visibility: in.Visibility})
		if err != nil {
			shopModelError(c, err)
			return
		}
		shopRespondOrder(c, id, 200)
	}
}

func ShopDeliverOrder(c *gin.Context)    { shopDeliver(c, false) }
func ShopReplaceDelivery(c *gin.Context) { shopDeliver(c, true) }

func shopDeliver(c *gin.Context, replacement bool) {
	var in struct {
		ExpectedVersion string                    `json:"expected_version"`
		OrderVersion    string                    `json:"order_version"`
		Reason          string                    `json:"reason"`
		SecretFields    []service.ShopSecretField `json:"secret_fields"`
		Instructions    string                    `json:"instructions"`
	}
	if !shopBind(c, &in, 64<<10) {
		return
	}
	id, caseID := c.Param("order_no"), ""
	action, proofAction, target := "delivery", "first", id
	if replacement {
		caseID = c.Param("case_id")
		row, err := model.GetShopAftersale(shopDB(c), shopScope(c), caseID)
		if err != nil {
			shopModelError(c, err)
			return
		}
		var order model.ShopOrder
		if err = shopDB(c).Select("public_id").First(&order, row.OrderID).Error; err != nil {
			shopModelError(c, err)
			return
		}
		id, action, proofAction, target = order.PublicID, "replacement", "replacement", caseID
	}
	detail, err := model.GetShopOrderDetail(shopDB(c), shopScope(c), id)
	if err != nil {
		shopModelError(c, err)
		return
	}
	cmd, ok := shopCommand(c, in, action, target, in.ExpectedVersion)
	if !ok {
		return
	}
	previous, ok := shopCommitted(c, cmd, action, target)
	if !ok {
		return
	}
	if previous != nil {
		if replacement {
			shopRespondAftersale(c, caseID, 200)
		} else {
			shopRespondOrder(c, id, 200)
		}
		return
	}
	orderVersion := cmd.ExpectedVersion
	if replacement {
		orderVersion, err = shopVersion(in.OrderVersion)
		if err != nil {
			shopModelError(c, err)
			return
		}
	} else if detail.Snapshot.DeliveryType != "manual" || in.OrderVersion != "" {
		shopModelError(c, model.ErrShopAction)
		return
	}
	proof := service.ShopVerificationContext{Action: proofAction, TargetID: id, ExpectedVersion: shopID(orderVersion), IdempotencyKey: cmd.Key, Details: service.ShopVerificationDetails{Reason: in.Reason, TemplateVersion: shopID(detail.Snapshot.Terms.TemplateVersion)}}
	if replacement {
		proof.Details.CaseID, proof.Details.CaseVersion = caseID, in.ExpectedVersion
	}
	if !shopConsumeProof(c, service.VerificationScopeShopDeliveryCreate, proof) {
		return
	}
	_, err = service.DeliverShopOrder(c.Request.Context(), shopDB(c), cmd, service.ShopDeliveryRequest{OrderPublicID: id, AftersalePublicID: caseID, OrderVersion: orderVersion, TemplateVersion: detail.Snapshot.Terms.TemplateVersion, SecretFields: in.SecretFields, Instructions: in.Instructions, Reason: in.Reason})
	if err != nil {
		shopModelError(c, err)
		return
	}
	if replacement {
		shopRespondAftersale(c, caseID, 200)
	} else {
		shopRespondOrder(c, id, 200)
	}
}

func ShopAdminRevealDelivery(c *gin.Context) {
	var in struct {
		ExpectedVersion string `json:"expected_version"`
		Reason          string `json:"reason"`
	}
	if !shopBind(c, &in, 8192) {
		return
	}
	id, deliveryID := c.Param("order_no"), c.Param("delivery_id")
	detail, err := model.GetShopOrderDetail(shopDB(c), shopScope(c), id)
	if err != nil {
		shopModelError(c, err)
		return
	}
	version, err := shopVersion(in.ExpectedVersion)
	if err != nil {
		shopModelError(c, err)
		return
	}
	if detail.Order.Version != version {
		shopModelError(c, model.ErrShopVersion)
		return
	}
	if detail.CurrentDelivery == nil || detail.CurrentDelivery.PublicID != deliveryID {
		shopModelError(c, model.ErrShopRevealBlocked)
		return
	}
	if !shopConsumeProof(c, service.VerificationScopeShopDeliveryReveal, service.ShopVerificationContext{Action: "reveal", TargetID: id, ExpectedVersion: in.ExpectedVersion, Details: service.ShopVerificationDetails{Reason: in.Reason, DeliveryID: deliveryID}}) {
		return
	}
	scope := shopScope(c)
	r, err := service.RevealShopDelivery(c.Request.Context(), shopDB(c), model.ShopRevealInput{Command: model.ShopCommand{ActorID: scope.UserID, TenantID: scope.TenantID, Admin: true, ExpectedVersion: version, Now: time.Now().Unix()}, OrderPublicID: id, DeliveryPublicID: deliveryID, Intent: "view", Reason: in.Reason}, "")
	if err != nil {
		shopModelError(c, err)
		return
	}
	shopSuccess(c, 200, gin.H{"order_no": r.Order.PublicID, "delivery_id": r.Delivery.PublicID, "delivery_version": shopID(r.Delivery.Version), "view_expires_at": shopTime(r.Delivery.ViewExpiresAt), "server_time": shopTime(time.Now().Unix()), "fields": r.Fields})
}
