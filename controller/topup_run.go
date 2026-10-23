package controller

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func walletRunDTO(run *model.WalletTopUpRun) gin.H {
	owner, _ := strconv.Atoi(run.Owner)
	return gin.H{"run_id": run.ID, "scope_digest": run.ScopeDigest, "control_version": strconv.FormatInt(run.ControlVersion, 10), "status": run.Status, "desired_state": run.DesiredState, "cursor": strconv.FormatInt(run.Cursor, 10), "counts": gin.H{"reviewed": run.ScannedCount, "credited": run.CreditedCount, "closed": run.ClosedCount, "review_required": run.ReviewCount, "retry_failed": run.ErrorCount}, "last_task_id": run.LastTaskID, "owner_id": owner, "updated_at": run.UpdatedAt}
}
func walletRunError(c *gin.Context, err error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		walletError(c, 404, "RUN_NOT_FOUND", nil)
		return
	}
	if errors.Is(err, model.ErrWalletOrderChanged) {
		walletError(c, 409, "RUN_CHANGED", nil)
		return
	}
	walletError(c, 503, "WALLET_STATE_UNAVAILABLE", nil)
}
func ReportWalletPaymentRun(c *gin.Context) {
	if !walletSchemaReady(c) {
		return
	}
	var req struct {
		Scope *struct {
			PaymentProvider string `json:"payment_provider"`
			AccountRef      string `json:"provider_account_ref"`
			Environment     string `json:"provider_environment"`
			TenantID        *int64 `json:"tenant_id"`
			CreatedBefore   int64  `json:"created_before"`
			CreatedAfter    int64  `json:"created_after"`
		} `json:"scope"`
		BatchSize int `json:"batch_size"`
		Rate      int `json:"max_queries_per_second"`
	}
	if !walletBind(c, &req) {
		return
	}
	if req.Scope == nil || req.Scope.TenantID == nil || *req.Scope.TenantID < 0 || req.Scope.CreatedBefore <= 0 || req.Scope.CreatedBefore > time.Now().Unix() || req.Scope.CreatedAfter < 0 || req.Scope.CreatedAfter >= req.Scope.CreatedBefore || req.BatchSize < 1 || req.BatchSize > 100 || req.Rate < 1 || req.Rate > 10 {
		walletError(c, 400, "VALIDATION_FAILED", nil)
		return
	}
	s := req.Scope
	s.AccountRef = strings.TrimSpace(s.AccountRef)
	if s.AccountRef == "" || len(s.AccountRef) > 128 || (s.Environment != "prod" && s.Environment != "test" && s.Environment != "unknown") {
		walletError(c, 400, "VALIDATION_FAILED", nil)
		return
	}
	switch s.PaymentProvider {
	case model.PaymentProviderEpay, model.PaymentProviderStripe, model.PaymentProviderCreem, model.PaymentProviderWaffo, model.PaymentProviderWaffoPancake, "unknown":
	default:
		walletError(c, 400, "VALIDATION_FAILED", nil)
		return
	}
	selection := model.WalletRunSelection{Scope: model.WalletRunScope{PaymentProvider: s.PaymentProvider, AccountRef: s.AccountRef, Environment: s.Environment, TenantID: *s.TenantID, CreatedBefore: s.CreatedBefore, CreatedAfter: s.CreatedAfter}, BatchSize: req.BatchSize, QueriesPerSecond: req.Rate}
	maxID, total, missing, err := model.ReportWalletTopUpRun(selection.Scope)
	if err != nil {
		walletRunError(c, err)
		return
	}
	data, err := common.Marshal(selection)
	if err != nil {
		walletRunError(c, err)
		return
	}
	id, err := common.GenerateRandomCharsKey(32)
	if err != nil {
		walletRunError(c, err)
		return
	}
	report := &model.WalletTopUpRun{ID: "wallet_report_" + id, ScopeJSON: string(data), ScopeDigest: walletHash(selection), MaxOrderID: maxID, Status: "report_only", DesiredState: "report_only", Owner: strconv.Itoa(c.GetInt("id")), ExpiresAt: time.Now().Add(15 * time.Minute).Unix()}
	if err = model.CreateWalletTopUpRun(report); err != nil {
		walletRunError(c, err)
		return
	}
	gaps := []string{}
	if missing > 0 {
		gaps = append(gaps, "ORIGINAL_PAYMENT_SNAPSHOT_MISSING")
	}
	if s.PaymentProvider != model.PaymentProviderStripe {
		gaps = append(gaps, "RELIABLE_CLOSE_UNSUPPORTED")
	}
	walletSuccess(c, 200, gin.H{"report_id": report.ID, "scope_digest": report.ScopeDigest, "max_order_id": strconv.FormatInt(maxID, 10), "counts": gin.H{"total": total, "missing_snapshot": missing}, "capability_gaps": gaps, "expires_at": report.ExpiresAt})
}
func StartWalletPaymentRun(c *gin.Context) {
	if !walletSchemaReady(c) {
		return
	}
	var req struct {
		ReportID         string `json:"report_id"`
		ScopeDigest      string `json:"scope_digest"`
		AuthorizationRef string `json:"authorization_ref"`
		Reason           string `json:"reason"`
	}
	if !walletBind(c, &req) {
		return
	}
	key := c.GetHeader("Idempotency-Key")
	if !walletKeyPattern.MatchString(key) || strings.TrimSpace(req.AuthorizationRef) == "" || len(req.AuthorizationRef) > 255 || strings.TrimSpace(req.Reason) == "" || len(req.Reason) > 500 {
		walletError(c, 400, "VALIDATION_FAILED", nil)
		return
	}
	keyHash := walletHash([]string{strconv.Itoa(c.GetInt("id")), key})
	requestHash := walletHash(req)
	run, err := model.FindWalletTopUpRunByKey(keyHash)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		walletRunError(c, err)
		return
	}
	if run != nil && err == nil {
		if run.RequestHash != requestHash {
			walletError(c, 409, "IDEMPOTENCY_CONFLICT", nil)
			return
		}
		walletSuccess(c, 200, gin.H{"run": walletRunDTO(run), "task_id": run.LastTaskID})
		return
	}
	report, err := model.GetWalletTopUpRun(req.ReportID)
	if err != nil {
		walletRunError(c, err)
		return
	}
	if report.Status != "report_only" || report.ExpiresAt <= time.Now().Unix() || report.ScopeDigest != req.ScopeDigest || report.Owner != strconv.Itoa(c.GetInt("id")) {
		walletError(c, 409, "REPORT_EXPIRED_OR_CHANGED", nil)
		return
	}
	id, err := common.GenerateRandomCharsKey(32)
	if err != nil {
		walletRunError(c, err)
		return
	}
	run = &model.WalletTopUpRun{ID: "wallet_run_" + id, ReportID: report.ID, ScopeJSON: report.ScopeJSON, ScopeDigest: report.ScopeDigest, MaxOrderID: report.MaxOrderID, Status: "queued", DesiredState: "running", Owner: report.Owner, AuthorizationRef: req.AuthorizationRef, IdempotencyKeyHash: &keyHash, RequestHash: requestHash}
	if err = model.CreateWalletTopUpRun(run); err != nil {
		previous, e := model.FindWalletTopUpRunByKey(keyHash)
		if e != nil || previous == nil {
			walletRunError(c, err)
			return
		}
		if previous.RequestHash != requestHash {
			walletError(c, 409, "IDEMPOTENCY_CONFLICT", nil)
			return
		}
		run = previous
	}
	task, _, err := service.EnqueueSystemTask(service.WalletPaymentHistoryTaskType, nil)
	if err != nil {
		walletRunError(c, err)
		return
	}
	if err = model.SetWalletRunTask(run.ID, task.TaskID); err != nil {
		walletRunError(c, err)
		return
	}
	run.LastTaskID = task.TaskID
	model.RecordOperationAuditLog(c.GetInt("id"), c.GetInt("role"), "wallet history reconciliation authorized", c.ClientIP(), "wallet_history_start", map[string]any{"run_id": run.ID, "scope_digest": run.ScopeDigest, "authorization_ref": req.AuthorizationRef, "reason": req.Reason}, nil, nil, c)
	walletSuccess(c, http.StatusAccepted, gin.H{"run": walletRunDTO(run), "task_id": task.TaskID})
}
func GetWalletPaymentRun(c *gin.Context) {
	if !walletSchemaReady(c) {
		return
	}
	run, err := model.GetWalletTopUpRun(c.Param("run_id"))
	if err != nil {
		walletRunError(c, err)
		return
	}
	walletSuccess(c, 200, walletRunDTO(run))
}
func PauseWalletPaymentRun(c *gin.Context)  { controlWalletPaymentRun(c, false) }
func ResumeWalletPaymentRun(c *gin.Context) { controlWalletPaymentRun(c, true) }
func controlWalletPaymentRun(c *gin.Context, resume bool) {
	if !walletSchemaReady(c) {
		return
	}
	var req struct {
		ExpectedVersion  string `json:"expected_control_version"`
		ScopeDigest      string `json:"scope_digest"`
		AuthorizationRef string `json:"authorization_ref"`
		Reason           string `json:"reason"`
	}
	if !walletBind(c, &req) {
		return
	}
	version, err := strconv.ParseInt(req.ExpectedVersion, 10, 64)
	if err != nil || version < 1 {
		walletError(c, 400, "VALIDATION_FAILED", nil)
		return
	}
	run, err := model.GetWalletTopUpRun(c.Param("run_id"))
	if err != nil {
		walletRunError(c, err)
		return
	}
	if run.Status == "report_only" || run.Status == "completed" || run.Status == "failed" {
		walletError(c, 409, "RUN_CHANGED", nil)
		return
	}
	if resume {
		if run.ScopeDigest != req.ScopeDigest || run.AuthorizationRef != req.AuthorizationRef || req.AuthorizationRef == "" {
			walletError(c, 409, "RUN_CHANGED", nil)
			return
		}
		if run.DesiredState == "running" && run.ControlVersion == version {
			walletSuccess(c, 200, walletRunDTO(run))
			return
		}
	} else {
		if strings.TrimSpace(req.Reason) == "" || len(req.Reason) > 500 {
			walletError(c, 400, "VALIDATION_FAILED", nil)
			return
		}
		if run.DesiredState == "paused" && run.ControlVersion == version {
			walletSuccess(c, 200, walletRunDTO(run))
			return
		}
	}
	updates := map[string]any{"desired_state": "paused", "status": "pausing"}
	if run.Status == "queued" {
		updates["status"] = "paused"
	}
	if resume {
		if run.Status != "paused" {
			walletError(c, 409, "RUN_CHANGED", nil)
			return
		}
		updates = map[string]any{"desired_state": "running", "status": "queued"}
	}
	run, err = model.UpdateWalletTopUpRun(run.ID, version, updates)
	if err != nil {
		walletRunError(c, err)
		return
	}
	if resume {
		if _, _, err = service.EnqueueSystemTask(service.WalletPaymentHistoryTaskType, nil); err != nil {
			walletRunError(c, err)
			return
		}
	}
	model.RecordOperationAuditLog(c.GetInt("id"), c.GetInt("role"), "wallet history reconciliation control", c.ClientIP(), "wallet_history_control", map[string]any{"run_id": run.ID, "resume": resume, "reason": req.Reason, "authorization_ref": req.AuthorizationRef}, nil, nil, c)
	walletSuccess(c, 200, walletRunDTO(run))
}
