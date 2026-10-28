package model

import (
	"slices"

	"gorm.io/gorm"
)

type ShopAftersaleInput struct {
	Command           ShopCommand
	OrderPublicID     string
	IssueType         string
	DesiredResolution string
	Description       string
}
type ShopAftersaleMessageInput struct {
	Command           ShopCommand
	AftersalePublicID string
	Visibility        string
	Body              string
}
type ShopAftersaleDecisionInput struct {
	Command           ShopCommand
	AftersalePublicID string
	Decision          string
	PublicReason      string
	InternalReason    string
}

func shopLockAftersale(tx *gorm.DB, publicID string, userID int, active bool, now int64) (*ShopOrder, *ShopAftersale, error) {
	var ref ShopAftersale
	if err := tx.Where("public_id = ? AND tenant_id = ?", publicID, PlatformTenantID).First(&ref).Error; err != nil {
		return nil, nil, shopNotFound(err)
	}
	var orderRef ShopOrder
	if err := tx.Select("public_id").First(&orderRef, ref.OrderID).Error; err != nil {
		return nil, nil, err
	}
	_, order, err := shopLockOrder(tx, orderRef.PublicID, userID, active, now)
	if err != nil {
		return nil, nil, err
	}
	var row ShopAftersale
	if err = lockForUpdate(tx).Where("id = ? AND order_id = ?", ref.ID, order.ID).First(&row).Error; err != nil {
		return nil, nil, err
	}
	return order, &row, nil
}

func CreateShopAftersale(db *gorm.DB, in ShopAftersaleInput) (*ShopAftersale, error) {
	c := in.Command
	now := shopNow(c.Now)
	var result ShopAftersale
	err := shopTransaction(db, func(tx *gorm.DB) error {
		if err := shopAuthorize(tx, c, false); err != nil {
			return err
		}
		_, order, err := shopLockOrder(tx, in.OrderPublicID, c.ActorID, true, now)
		if err != nil {
			return err
		}
		k, err := shopIdempotency(tx, c, "create-aftersale", in.OrderPublicID)
		if err != nil {
			return err
		}
		if k != nil {
			return tx.Where("public_id = ? AND order_id = ?", k.ResourcePublicID, order.ID).First(&result).Error
		}
		if !slices.Contains([]string{"unusable", "mismatch", "not_delivered", "other"}, in.IssueType) || !slices.Contains([]string{"investigate", "replace", "refund"}, in.DesiredResolution) || !shopRequired(in.Description, 4000) {
			return ErrShopInvalid
		}
		if order.PaymentState != "paid" {
			return ErrShopAction
		}
		if order.ActiveAftersaleID != nil {
			if err = tx.Where("id = ? AND order_id = ? AND state IN ?", *order.ActiveAftersaleID, order.ID, []string{"pending", "processing"}).First(&result).Error; err != nil {
				return err
			}
			return shopRemember(tx, c, "create-aftersale", in.OrderPublicID, "aftersale", result.PublicID, nil)
		}
		rec, err := shopRecord("sa_", now)
		if err != nil {
			return err
		}
		result = ShopAftersale{ShopRecord: rec, TenantID: PlatformTenantID, UserID: order.UserID, OrderID: order.ID, State: "pending", IssueType: in.IssueType, RequestedResolution: in.DesiredResolution, Description: in.Description}
		if err = tx.Create(&result).Error; err != nil {
			return err
		}
		if err = shopUpdateOrder(tx, order, now, map[string]any{"active_aftersale_id": result.ID}); err != nil {
			return err
		}
		if err = shopCaseEvent(tx, c, order, nil, &result.ID, nil, "aftersale_created", "public", in.Description, nil); err != nil {
			return err
		}
		return shopRemember(tx, c, "create-aftersale", in.OrderPublicID, "aftersale", result.PublicID, nil)
	})
	return &result, err
}

func GetShopAftersale(db *gorm.DB, scope ShopScope, publicID string) (*ShopAftersale, error) {
	if err := shopReadScope(db, scope, scope.Admin); err != nil {
		return nil, err
	}
	var result ShopAftersale
	q := db.Where("public_id = ? AND tenant_id = ?", publicID, PlatformTenantID)
	if !scope.Admin {
		q = q.Where("user_id = ?", scope.UserID)
	}
	err := q.First(&result).Error
	return &result, shopNotFound(err)
}

func ListShopAftersales(db *gorm.DB, scope ShopScope, orderPublicID, state string, page, limit int) ([]ShopAftersale, int64, error) {
	if err := shopReadScope(db, scope, scope.Admin); err != nil {
		return nil, 0, err
	}
	q := db.Model(&ShopAftersale{}).Where("tenant_id = ?", PlatformTenantID)
	if !scope.Admin {
		q = q.Where("user_id = ?", scope.UserID)
	}
	if orderPublicID != "" {
		order, err := GetShopOrder(db, scope, orderPublicID)
		if err != nil {
			return nil, 0, err
		}
		q = q.Where("order_id = ?", order.ID)
	}
	if state != "" {
		q = q.Where("state = ?", state)
	}
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	if page > 1000000 {
		return nil, 0, ErrShopInvalid
	}
	var count int64
	if err := q.Count(&count).Error; err != nil {
		return nil, 0, err
	}
	var rows []ShopAftersale
	err := q.Order("id desc").Offset((page - 1) * limit).Limit(limit).Find(&rows).Error
	return rows, count, err
}

func AddShopAftersaleMessage(db *gorm.DB, in ShopAftersaleMessageInput) (*ShopAftersale, error) {
	c := in.Command
	var result *ShopAftersale
	err := shopTransaction(db, func(tx *gorm.DB) error {
		if err := shopAuthorize(tx, c, c.Admin); err != nil {
			return err
		}
		owner := c.ActorID
		if c.Admin {
			owner = 0
		}
		order, row, err := shopLockAftersale(tx, in.AftersalePublicID, owner, !c.Admin, shopNow(c.Now))
		if err != nil {
			return err
		}
		k, err := shopIdempotency(tx, c, "aftersale-message", in.AftersalePublicID)
		if err != nil {
			return err
		}
		if k != nil {
			result = row
			return nil
		}
		if err = shopVersion(row.Version, c.ExpectedVersion); err != nil {
			return err
		}
		visibility := in.Visibility
		if !c.Admin {
			visibility = "public"
		}
		if !slices.Contains([]string{"public", "internal"}, visibility) || !shopRequired(in.Body, 4000) {
			return ErrShopInvalid
		}
		if err = shopCAS(tx, &ShopAftersale{}, row.ID, row.Version, shopNow(c.Now), map[string]any{}); err != nil {
			return err
		}
		row.Version++
		row.UpdatedAt = shopNow(c.Now)
		if err = shopCaseEvent(tx, c, order, nil, &row.ID, nil, "aftersale_message", visibility, in.Body, nil); err != nil {
			return err
		}
		result = row
		return shopRemember(tx, c, "aftersale-message", in.AftersalePublicID, "aftersale", row.PublicID, nil)
	})
	return result, err
}

func DecideShopAftersale(db *gorm.DB, in ShopAftersaleDecisionInput) (*ShopAftersale, error) {
	c := in.Command
	now := shopNow(c.Now)
	var result *ShopAftersale
	err := shopTransaction(db, func(tx *gorm.DB) error {
		if err := shopAuthorize(tx, c, true); err != nil {
			return err
		}
		order, row, err := shopLockAftersale(tx, in.AftersalePublicID, 0, false, now)
		if err != nil {
			return err
		}
		k, err := shopIdempotency(tx, c, "aftersale-decision", in.AftersalePublicID)
		if err != nil {
			return err
		}
		if k != nil {
			result = row
			return nil
		}
		if err = shopVersion(row.Version, c.ExpectedVersion); err != nil {
			return err
		}
		if !slices.Contains([]string{"accept", "resolve", "reject", "close"}, in.Decision) || !shopRequired(in.PublicReason, 2000) || !shopBoundedText(in.InternalReason, 2000) {
			return ErrShopInvalid
		}
		if order.ActiveAftersaleID == nil || *order.ActiveAftersaleID != row.ID || !slices.Contains([]string{"pending", "processing"}, row.State) {
			return ErrShopAction
		}
		state := "processing"
		if in.Decision != "accept" {
			unresolved, err := shopOrderHasResponsibilities(tx, order, false)
			if err != nil {
				return err
			}
			if unresolved {
				return ErrShopResponsibilities
			}
			state = map[string]string{"resolve": "resolved", "reject": "rejected", "close": "closed"}[in.Decision]
		}
		fields := map[string]any{"state": state, "assigned_admin_id": c.ActorID, "resolution_code": in.Decision, "resolution_note": in.PublicReason}
		if state != "processing" {
			fields["closed_at"] = now
		}
		if err = shopCAS(tx, &ShopAftersale{}, row.ID, row.Version, now, fields); err != nil {
			return err
		}
		orderFields := map[string]any{}
		if state != "processing" {
			orderFields["active_aftersale_id"] = nil
		}
		if err = shopUpdateOrder(tx, order, now, orderFields); err != nil {
			return err
		}
		if err = shopCaseEvent(tx, c, order, nil, &row.ID, nil, "aftersale_"+in.Decision, "public", in.PublicReason, nil); err != nil {
			return err
		}
		if in.InternalReason != "" {
			if err = shopCaseEvent(tx, c, order, nil, &row.ID, nil, "aftersale_decision_note", "internal", in.InternalReason, nil); err != nil {
				return err
			}
		}
		if err = tx.First(row, row.ID).Error; err != nil {
			return err
		}
		result = row
		return shopRemember(tx, c, "aftersale-decision", in.AftersalePublicID, "aftersale", row.PublicID, nil)
	})
	return result, err
}

func shopCaseEvent(tx *gorm.DB, c ShopCommand, order *ShopOrder, receiptID, aftersaleID, refundID *int64, kind, visibility, note string, evidence []string) error {
	rec, err := shopRecord("se_", shopNow(c.Now))
	if err != nil {
		return err
	}
	ids, err := shopEvidenceJSON(tx, receiptID, evidence)
	if err != nil {
		return err
	}
	actor := "user"
	if c.Admin {
		actor = "admin"
	}
	if c.ActorID == 0 {
		actor = "system"
	}
	var orderID *int64
	if order != nil {
		orderID = &order.ID
	}
	return tx.Create(&ShopEvent{ShopRecord: rec, TenantID: PlatformTenantID, OrderID: orderID, ReceiptID: receiptID, AftersaleID: aftersaleID, RefundCaseID: refundID, Kind: kind, ActorID: c.ActorID, ActorKind: actor, Visibility: visibility, Note: note, EvidenceIDsJSON: ids}).Error
}
