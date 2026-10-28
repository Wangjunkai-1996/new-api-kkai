package service

import (
	"context"
	"errors"
	"net/mail"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

type ShopSecretField struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}
type ShopRevealedField struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Value string `json:"value"`
}
type shopSecretPayload struct {
	TemplateVersion int64               `json:"template_version"`
	Fields          []ShopRevealedField `json:"fields"`
}
type shopBuyerPayload struct {
	TemplateVersion int64             `json:"template_version"`
	Values          map[string]string `json:"values"`
}
type ShopDeliveryRequest struct {
	OrderPublicID, AftersalePublicID string
	OrderVersion, TemplateVersion    int64
	SecretFields                     []ShopSecretField
	Instructions                     string
	Reason                           string
}
type ShopDeliveryReveal struct {
	Order    model.ShopOrder
	Delivery model.ShopDelivery
	Fields   []ShopRevealedField
}

func shopObjectBinding(purpose, id, related string) ShopSecretBinding {
	return ShopSecretBinding{Purpose: purpose, TenantID: model.PlatformTenantID, ObjectID: id, RelatedID: related, TemplateVersion: "1"}
}

func shopDeliveryPayload(terms model.ShopProductTerms, fields []ShopSecretField) (shopSecretPayload, error) {
	result := shopSecretPayload{TemplateVersion: terms.TemplateVersion, Fields: []ShopRevealedField{}}
	if terms.TemplateVersion <= 0 || len(fields) > 20 || len(terms.DeliveryFields) == 0 || len(terms.DeliveryFields) > 20 {
		return result, model.ErrShopInvalid
	}
	values := map[string]string{}
	for _, f := range fields {
		if _, exists := values[f.Key]; exists || !utf8.ValidString(f.Value) || strings.ContainsRune(f.Value, 0) || len(f.Value) > 8192 || strings.TrimSpace(f.Value) == "" {
			return result, model.ErrShopInvalid
		}
		values[f.Key] = f.Value
	}
	for _, f := range terms.DeliveryFields {
		value, exists := values[f.Key]
		if !exists {
			if f.Required {
				return result, model.ErrShopInvalid
			}
			continue
		}
		result.Fields = append(result.Fields, ShopRevealedField{Key: f.Key, Label: f.Label, Value: value})
		delete(values, f.Key)
	}
	if len(values) > 0 || len(result.Fields) == 0 {
		return result, model.ErrShopInvalid
	}
	raw, err := common.Marshal(result)
	if err != nil || len(raw) > 32<<10 {
		return result, model.ErrShopInvalid
	}
	return result, nil
}

func validateShopPayload(payload shopSecretPayload, terms model.ShopProductTerms) error {
	if payload.TemplateVersion != terms.TemplateVersion {
		return model.ErrShopSecret
	}
	fields := make([]ShopSecretField, 0, len(payload.Fields))
	for _, f := range payload.Fields {
		fields = append(fields, ShopSecretField{Key: f.Key, Value: f.Value})
	}
	expected, err := shopDeliveryPayload(terms, fields)
	if err != nil || !slices.Equal(expected.Fields, payload.Fields) {
		return model.ErrShopSecret
	}
	return nil
}

func EncryptShopBuyerInput(orderPublicID string, terms model.ShopProductTerms, values map[string]string) (model.ShopSecret, []string, error) {
	if len(values) > 3 || len(terms.BuyerInputSchema) > 3 || terms.TemplateVersion <= 0 {
		return model.ShopSecret{}, nil, model.ErrShopInvalid
	}
	allowed := map[string]bool{}
	for _, f := range terms.BuyerInputSchema {
		allowed[f.Key] = true
	}
	normalized := map[string]string{}
	keys := []string{}
	for key, value := range values {
		if !allowed[key] || !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
			return model.ShopSecret{}, nil, model.ErrShopInvalid
		}
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		switch key {
		case "account_identifier":
			if utf8.RuneCountInString(value) > 320 || strings.ContainsAny(value, "\r\n") {
				return model.ShopSecret{}, nil, model.ErrShopInvalid
			}
		case "contact_email":
			address, err := mail.ParseAddress(value)
			if err != nil || address.Address != value || len(value) > 320 || strings.ContainsAny(value, "\r\n,;") {
				return model.ShopSecret{}, nil, model.ErrShopInvalid
			}
		case "order_note":
			if utf8.RuneCountInString(value) > 2000 {
				return model.ShopSecret{}, nil, model.ErrShopInvalid
			}
		default:
			return model.ShopSecret{}, nil, model.ErrShopInvalid
		}
		normalized[key] = value
		keys = append(keys, key)
	}
	slices.Sort(keys)
	secret, err := SealShopSecret(shopBuyerPayload{TemplateVersion: terms.TemplateVersion, Values: normalized}, shopObjectBinding("buyer_input", orderPublicID, ""))
	return secret, keys, err
}

func ReadShopBuyerInput(ctx context.Context, db *gorm.DB, c model.ShopCommand, orderPublicID string) (map[string]string, error) {
	order, err := model.ReadShopBuyerInput(db.WithContext(ctx), c, orderPublicID)
	if err != nil {
		return nil, err
	}
	var snapshot model.ShopOrderSnapshot
	var payload shopBuyerPayload
	if common.UnmarshalJsonStr(order.SnapshotJSON, &snapshot) != nil {
		return nil, model.ErrShopSecret
	}
	if err = OpenShopSecret(model.ShopSecret{Ciphertext: order.BuyerInputCiphertext, KeyVersion: order.BuyerInputKeyVersion}, shopObjectBinding("buyer_input", order.PublicID, ""), &payload); err != nil {
		return nil, err
	}
	if payload.TemplateVersion != snapshot.Terms.TemplateVersion {
		return nil, model.ErrShopSecret
	}
	return payload.Values, nil
}

func DeliverShopOrder(ctx context.Context, db *gorm.DB, c model.ShopCommand, in ShopDeliveryRequest) (*model.ShopDelivery, error) {
	db = db.WithContext(ctx)
	action, target := "delivery", in.OrderPublicID
	if in.AftersalePublicID != "" {
		action, target = "replacement", in.AftersalePublicID
	}
	// A committed replay returns only its delivery metadata, never re-decrypts.
	if c.ActorID > 0 {
		key, err := model.GetShopIdempotency(db, model.ShopScope{UserID: c.ActorID, TenantID: c.TenantID, Admin: c.Admin}, action, target, c.Key)
		if err != nil {
			return nil, err
		}
		if key != nil {
			return model.FulfillShopOrder(db, model.ShopFulfillInput{Command: c, OrderPublicID: in.OrderPublicID, AftersalePublicID: in.AftersalePublicID})
		}
	}
	var order *model.ShopOrder
	var err error
	if c.ActorID == 0 {
		var row model.ShopOrder
		err = db.Where("public_id = ? AND tenant_id = ?", in.OrderPublicID, model.PlatformTenantID).First(&row).Error
		order = &row
	} else {
		order, err = model.GetShopOrder(db, model.ShopScope{UserID: c.ActorID, TenantID: c.TenantID, Admin: c.Admin}, in.OrderPublicID)
	}
	if err != nil {
		return nil, err
	}
	var snapshot model.ShopOrderSnapshot
	if common.UnmarshalJsonStr(order.SnapshotJSON, &snapshot) != nil {
		return nil, model.ErrShopSecret
	}
	if in.TemplateVersion != snapshot.Terms.TemplateVersion {
		return nil, model.ErrShopProductChanged
	}
	id, err := model.NewShopPublicID("sd_")
	if err != nil {
		return nil, err
	}
	command := model.ShopFulfillInput{Command: c, OrderPublicID: order.PublicID, DeliveryPublicID: id, AftersalePublicID: in.AftersalePublicID, OrderVersion: in.OrderVersion, Instructions: in.Instructions, Reason: in.Reason}
	if snapshot.DeliveryType == "automatic" {
		if len(in.SecretFields) > 0 {
			return nil, model.ErrShopInvalid
		}
		stock, err := model.GetShopFulfillmentCandidate(db, order.PublicID)
		if err != nil {
			return nil, err
		}
		var payload shopSecretPayload
		if err = OpenShopSecret(model.ShopSecret{Ciphertext: stock.SecretCiphertext, KeyVersion: stock.SecretKeyVersion}, shopObjectBinding("inventory", stock.PublicID, ""), &payload); err != nil {
			return nil, err
		}
		if err = validateShopPayload(payload, snapshot.Terms); err != nil {
			return nil, err
		}
		command.StockUnitID, command.StockVersion = stock.ID, stock.Version
	} else {
		if c.ActorID <= 0 || !c.Admin {
			return nil, model.ErrShopAction
		}
		payload, err := shopDeliveryPayload(snapshot.Terms, in.SecretFields)
		if err != nil {
			return nil, err
		}
		command.Secret, err = SealShopSecret(payload, shopObjectBinding("delivery", id, order.PublicID))
		if err != nil {
			return nil, err
		}
	}
	return model.FulfillShopOrder(db, command)
}

func FulfillShopOrderAutomatically(ctx context.Context, db *gorm.DB, orderPublicID string) error {
	db = db.WithContext(ctx)
	var order model.ShopOrder
	if err := db.Where("public_id = ? AND tenant_id = ?", orderPublicID, model.PlatformTenantID).First(&order).Error; err != nil {
		return err
	}
	if order.CurrentDeliveryID != nil || order.PaymentState != "paid" || order.InputCompleteAt == 0 || order.RiskHold {
		return nil
	}
	var snapshot model.ShopOrderSnapshot
	if common.UnmarshalJsonStr(order.SnapshotJSON, &snapshot) != nil {
		return model.ErrShopSecret
	}
	if snapshot.DeliveryType != "automatic" {
		return nil
	}
	_, err := DeliverShopOrder(ctx, db, model.ShopCommand{TenantID: model.PlatformTenantID, Now: time.Now().Unix()}, ShopDeliveryRequest{OrderPublicID: order.PublicID, TemplateVersion: snapshot.Terms.TemplateVersion})
	if errors.Is(err, model.ErrShopStock) || errors.Is(err, model.ErrShopSecret) || errors.Is(err, ErrShopSecretUnavailable) {
		code := "STOCK_UNAVAILABLE"
		if !errors.Is(err, model.ErrShopStock) {
			code = "SECRET_UNAVAILABLE"
		}
		if markErr := model.MarkShopDeliveryException(db, order.PublicID, code, time.Now().Unix()); markErr != nil {
			return markErr
		}
	}
	return err
}

func RevealShopDelivery(ctx context.Context, db *gorm.DB, in model.ShopRevealInput, fieldKey string) (*ShopDeliveryReveal, error) {
	if in.Intent == "copy" && fieldKey == "" || in.Intent == "view" && fieldKey != "" {
		return nil, model.ErrShopInvalid
	}
	grant, err := model.RevealShopDelivery(db.WithContext(ctx), in)
	if err != nil {
		return nil, err
	}
	var snapshot model.ShopOrderSnapshot
	var payload shopSecretPayload
	if common.UnmarshalJsonStr(grant.Order.SnapshotJSON, &snapshot) != nil {
		return nil, model.ErrShopSecret
	}
	binding := shopObjectBinding("delivery", grant.Delivery.PublicID, grant.Order.PublicID)
	if grant.Stock != nil {
		binding = shopObjectBinding("inventory", grant.Stock.PublicID, "")
	}
	if err = OpenShopSecret(grant.Secret, binding, &payload); err != nil {
		return nil, err
	}
	if err = validateShopPayload(payload, snapshot.Terms); err != nil {
		return nil, err
	}
	fields := payload.Fields
	if fieldKey != "" {
		fields = nil
		for _, field := range payload.Fields {
			if field.Key == fieldKey {
				fields = []ShopRevealedField{field}
				break
			}
		}
		if len(fields) == 0 {
			return nil, model.ErrShopInvalid
		}
	}
	return &ShopDeliveryReveal{Order: grant.Order, Delivery: grant.Delivery, Fields: fields}, nil
}

func RevealShopInventory(ctx context.Context, db *gorm.DB, c model.ShopCommand, unitPublicID, reason string) ([]ShopRevealedField, error) {
	stock, err := model.RevealShopInventory(db.WithContext(ctx), c, unitPublicID, reason)
	if err != nil {
		return nil, err
	}
	var payload shopSecretPayload
	if err = OpenShopSecret(model.ShopSecret{Ciphertext: stock.SecretCiphertext, KeyVersion: stock.SecretKeyVersion}, shopObjectBinding("inventory", stock.PublicID, ""), &payload); err != nil {
		return nil, err
	}
	if payload.TemplateVersion <= 0 || len(payload.Fields) == 0 || len(payload.Fields) > 20 {
		return nil, model.ErrShopSecret
	}
	return payload.Fields, nil
}
