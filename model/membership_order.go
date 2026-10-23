package model

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// lockMembershipUserTx is the common lock order for checkout, activation and
// billing: user, member state, order/request, then quota account and token.
func lockMembershipUserTx(tx *gorm.DB, userID int) (*User, *MembershipUserState, error) {
	var user User
	if err := lockForUpdate(tx).First(&user, userID).Error; err != nil {
		return nil, nil, err
	}
	state := MembershipUserState{UserID: userID}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&state).Error; err != nil {
		return nil, nil, err
	}
	if err := lockForUpdate(tx).First(&state, "user_id = ?", userID).Error; err != nil {
		return nil, nil, err
	}
	return &user, &state, nil
}

func CreateMembershipOrder(input MembershipOrderInput) (*MembershipOrder, error) {
	if !MembershipEnabled() {
		return nil, ErrMembershipDisabled
	}
	if err := CheckMembershipSchema(); err != nil {
		return nil, err
	}
	now := common.GetTimestamp()
	if input.UserID <= 0 || input.ReleaseID <= 0 || input.TierID == "" || input.TradeNo == "" || len(input.TradeNo) > 128 || input.Provider == "" || len(input.Provider) > 32 || len(input.PaymentMethod) > 32 || input.Provider == PaymentProviderBalance || input.ExpiresAt <= now {
		return nil, ErrMembershipInvalidInput
	}
	var result *MembershipOrder
	err := membershipTransaction(func(tx *gorm.DB) error {
		_, state, err := lockMembershipUserTx(tx, input.UserID)
		if err != nil {
			return err
		}
		var current MembershipCatalogState
		if err := lockForUpdate(tx).First(&current, 1).Error; err != nil {
			return err
		}
		if !current.SalesEnabled {
			return ErrMembershipDisabled
		}
		if current.ReleaseID != input.ReleaseID {
			return ErrMembershipConflict
		}
		catalog, err := membershipCatalogTx(tx, input.ReleaseID)
		if err != nil {
			return err
		}
		var selected *MembershipTier
		for i := range catalog.Tiers {
			if catalog.Tiers[i].ID == input.TierID {
				selected = &catalog.Tiers[i]
				break
			}
		}
		if selected == nil || !selected.Enabled {
			return ErrMembershipInvalidInput
		}
		account, err := membershipAccountTx(tx, input.UserID, now)
		if err != nil {
			return err
		}
		terms := MembershipOrderTerms{Tier: *selected, ReleaseID: catalog.ReleaseID, DurationSeconds: MembershipDurationSeconds, Action: "purchase"}
		if account != nil && account.ActiveAt(now) {
			if selected.ID == account.TierID || selected.Rank <= account.Rank {
				return fmt.Errorf("%w: an active membership can only upgrade", ErrMembershipConflict)
			}
			terms.Action, terms.ExpectedAccountID, terms.ExpectedRevision = "upgrade", account.ID, account.Revision
		}
		if terms.ExpectedAccountID != input.ExpectedAccountID || terms.ExpectedRevision != input.ExpectedRevision {
			return ErrMembershipConflict
		}
		// Legacy contracts are preserved; no implicit conversion or parallel sale.
		if tx.Migrator().HasTable(&UserSubscription{}) {
			var legacy int64
			if err := tx.Model(&UserSubscription{}).Where("user_id = ? AND status = ? AND end_time > ?", input.UserID, "active", now).Count(&legacy).Error; err != nil {
				return err
			}
			if legacy > 0 {
				return fmt.Errorf("%w: an existing legacy subscription needs a transition first", ErrMembershipConflict)
			}
		}
		if tx.Migrator().HasTable(&SubscriptionOrder{}) {
			var pendingLegacy int64
			if err := tx.Model(&SubscriptionOrder{}).Where("user_id = ? AND status = ?", input.UserID, common.TopUpStatusPending).Count(&pendingLegacy).Error; err != nil {
				return err
			}
			if pendingLegacy > 0 {
				return fmt.Errorf("%w: close pending legacy subscription checkouts first", ErrMembershipConflict)
			}
		}
		if state.PendingOrderID != 0 {
			var pending MembershipOrder
			if err := lockForUpdate(tx).First(&pending, state.PendingOrderID).Error; err != nil {
				return err
			}
			if pending.Status == MembershipOrderPending && pending.ExpiresAt > now && pending.TierID == input.TierID && pending.ReleaseID == input.ReleaseID && pending.Provider == input.Provider && pending.PaymentMethod == input.PaymentMethod {
				var previous MembershipOrderTerms
				if err := common.UnmarshalJsonStr(pending.Terms, &previous); err != nil {
					return err
				}
				if previous.ExpectedAccountID == terms.ExpectedAccountID && previous.ExpectedRevision == terms.ExpectedRevision {
					result = &pending
					return nil
				}
			}
			if pending.Status == MembershipOrderPending {
				if err := tx.Model(&pending).Update("status", MembershipOrderClosed).Error; err != nil {
					return err
				}
			}
		}
		payload, err := common.Marshal(terms)
		if err != nil {
			return err
		}
		order := MembershipOrder{TradeNo: input.TradeNo, UserID: input.UserID, TierID: input.TierID, ReleaseID: input.ReleaseID, AmountMinor: selected.PriceMinor, Currency: selected.Currency, Provider: input.Provider, PaymentMethod: input.PaymentMethod, Status: MembershipOrderPending, ExpiresAt: input.ExpiresAt, Terms: string(payload), CreatedAt: now}
		if err := tx.Create(&order).Error; err != nil {
			return err
		}
		state.PendingOrderID = order.ID
		if err := tx.Save(state).Error; err != nil {
			return err
		}
		result = &order
		return nil
	})
	return result, err
}

func GetMembershipOrder(tradeNo string, userID int) (*MembershipOrder, error) {
	if err := CheckMembershipSchema(); err != nil {
		return nil, err
	}
	var order MembershipOrder
	query := DB.Where("trade_no = ?", tradeNo)
	if userID > 0 {
		query = query.Where("user_id = ?", userID)
	}
	if err := query.First(&order).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrMembershipNotFound
		}
		return nil, err
	}
	if err := DB.Where("order_id = ?", order.ID).Order("id asc").Find(&order.Payments).Error; err != nil {
		return nil, err
	}
	return &order, nil
}

func ListMembershipOrders(userID int, status string, limit int) ([]MembershipOrder, error) {
	if err := CheckMembershipSchema(); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	query := DB.Order("id desc").Limit(limit)
	if userID > 0 {
		query = query.Where("user_id = ?", userID)
	}
	if status != "" {
		if status == MembershipOrderException {
			query = query.Where("(status = ? AND resolved_at = 0) OR id IN (?)", status, DB.Model(&MembershipPaymentReceipt{}).Select("order_id").Where("status = ?", MembershipOrderException))
		} else {
			query = query.Where("status = ?", status)
		}
	}
	orders := []MembershipOrder{}
	err := query.Find(&orders).Error
	if err == nil && len(orders) > 0 {
		ids := make([]int64, 0, len(orders))
		for _, order := range orders {
			ids = append(ids, order.ID)
		}
		var receipts []MembershipPaymentReceipt
		if err := DB.Where("order_id IN ?", ids).Order("id asc").Find(&receipts).Error; err != nil {
			return nil, err
		}
		byOrder := map[int64][]MembershipPaymentReceipt{}
		for _, receipt := range receipts {
			byOrder[receipt.OrderID] = append(byOrder[receipt.OrderID], receipt)
		}
		for i := range orders {
			orders[i].Payments = byOrder[orders[i].ID]
		}
	}
	return orders, err
}

func CloseMembershipOrder(tradeNo string, userID int) (*MembershipOrder, error) {
	initial, err := GetMembershipOrder(tradeNo, userID)
	if err != nil {
		return nil, err
	}
	var result MembershipOrder
	err = membershipTransaction(func(tx *gorm.DB) error {
		_, state, err := lockMembershipUserTx(tx, initial.UserID)
		if err != nil {
			return err
		}
		if err := lockForUpdate(tx).First(&result, initial.ID).Error; err != nil {
			return err
		}
		if result.Status == MembershipOrderPending {
			result.Status = MembershipOrderClosed
			if err := tx.Save(&result).Error; err != nil {
				return err
			}
		}
		if state.PendingOrderID == result.ID {
			state.PendingOrderID = 0
			return tx.Save(state).Error
		}
		return nil
	})
	return &result, err
}

func CompleteMembershipPayment(input MembershipPaymentInput) (*MembershipOrder, error) {
	if !input.Paid || input.Provider == "" || len(input.Currency) > 8 || input.ReceiptID == "" || len(input.ReceiptID) > 128 || len(input.Provider) > 32 || input.AmountMinor < 0 {
		return nil, ErrMembershipInvalidInput
	}
	initial, err := GetMembershipOrder(input.TradeNo, 0)
	if err != nil {
		return nil, err
	}
	var result MembershipOrder
	err = membershipTransaction(func(tx *gorm.DB) error {
		_, state, err := lockMembershipUserTx(tx, initial.UserID)
		if err != nil {
			return err
		}
		if err := lockForUpdate(tx).First(&result, initial.ID).Error; err != nil {
			return err
		}
		now := common.GetTimestamp()
		receiptKey := input.Provider + ":" + input.ReceiptID
		var payment MembershipPaymentReceipt
		query := tx.Where("receipt_key = ?", receiptKey).First(&payment)
		if query.Error == nil && payment.OrderID == result.ID {
			// Lock an existing replay, without introducing a MySQL gap lock before
			// the unique insert that arbitrates concurrent first receipts below.
			if err := lockForUpdate(tx).Where("receipt_key = ?", receiptKey).First(&payment).Error; err != nil {
				return err
			}
			if payment.Provider != input.Provider || payment.ReceiptID != input.ReceiptID || payment.AmountMinor != input.AmountMinor || payment.Currency != strings.ToUpper(input.Currency) || payment.OneTime != input.OneTime {
				payment.Status, payment.Reason = MembershipOrderException, "verified payment receipt changed"
				if err := tx.Save(&payment).Error; err != nil {
					return err
				}
				result.ExceptionReason = payment.Reason
				if err := tx.Save(&result).Error; err != nil {
					return err
				}
				return holdMembershipInvoiceForPaymentExceptionTx(tx, result, now)
			}
			return nil
		}
		duplicate := query.Error == nil
		if query.Error != nil && !errors.Is(query.Error, gorm.ErrRecordNotFound) {
			return query.Error
		}
		if !duplicate {
			payment = MembershipPaymentReceipt{ReceiptKey: receiptKey, OrderID: result.ID, Provider: input.Provider, ReceiptID: input.ReceiptID, AmountMinor: input.AmountMinor, Currency: strings.ToUpper(input.Currency), OneTime: input.OneTime, Status: "received", CreatedAt: now}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&payment).Error; err != nil {
				return err
			}
			// A current read also sees a concurrent winner under MySQL's default
			// repeatable-read isolation after our unique insert waited for it.
			if err := lockForUpdate(tx).Where("receipt_key = ?", receiptKey).First(&payment).Error; err != nil {
				return err
			}
			duplicate = payment.OrderID != result.ID
		}
		if result.PaidAt > 0 {
			if !duplicate {
				payment.Status, payment.Reason = MembershipOrderException, "additional verified payment for an already paid order"
				if err := tx.Save(&payment).Error; err != nil {
					return err
				}
				result.ExceptionReason = payment.Reason
				if err := tx.Save(&result).Error; err != nil {
					return err
				}
				return holdMembershipInvoiceForPaymentExceptionTx(tx, result, now)
			}
			return nil
		}
		result.ReceiptID, result.ActualAmountMinor, result.ActualCurrency, result.ActualProvider, result.PaidAt = input.ReceiptID, input.AmountMinor, strings.ToUpper(input.Currency), input.Provider, now
		result.ActualOneTime = input.OneTime
		reason := strings.TrimSpace(input.ValidationError)
		if runes := []rune(reason); len(runes) > 512 {
			reason = string(runes[:512])
		}
		if duplicate {
			reason = "provider transaction already belongs to another order"
		} else {
			result.ReceiptKey = &receiptKey
		}
		if input.Provider != result.Provider {
			reason = "payment provider mismatch"
		}
		if !input.OneTime {
			reason = "payment is not one-time"
		}
		if result.AmountMinor != input.AmountMinor || result.Currency != result.ActualCurrency {
			reason = "payment amount or currency mismatch"
		}
		if result.Status != MembershipOrderPending || result.ExpiresAt <= now {
			reason = "payment arrived after order closure"
		}
		if reason == "" {
			err = activateMembershipOrderTx(tx, &result, state, now)
			if err != nil {
				if !errors.Is(err, ErrMembershipConflict) && !errors.Is(err, ErrMembershipInvalidInput) {
					return err
				}
				reason = err.Error()
			}
		}
		if reason != "" {
			result.Status, result.ExceptionReason = MembershipOrderException, reason
		}
		if !duplicate {
			payment.Status, payment.Reason = result.Status, reason
			if err := tx.Save(&payment).Error; err != nil {
				return err
			}
		}
		if state.PendingOrderID == result.ID {
			state.PendingOrderID = 0
			if err := tx.Save(state).Error; err != nil {
				return err
			}
		}
		// Business exceptions return success here so verified money is not rolled back.
		return tx.Save(&result).Error
	})
	if err != nil {
		return nil, err
	}
	return GetMembershipOrder(result.TradeNo, 0)
}

func activateMembershipOrderTx(tx *gorm.DB, order *MembershipOrder, state *MembershipUserState, now int64) error {
	if legacy, err := hasLegacyMembershipTx(tx, order.UserID, now); err != nil {
		return err
	} else if legacy {
		return fmt.Errorf("%w: existing legacy subscription must end first", ErrMembershipConflict)
	}
	var terms MembershipOrderTerms
	if err := common.UnmarshalJsonStr(order.Terms, &terms); err != nil {
		return err
	}
	if terms.Tier.Quota <= 0 || terms.DurationSeconds != MembershipDurationSeconds {
		return ErrMembershipInvalidInput
	}
	var current MembershipCatalogState
	if err := lockForShare(tx).First(&current, 1).Error; err != nil {
		return err
	}
	catalog, err := membershipCatalogTx(lockForShare(tx), current.ReleaseID)
	if err != nil {
		return err
	}
	retained := false
	for _, tier := range catalog.Tiers {
		if tier.ID == terms.Tier.ID {
			retained = true
			break
		}
	}
	if !retained {
		return fmt.Errorf("%w: purchased tier is missing from current pricing release", ErrMembershipConflict)
	}
	account, err := membershipAccountTx(tx, order.UserID, now)
	if err != nil {
		return err
	}
	active := account != nil && account.ActiveAt(now)
	if terms.Action == "upgrade" {
		if !active || account.ID != terms.ExpectedAccountID || account.Revision != terms.ExpectedRevision || terms.Tier.ID == account.TierID || terms.Tier.Rank <= account.Rank {
			return ErrMembershipConflict
		}
		if account.AmountTotal > math.MaxInt64-terms.Tier.Quota {
			return fmt.Errorf("%w: membership quota capacity exceeded", ErrMembershipInvalidInput)
		}
		account.AmountTotal += terms.Tier.Quota
		account.Revision++
	} else {
		if active {
			return ErrMembershipConflict
		}
		if account != nil {
			if err := tx.Model(account).Update("status", account.Status).Error; err != nil {
				return err
			}
		}
		account = &MembershipAccount{UserID: order.UserID, AmountTotal: terms.Tier.Quota, StartedAt: now, Revision: 1}
	}
	account.TierID, account.Rank, account.ReleaseID, account.CurrentOrderID = terms.Tier.ID, terms.Tier.Rank, terms.ReleaseID, order.ID
	account.Status, account.ExpiresAt = MembershipActive, now+terms.DurationSeconds
	if err := tx.Save(account).Error; err != nil {
		return err
	}
	state.CurrentAccountID = account.ID
	if err := tx.Save(state).Error; err != nil {
		return err
	}
	order.AccountID, order.Status, order.ExceptionReason = account.ID, MembershipOrderSuccess, ""
	return nil
}

func ResolveMembershipOrder(tradeNo string, actorID int, operationID, action, reason string) (*MembershipOrder, error) {
	operationID, reason = strings.TrimSpace(operationID), strings.TrimSpace(reason)
	if actorID <= 0 || operationID == "" || len(operationID) > 128 || reason == "" || len(reason) > 2048 || (action != "grant" && action != "dismiss") {
		return nil, ErrMembershipInvalidInput
	}
	initial, err := GetMembershipOrder(tradeNo, 0)
	if err != nil {
		return nil, err
	}
	var result MembershipOrder
	err = membershipTransaction(func(tx *gorm.DB) error {
		_, state, err := lockMembershipUserTx(tx, initial.UserID)
		if err != nil {
			return err
		}
		if err := lockForUpdate(tx).First(&result, initial.ID).Error; err != nil {
			return err
		}
		var previous struct {
			OperationID string `json:"operation_id"`
			ActorID     int    `json:"actor_id"`
			Action      string `json:"action"`
			Reason      string `json:"reason"`
		}
		if result.Resolution != "" {
			if err := common.UnmarshalJsonStr(result.Resolution, &previous); err != nil {
				return err
			}
			if previous.OperationID == operationID {
				if previous.ActorID != actorID || previous.Action != action || previous.Reason != reason {
					return ErrMembershipConflict
				}
				return nil
			}
			var unresolved int64
			if err := tx.Model(&MembershipPaymentReceipt{}).Where("order_id = ? AND status = ?", result.ID, MembershipOrderException).Count(&unresolved).Error; err != nil {
				return err
			}
			if action != "dismiss" || unresolved == 0 {
				return ErrMembershipConflict
			}
		}
		if result.PaidAt == 0 {
			return ErrMembershipConflict
		}
		if result.Status != MembershipOrderException {
			var unresolved int64
			if err := tx.Model(&MembershipPaymentReceipt{}).Where("order_id = ? AND status = ?", result.ID, MembershipOrderException).Count(&unresolved).Error; err != nil {
				return err
			}
			if action != "dismiss" || unresolved == 0 {
				return ErrMembershipConflict
			}
		}
		now := common.GetTimestamp()
		if action == "grant" {
			if result.ReceiptKey == nil || !result.ActualOneTime || result.ActualProvider != result.Provider || result.ActualAmountMinor != result.AmountMinor || result.ActualCurrency != result.Currency {
				return fmt.Errorf("%w: verified payment does not match order terms", ErrMembershipConflict)
			}
			if err := activateMembershipOrderTx(tx, &result, state, now); err != nil {
				return err
			}
		}
		payload, err := common.Marshal(map[string]any{"operation_id": operationID, "actor_id": actorID, "action": action, "reason": reason, "at": now})
		if err != nil {
			return err
		}
		result.Resolution, result.ResolvedAt = string(payload), now
		receipts := tx.Model(&MembershipPaymentReceipt{}).Where("order_id = ? AND status = ?", result.ID, MembershipOrderException)
		if action == "grant" {
			receipts = receipts.Where("receipt_key = ?", result.ReceiptKey)
		}
		if err := receipts.Updates(map[string]any{"status": "resolved", "resolution": string(payload)}).Error; err != nil {
			return err
		}
		if action == "dismiss" {
			result.ExceptionReason = ""
		}
		return tx.Save(&result).Error
	})
	return &result, err
}

func ClaimMembershipCheckout(tradeNo string, userID int) (*MembershipOrder, bool, error) {
	initial, err := GetMembershipOrder(tradeNo, userID)
	if err != nil {
		return nil, false, err
	}
	var result MembershipOrder
	claimed := false
	err = membershipTransaction(func(tx *gorm.DB) error {
		if _, _, err := lockMembershipUserTx(tx, initial.UserID); err != nil {
			return err
		}
		if err := lockForUpdate(tx).First(&result, initial.ID).Error; err != nil {
			return err
		}
		if result.Status != MembershipOrderPending || result.ExpiresAt <= common.GetTimestamp() {
			return ErrMembershipConflict
		}
		if result.CheckoutState == "ready" {
			return nil
		}
		if result.CheckoutState != "" {
			return ErrMembershipConflict
		}
		result.CheckoutState, result.CheckoutStartedAt = "creating", common.GetTimestamp()
		if err := tx.Save(&result).Error; err != nil {
			return err
		}
		claimed = true
		return nil
	})
	return &result, claimed, err
}

func SaveMembershipCheckout(tradeNo string, data string) error {
	var decoded any
	if len(data) > 65536 || common.UnmarshalJsonStr(data, &decoded) != nil {
		return ErrMembershipInvalidInput
	}
	updated := DB.Model(&MembershipOrder{}).Where("trade_no = ? AND checkout_state = ?", tradeNo, "creating").Updates(map[string]any{"checkout_state": "ready", "checkout_data": data})
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected != 1 {
		return ErrMembershipConflict
	}
	return nil
}

// ReleaseMembershipEpayCheckout only resets a failed local form-generation
// claim. Remote providers may already have created a payable checkout and must
// retain their claim when their outcome is uncertain.
func ReleaseMembershipEpayCheckout(tradeNo string, startedAt int64) error {
	if tradeNo == "" || startedAt <= 0 {
		return ErrMembershipInvalidInput
	}
	return DB.Model(&MembershipOrder{}).
		Where("trade_no = ? AND provider = ? AND status = ? AND checkout_state = ? AND checkout_started_at = ? AND checkout_data = ?",
			tradeNo, PaymentProviderEpay, MembershipOrderPending, "creating", startedAt, "").
		Updates(map[string]any{"checkout_state": "", "checkout_started_at": 0}).Error
}
