package model

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrWalletPaymentSchema          = errors.New("wallet payment schema is not ready")
	ErrWalletFlowActive             = errors.New("current wallet flow is active")
	ErrWalletFlowChanged            = errors.New("wallet flow changed")
	ErrWalletOrderChanged           = errors.New("wallet order changed")
	ErrWalletIdempotencyConflict    = errors.New("wallet idempotency key reused with different request")
	ErrWalletPaymentEvidence        = errors.New("wallet payment evidence requires review")
	ErrWalletPaymentProofIncomplete = errors.New("wallet payment proof is incomplete")
	ErrWalletAttemptEnded           = errors.New("wallet payment attempt has ended locally")
)

const WalletTopUpAttemptTTLSeconds int64 = 30 * 60

type WalletTopUpScope struct {
	TenantID int64
	UserID   int
}
type WalletTopUpFlow struct {
	ID                      int64  `gorm:"primaryKey"`
	TenantID                int64  `gorm:"not null;uniqueIndex:idx_wallet_flow_owner,priority:1"`
	UserID                  int    `gorm:"not null;uniqueIndex:idx_wallet_flow_owner,priority:2"`
	FlowID                  string `gorm:"type:varchar(64);not null"`
	Revision                int64  `gorm:"not null"`
	TopUpID                 *int
	LastAcknowledgedFlowID  string `gorm:"type:varchar(64);not null"`
	LastAcknowledgedTradeNo string `gorm:"type:varchar(255);not null"`
	UpdatedAt               int64  `gorm:"not null"`
}

func (WalletTopUpFlow) TableName() string { return "wallet_topup_flows" }

type WalletTopUpState struct {
	Flow                    *WalletTopUpFlow
	Order                   *TopUp
	OtherUnfinishedCount    int64
	HasRecoverableCandidate bool
}

type WalletTopUpRun struct {
	ID                 string `gorm:"type:varchar(64);primaryKey"`
	RequestHash        string `gorm:"type:varchar(64)"`
	ReportID           string `gorm:"type:varchar(64);index"`
	ScopeJSON          string `gorm:"type:text;not null"`
	ScopeDigest        string `gorm:"type:varchar(64);not null"`
	MaxOrderID         int64
	Cursor             int64
	ControlVersion     int64
	ExpiresAt          int64
	DesiredState       string  `gorm:"type:varchar(32)"`
	Status             string  `gorm:"type:varchar(32);index"`
	Owner              string  `gorm:"type:varchar(128)"`
	LastTaskID         string  `gorm:"type:varchar(64)"`
	AuthorizationRef   string  `gorm:"type:varchar(255)"`
	IdempotencyKeyHash *string `gorm:"type:varchar(64);uniqueIndex"`
	ScannedCount       int64
	CreditedCount      int64
	ClosedCount        int64
	ReviewCount        int64
	ErrorCount         int64
	CreatedAt          int64
	UpdatedAt          int64
}

func (WalletTopUpRun) TableName() string { return "wallet_topup_runs" }

// WalletPaymentSchemaAvailable fails closed before the explicit expansion. No
// request path creates tables or changes the production schema.
func WalletPaymentSchemaAvailable() bool {
	if DB == nil {
		return false
	}
	for _, value := range []any{&TopUp{}, &WalletTopUpFlow{}, &WalletPaymentReceipt{}, &WalletPaymentEvent{}, &WalletTopUpRun{}} {
		stmt := &gorm.Statement{DB: DB}
		if err := stmt.Parse(value); err != nil {
			return false
		}
		cols, err := DB.Migrator().ColumnTypes(value)
		if err != nil {
			return false
		}
		actual := make(map[string]bool, len(cols))
		for _, col := range cols {
			actual[col.Name()] = true
		}
		for _, field := range stmt.Schema.Fields {
			if field.DBName != "" && !actual[field.DBName] {
				return false
			}
		}
	}
	return true
}

// Legacy order reads and callback accounting remain usable before expansion;
// new purchases require the v18 gate. Never send absent columns to SQL.
func (t *TopUp) BeforeSave(tx *gorm.DB) error {
	if tx.Session(&gorm.Session{NewDB: true}).Migrator().HasColumn("top_ups", "wallet_lifecycle_version") {
		return nil
	}
	if t.WalletLifecycleVersion > 0 {
		return ErrWalletPaymentSchema
	}
	tx.Statement.Omits = append(tx.Statement.Omits, walletTopUpColumnNames...)
	return nil
}

var walletTopUpColumnNames = []string{
	"wallet_lifecycle_version", "currency_exponent", "credited_quota", "credit_display_amount", "credit_display_unit", "pricing_mode", "pricing_snapshot", "pricing_notice_code", "payable_minor", "request_key_hash", "request_hash", "provider_account_ref", "provider_environment", "provider_credential_version", "create_state", "create_claim_token", "create_claim_until", "provider_session_id", "provider_order_id", "checkout_payload", "provider_expires_at", "pay_deadline_at", "deadline_source", "local_ended_at", "local_end_reason", "close_requested_at", "close_reason", "closed_at", "close_evidence_ref", "provider_payment_state", "last_checked_at", "next_check_at", "check_attempts", "check_claim_token", "check_claim_until", "reason_code", "review_owner", "review_started_at", "version",
}

func walletID() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
func walletTransaction(fn func(*gorm.DB) error) error {
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		err = DB.Transaction(fn)
		if !isSQLiteBusyError(err) {
			return err
		}
		time.Sleep(time.Duration(attempt+1) * 5 * time.Millisecond)
	}
	return err
}
func walletScopeQuery(tx *gorm.DB, scope WalletTopUpScope) *gorm.DB {
	return tx.Where("tenant_id = ? AND user_id = ?", scope.TenantID, scope.UserID)
}
func validateWalletScope(scope WalletTopUpScope) error {
	if scope.TenantID < 0 || scope.UserID <= 0 {
		return ErrResellerFinanceInvalidInput
	}
	return nil
}
func lockWalletFlow(tx *gorm.DB, scope WalletTopUpScope) (*WalletTopUpFlow, error) {
	flow := &WalletTopUpFlow{TenantID: scope.TenantID, UserID: scope.UserID, UpdatedAt: common.GetTimestamp()}
	if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "tenant_id"}, {Name: "user_id"}}, DoNothing: true}).Create(flow).Error; err != nil {
		return nil, err
	}
	flow = &WalletTopUpFlow{}
	if err := walletScopeQuery(lockForUpdate(tx), scope).First(flow).Error; err != nil {
		return nil, err
	}
	return flow, nil
}
func WalletTopUpReliableTerminal(order *TopUp) bool {
	if order == nil {
		return false
	}
	if order.Status == common.TopUpStatusSuccess {
		return true
	}
	if order.Status != common.TopUpStatusExpired && order.Status != "cancelled" && order.Status != common.TopUpStatusFailed {
		return false
	}
	return order.ClosedAt > 0 && order.CloseEvidenceRef != "" && (order.ProviderPaymentState == "unpaid" || order.ProviderPaymentState == "failed" || order.CreateState == "not_created")
}

// WalletTopUpDeadline bounds the site's payment operation independently from
// the provider session. An original earlier provider deadline is still honored.
func WalletTopUpDeadline(order *TopUp) int64 {
	if order == nil || order.CreateTime <= 0 {
		return 0
	}
	deadline := int64(math.MaxInt64)
	if order.CreateTime <= math.MaxInt64-WalletTopUpAttemptTTLSeconds {
		deadline = order.CreateTime + WalletTopUpAttemptTTLSeconds
	}
	for _, earlier := range []int64{order.PayDeadlineAt, order.ProviderExpiresAt} {
		if earlier > 0 && earlier < deadline {
			deadline = earlier
		}
	}
	return deadline
}

// Local ending never proves provider closure and never blocks a trusted paid
// receipt. Old pending orders expire on the site without a destructive backfill.
func WalletTopUpLocalEnded(order *TopUp, now int64) bool {
	if order == nil || order.Status == common.TopUpStatusSuccess || order.Status == "credit_review" || order.ProviderPaymentState == "paid" || WalletTopUpReliableTerminal(order) {
		return false
	}
	deadline := WalletTopUpDeadline(order)
	return order.LocalEndedAt > 0 || deadline > 0 && deadline <= now
}

// Keep old abandoned orders visible in history while excluding them from the
// actionable unfinished count and manual historical processing.
const WalletTopUpHistoryRetainedSQL = "(COALESCE(status,'') <> 'success' AND NOT (status IN ('cancelled','expired','failed') AND COALESCE(closed_at,0) > 0 AND COALESCE(close_evidence_ref,'') <> '' AND (COALESCE(provider_payment_state,'') IN ('unpaid','failed') OR COALESCE(create_state,'') = 'not_created')))"
const walletTopUpActiveAttemptSQL = "(COALESCE(local_ended_at,0) = 0 AND COALESCE(create_time,0) > ? AND (COALESCE(pay_deadline_at,0) = 0 OR pay_deadline_at > ?) AND (COALESCE(provider_expires_at,0) = 0 OR provider_expires_at > ?))"
const WalletTopUpUnfinishedSQL = "(" + WalletTopUpHistoryRetainedSQL + " AND (status = 'credit_review' OR provider_payment_state = 'paid' OR (COALESCE(local_ended_at,0) = 0 AND COALESCE(create_time,0) <= 0) OR " + walletTopUpActiveAttemptSQL + "))"

func walletRestoreCandidate(tx *gorm.DB, scope WalletTopUpScope, trade string) (*TopUp, error) {
	q := WalletTopUpOnlyQuery(walletScopeQuery(tx.Model(&TopUp{}), scope)).Where("wallet_lifecycle_version = ? AND create_state = ? AND provider_session_id <> ? AND checkout_payload <> ? AND status = ? AND COALESCE(reason_code,'') NOT IN ?", 1, "ready", "", "", common.TopUpStatusPending, []string{"REVIEW_REQUIRED", "CREDIT_REVIEW"})
	now := common.GetTimestamp()
	q = q.Where(walletTopUpActiveAttemptSQL, now-WalletTopUpAttemptTTLSeconds, now, now)
	if trade != "" {
		q = q.Where("trade_no = ?", trade)
	}
	var order TopUp
	if err := q.Order("id DESC").First(&order).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &order, nil
}
func walletStateTx(tx *gorm.DB, scope WalletTopUpScope) (*WalletTopUpState, error) {
	state := &WalletTopUpState{}
	flow := &WalletTopUpFlow{}
	err := walletScopeQuery(tx, scope).First(flow).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if err == nil {
		state.Flow = flow
		if flow.TopUpID != nil {
			order := &TopUp{}
			if err := walletScopeQuery(tx, scope).First(order, *flow.TopUpID).Error; err != nil {
				return nil, err
			}
			state.Order = order
		}
	}
	if state.Order == nil {
		candidate, err := walletRestoreCandidate(tx, scope, "")
		if err != nil {
			return nil, err
		}
		state.HasRecoverableCandidate = candidate != nil
	}
	now := common.GetTimestamp()
	q := walletScopeQuery(tx.Model(&TopUp{}), scope).Where(WalletTopUpUnfinishedSQL, now-WalletTopUpAttemptTTLSeconds, now, now)
	q = WalletTopUpOnlyQuery(q)
	if state.Order != nil {
		q = q.Where("id <> ?", state.Order.Id)
	}
	if err := q.Count(&state.OtherUnfinishedCount).Error; err != nil {
		return nil, err
	}
	return state, nil
}
func GetWalletTopUpState(scope WalletTopUpScope) (*WalletTopUpState, error) {
	if err := validateWalletScope(scope); err != nil {
		return nil, err
	}
	if !WalletPaymentSchemaAvailable() {
		return nil, ErrWalletPaymentSchema
	}
	var state *WalletTopUpState
	err := DB.Transaction(func(tx *gorm.DB) error { var err error; state, err = walletStateTx(tx, scope); return err })
	return state, err
}
func bindWalletTopUp(tx *gorm.DB, flow *WalletTopUpFlow, order *TopUp) error {
	id, err := walletID()
	if err != nil {
		return err
	}
	changed := tx.Model(&WalletTopUpFlow{}).Where("id = ? AND revision = ? AND top_up_id IS NULL", flow.ID, flow.Revision).Updates(map[string]any{"flow_id": id, "revision": flow.Revision + 1, "top_up_id": order.Id, "updated_at": common.GetTimestamp()})
	if changed.Error != nil {
		return changed.Error
	}
	if changed.RowsAffected != 1 {
		return ErrWalletFlowChanged
	}
	flow.FlowID = id
	flow.Revision++
	flow.TopUpID = &order.Id
	return nil
}
func RestoreWalletTopUp(scope WalletTopUpScope, trade string) (*WalletTopUpState, error) {
	if err := validateWalletScope(scope); err != nil {
		return nil, err
	}
	if !WalletPaymentSchemaAvailable() {
		return nil, ErrWalletPaymentSchema
	}
	var state *WalletTopUpState
	err := walletTransaction(func(tx *gorm.DB) error {
		flow, err := lockWalletFlow(tx, scope)
		if err != nil {
			return err
		}
		if flow.TopUpID != nil {
			if trade != "" {
				var current TopUp
				if err := tx.First(&current, *flow.TopUpID).Error; err != nil {
					return err
				}
				if current.TradeNo != trade {
					return ErrWalletFlowActive
				}
			}
		} else {
			candidate, err := walletRestoreCandidate(tx, scope, trade)
			if err != nil {
				return err
			}
			if candidate != nil {
				if err := bindWalletTopUp(tx, flow, candidate); err != nil {
					return err
				}
			} else if trade != "" {
				return ErrTopUpStatusInvalid
			}
		}
		state, err = walletStateTx(tx, scope)
		return err
	})
	return state, err
}
func CreateWalletTopUp(scope WalletTopUpScope, keyHash, requestHash string, expectedRev int64, order *TopUp) (*TopUp, *WalletTopUpFlow, bool, error) {
	if err := validateWalletScope(scope); err != nil {
		return nil, nil, false, err
	}
	if !WalletPaymentSchemaAvailable() {
		return nil, nil, false, ErrWalletPaymentSchema
	}
	if order == nil || len(keyHash) != 64 || len(requestHash) != 64 || order.TradeNo == "" || order.CreditedQuota <= 0 || order.PaidMinor <= 0 || order.CheckoutPayload == "" || order.ProviderAccountRef == "" || order.ProviderEnvironment == "" {
		return nil, nil, false, ErrResellerFinanceInvalidInput
	}
	var saved *TopUp
	var current *WalletTopUpFlow
	replay := false
	err := walletTransaction(func(tx *gorm.DB) error {
		flow, err := lockWalletFlow(tx, scope)
		if err != nil {
			return err
		}
		current = flow
		var previous TopUp
		lookup := walletScopeQuery(tx, scope).Where("request_key_hash = ?", keyHash).First(&previous)
		if lookup.Error == nil {
			if previous.RequestHash != requestHash {
				return ErrWalletIdempotencyConflict
			}
			saved = &previous
			replay = true
			return nil
		}
		if !errors.Is(lookup.Error, gorm.ErrRecordNotFound) {
			return lookup.Error
		}
		if flow.TopUpID != nil {
			return ErrWalletFlowActive
		}
		if flow.Revision != expectedRev {
			return ErrWalletFlowChanged
		}
		candidate, err := walletRestoreCandidate(tx, scope, "")
		if err != nil {
			return err
		}
		if candidate != nil {
			return ErrWalletFlowActive
		}
		var user User
		if err := tx.Select("id", "tenant_id", "quota").First(&user, scope.UserID).Error; err != nil {
			return err
		}
		if user.TenantID != scope.TenantID {
			return ErrResellerFinanceInvalidInput
		}
		max, err := topUpQuotaMaxCurrent(order.CreditedQuota)
		if err != nil {
			return err
		}
		if user.Quota > max {
			return ErrTopUpQuotaLimitExceeded
		}
		if scope.TenantID > 0 && (order.PaymentProvider != PaymentProviderEpay || order.Currency != ResellerCurrencyCNY) {
			return ErrResellerFinanceInvalidInput
		}
		clone := *order
		clone.Id = 0
		clone.TenantID = scope.TenantID
		clone.UserId = scope.UserID
		clone.RequestKeyHash = &keyHash
		clone.RequestHash = requestHash
		clone.WalletLifecycleVersion = 1
		clone.Version = 1
		clone.Status = common.TopUpStatusPending
		clone.CreateState = "creating"
		clone.CreateTime = common.GetTimestamp()
		clone.PayDeadlineAt = clone.CreateTime + WalletTopUpAttemptTTLSeconds
		clone.DeadlineSource = "local_policy"
		clone.CreateClaimUntil = clone.CreateTime + 60
		clone.CreateClaimToken, err = walletID()
		if err != nil {
			return err
		}
		clone.NextCheckAt = clone.CreateTime + 60
		if err := tx.Create(&clone).Error; err != nil {
			return err
		}
		if err := bindWalletTopUp(tx, flow, &clone); err != nil {
			return err
		}
		if err := recordWalletPaymentEventTx(tx, &clone, "created", "creating", "", scope.UserID, "wallet_api", ""); err != nil {
			return err
		}
		saved = &clone
		return nil
	})
	return saved, current, replay, err
}
func FindWalletTopUpForUser(scope WalletTopUpScope, trade string) (*TopUp, error) {
	var order TopUp
	if err := validateWalletScope(scope); err != nil {
		return nil, err
	}
	err := WalletTopUpOnlyQuery(walletScopeQuery(DB.Model(&TopUp{}), scope)).Where("trade_no = ?", trade).First(&order).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrTopUpNotFound
	}
	if err != nil {
		return nil, err
	}
	return &order, nil
}
func SaveWalletTopUpSession(trade, claim, sessionID, providerOrderID, cipher string, expiresAt int64) (*TopUp, error) {
	return saveWalletTopUpSession(trade, claim, sessionID, providerOrderID, cipher, expiresAt, nil)
}
func RecoverWalletTopUpSession(trade, claim string, expectedVersion int64, sessionID, providerOrderID, cipher string, expiresAt int64) (*TopUp, error) {
	return saveWalletTopUpSession(trade, claim, sessionID, providerOrderID, cipher, expiresAt, &expectedVersion)
}
func saveWalletTopUpSession(trade, claim, sessionID, providerOrderID, cipher string, expiresAt int64, recoveryVersion *int64) (*TopUp, error) {
	if sessionID == "" || cipher == "" || claim == "" {
		return nil, ErrResellerFinanceInvalidInput
	}
	var order *TopUp
	err := walletTransaction(func(tx *gorm.DB) error {
		var err error
		order, err = lockTopUpByTradeNo(tx, trade)
		if err != nil {
			return err
		}
		if recoveryVersion == nil {
			if order.CreateClaimToken != claim {
				return ErrWalletOrderChanged
			}
		} else if order.CheckClaimToken != claim || order.Version != *recoveryVersion {
			return ErrWalletOrderChanged
		}
		if order.ProviderSessionID != "" && order.ProviderSessionID != sessionID {
			return ErrWalletPaymentEvidence
		}
		updates := map[string]any{"provider_session_id": sessionID, "provider_order_id": providerOrderID, "checkout_payload": cipher, "provider_expires_at": expiresAt, "create_state": "ready", "create_claim_token": "", "create_claim_until": 0, "version": order.Version + 1}
		if recoveryVersion != nil {
			updates["check_claim_token"] = ""
			updates["check_claim_until"] = 0
		}
		if expiresAt > 0 && expiresAt < WalletTopUpDeadline(order) {
			updates["pay_deadline_at"] = expiresAt
			updates["deadline_source"] = "provider"
		}
		if order.Status == common.TopUpStatusPending && order.CloseRequestedAt == 0 {
			updates["reason_code"] = ""
		}
		if err := tx.Model(order).Updates(updates).Error; err != nil {
			return err
		}
		return tx.First(order, order.Id).Error
	})
	return order, err
}
func MarkWalletTopUpCreateUnknown(trade, claim, reason string) (*TopUp, error) {
	var order *TopUp
	err := walletTransaction(func(tx *gorm.DB) error {
		var err error
		order, err = lockTopUpByTradeNo(tx, trade)
		if err != nil {
			return err
		}
		if order.CreateClaimToken != claim || order.CreateState == "ready" {
			return ErrWalletOrderChanged
		}
		if err := tx.Model(order).Updates(map[string]any{"create_state": "unknown", "reason_code": reason, "create_claim_token": "", "create_claim_until": 0, "next_check_at": common.GetTimestamp() + 30, "version": order.Version + 1}).Error; err != nil {
			return err
		}
		return tx.First(order, order.Id).Error
	})
	return order, err
}
func RequestWalletTopUpCancel(scope WalletTopUpScope, trade string, expectedVersion int64, reason string) (*TopUp, error) {
	if err := validateWalletScope(scope); err != nil {
		return nil, err
	}
	if expectedVersion < 0 || len(reason) > 32 {
		return nil, ErrResellerFinanceInvalidInput
	}
	var order *TopUp
	err := walletTransaction(func(tx *gorm.DB) error {
		_, err := lockWalletFlow(tx, scope)
		if err != nil {
			return err
		}
		order, err = lockTopUpByTradeNo(tx, trade)
		if err != nil {
			return err
		}
		if order.UserId != scope.UserID || order.TenantID != scope.TenantID {
			return ErrTopUpNotFound
		}

		if order.Version != expectedVersion {
			return ErrWalletOrderChanged
		}
		if WalletTopUpReliableTerminal(order) {
			return nil
		}
		if WalletTopUpDeadline(order) <= 0 || WalletTopUpLocalEnded(order, common.GetTimestamp()) {
			return ErrWalletAttemptEnded
		}
		if order.CloseRequestedAt > 0 {
			return nil
		}
		now := common.GetTimestamp()
		if err := tx.Model(order).Updates(map[string]any{"close_requested_at": now, "close_reason": reason, "reason_code": "CANCEL_REQUESTED", "next_check_at": now, "version": order.Version + 1}).Error; err != nil {
			return err
		}
		if err := recordWalletPaymentEventTx(tx, order, "cancel_requested", order.Status, reason, scope.UserID, "wallet_api", ""); err != nil {
			return err
		}
		return tx.First(order, order.Id).Error
	})
	return order, err
}
func ScheduleWalletTopUpCheck(scope WalletTopUpScope, trade string) (*TopUp, error) {
	order, err := FindWalletTopUpForUser(scope, trade)
	if err != nil {
		return nil, err
	}
	if order.Status == common.TopUpStatusSuccess {
		return order, nil
	}
	changed := DB.Model(&TopUp{}).Where("id = ? AND COALESCE(version,0) = ?", order.Id, order.Version).Updates(map[string]any{"next_check_at": common.GetTimestamp()})
	if changed.Error != nil {
		return nil, changed.Error
	}
	if changed.RowsAffected != 1 {
		return nil, ErrWalletOrderChanged
	}
	return order, nil
}

// Ending a site attempt releases no money and makes no claim about the remote
// session. Its result remains current until the exact flow is acknowledged.
func EndWalletTopUpAttempt(scope WalletTopUpScope, trade string, expectedVersion int64, reason string) (*TopUp, error) {
	if err := validateWalletScope(scope); err != nil {
		return nil, err
	}
	if expectedVersion < 0 || reason != "user_ended" && reason != "timeout" {
		return nil, ErrResellerFinanceInvalidInput
	}
	var order *TopUp
	err := walletTransaction(func(tx *gorm.DB) error {
		_, err := lockWalletFlow(tx, scope)
		if err != nil {
			return err
		}
		order, err = lockTopUpByTradeNo(tx, trade)
		if err != nil {
			return err
		}
		if order.UserId != scope.UserID || order.TenantID != scope.TenantID {
			return ErrTopUpNotFound
		}
		if order.Version != expectedVersion {
			return ErrWalletOrderChanged
		}
		if order.ProviderPaymentState == "paid" || order.Status == "credit_review" {
			return ErrTopUpStatusInvalid
		}
		if order.LocalEndedAt > 0 || WalletTopUpReliableTerminal(order) {
			return nil
		}
		now := common.GetTimestamp()
		endedAt := now
		deadline := WalletTopUpDeadline(order)
		if deadline > 0 && deadline <= now {
			reason = "timeout"
			endedAt = deadline
		} else if reason == "timeout" {
			return ErrTopUpStatusInvalid
		}
		if err := tx.Model(order).Updates(map[string]any{"local_ended_at": endedAt, "local_end_reason": reason, "version": order.Version + 1}).Error; err != nil {
			return err
		}
		if err := recordWalletPaymentEventTx(tx, order, "attempt_ended", "ended", reason, scope.UserID, "wallet_api", ""); err != nil {
			return err
		}
		return tx.First(order, order.Id).Error
	})
	return order, err
}

func AcknowledgeWalletTopUp(scope WalletTopUpScope, flowID string, revision int64, trade string) (*WalletTopUpState, error) {
	if err := validateWalletScope(scope); err != nil {
		return nil, err
	}
	if flowID == "" || trade == "" || revision <= 0 {
		return nil, ErrWalletFlowChanged
	}
	var state *WalletTopUpState
	err := walletTransaction(func(tx *gorm.DB) error {
		flow, err := lockWalletFlow(tx, scope)
		if err != nil {
			return err
		}
		if flow.TopUpID == nil {
			if flow.LastAcknowledgedFlowID != flowID || flow.LastAcknowledgedTradeNo != trade || flow.Revision != revision+1 {
				return ErrWalletFlowChanged
			}
			state, err = walletStateTx(tx, scope)
			return err
		}
		if flow.FlowID != flowID || flow.Revision != revision {
			return ErrWalletFlowChanged
		}
		order, err := lockTopUpByTradeNo(tx, trade)
		if err != nil {
			return err
		}
		if order.Id != *flow.TopUpID || order.UserId != scope.UserID || order.TenantID != scope.TenantID {
			return ErrWalletFlowChanged
		}
		if !WalletTopUpReliableTerminal(order) && !WalletTopUpLocalEnded(order, common.GetTimestamp()) {
			return ErrTopUpStatusInvalid
		}
		changed := tx.Model(&WalletTopUpFlow{}).Where("id = ? AND revision = ? AND top_up_id = ?", flow.ID, revision, order.Id).Updates(map[string]any{"top_up_id": nil, "revision": revision + 1, "flow_id": "", "last_acknowledged_flow_id": flowID, "last_acknowledged_trade_no": trade, "updated_at": common.GetTimestamp()})
		if changed.Error != nil {
			return changed.Error
		}
		if changed.RowsAffected != 1 {
			return ErrWalletFlowChanged
		}
		if err := recordWalletPaymentEventTx(tx, order, "acknowledged", order.Status, "", scope.UserID, "wallet_api", order.CloseEvidenceRef); err != nil {
			return err
		}
		state, err = walletStateTx(tx, scope)
		return err
	})
	return state, err
}

// WalletTopUpOnlyQuery excludes subscription/membership mirror rows. These
// immutable trade identities may be read without reversing their lock order.
func WalletTopUpOnlyQuery(q *gorm.DB) *gorm.DB {
	if q.Session(&gorm.Session{NewDB: true}).Migrator().HasTable(&MembershipOrder{}) {
		q = q.Where("NOT EXISTS (SELECT 1 FROM kkai_membership_orders WHERE kkai_membership_orders.trade_no = top_ups.trade_no)")
	}
	if q.Session(&gorm.Session{NewDB: true}).Migrator().HasTable(&SubscriptionOrder{}) {
		q = q.Where("NOT EXISTS (SELECT 1 FROM subscription_orders WHERE subscription_orders.trade_no = top_ups.trade_no)")
	}
	return q
}
func CreateWalletTopUpRun(run *WalletTopUpRun) error {
	if run == nil || run.ID == "" || run.ScopeJSON == "" || run.ScopeDigest == "" {
		return ErrResellerFinanceInvalidInput
	}
	if !WalletPaymentSchemaAvailable() {
		return ErrWalletPaymentSchema
	}
	if run.ControlVersion == 0 {
		run.ControlVersion = 1
	}
	run.CreatedAt = common.GetTimestamp()
	run.UpdatedAt = run.CreatedAt
	return DB.Create(run).Error
}
func UpdateWalletTopUpRun(id string, expectedVersion int64, updates map[string]any, lease ...WalletRunTaskLease) (*WalletTopUpRun, error) {
	allowed := map[string]bool{"cursor": true, "desired_state": true, "status": true, "scanned_count": true, "credited_count": true, "closed_count": true, "review_count": true, "error_count": true, "last_task_id": true}
	data := map[string]any{"control_version": expectedVersion + 1, "updated_at": common.GetTimestamp()}
	for key, value := range updates {
		if !allowed[key] {
			return nil, ErrResellerFinanceInvalidInput
		}
		data[key] = value
	}
	if len(lease) > 1 || len(lease) == 1 && (lease[0].TaskID == "" || lease[0].Runner == "") {
		return nil, ErrResellerFinanceInvalidInput
	}
	var run WalletTopUpRun
	err := walletTransaction(func(tx *gorm.DB) error {
		q := tx.Model(&WalletTopUpRun{}).Where("id = ? AND control_version = ? AND status NOT IN ?", id, expectedVersion, []string{"report_only", "completed", "failed"})
		if len(lease) == 1 {
			desired := "running"
			if updates["status"] == "paused" {
				desired = "paused"
			}
			q = q.Where("desired_state = ?", desired).Where("EXISTS (SELECT 1 FROM system_task_locks WHERE type = ? AND task_id = ? AND locked_by = ? AND locked_until > ?)", "wallet_payment_history", lease[0].TaskID, lease[0].Runner, common.GetTimestamp())
		}
		result := q.Updates(data)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrWalletOrderChanged
		}
		return lockForUpdate(tx).First(&run, "id = ?", id).Error
	})
	return &run, err
}

// NormalizeWalletPaymentIdentity deliberately does not case-fold provider
// transaction IDs or merchant accounts, which can be case-sensitive.
func NormalizeWalletPaymentIdentity(value string) string { return strings.TrimSpace(value) }

func FindWalletTopUpByRequestKey(scope WalletTopUpScope, keyHash string) (*TopUp, error) {
	if err := validateWalletScope(scope); err != nil {
		return nil, err
	}
	if !WalletPaymentSchemaAvailable() {
		return nil, ErrWalletPaymentSchema
	}
	var order TopUp
	err := walletScopeQuery(DB, scope).Where("request_key_hash = ?", keyHash).First(&order).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrTopUpNotFound
	}
	if err != nil {
		return nil, err
	}
	return &order, nil
}

func GetWalletTopUpRun(id string) (*WalletTopUpRun, error) {
	var run WalletTopUpRun
	err := DB.First(&run, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &run, nil
}
func FindWalletTopUpRunByKey(hash string) (*WalletTopUpRun, error) {
	var run WalletTopUpRun
	err := DB.Where("idempotency_key_hash = ?", hash).First(&run).Error
	if err != nil {
		return nil, err
	}
	return &run, nil
}

// MarkWalletTopUpCreateNotStarted is only for a proven local failure before
// any request could have reached the provider; network errors remain unknown.
func MarkWalletTopUpCreateNotStarted(trade, claim, reason string) (*TopUp, error) {
	var order *TopUp
	err := walletTransaction(func(tx *gorm.DB) error {
		var err error
		order, err = lockTopUpByTradeNo(tx, trade)
		if err != nil {
			return err
		}
		if claim == "" || order.CreateClaimToken != claim || order.CreateState != "creating" || order.ProviderSessionID != "" || order.Status != common.TopUpStatusPending {
			return ErrWalletOrderChanged
		}
		now := common.GetTimestamp()
		if err := tx.Model(order).Updates(map[string]any{"create_state": "not_created", "status": common.TopUpStatusFailed, "provider_payment_state": "failed", "reason_code": reason, "closed_at": now, "close_evidence_ref": "local:not-started:" + trade, "create_claim_token": "", "create_claim_until": 0, "next_check_at": 0, "version": order.Version + 1}).Error; err != nil {
			return err
		}
		if err := recordWalletPaymentEventTx(tx, order, "creation_not_started", common.TopUpStatusFailed, reason, 0, "local_creation", "local:not-started:"+trade); err != nil {
			return err
		}
		return tx.First(order, order.Id).Error
	})
	return order, err
}
