package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"gorm.io/gorm"
)

func (s InvoiceService) VerifyPayment(ctx context.Context, scope InvoiceScope, input InvoiceOrderRef) error {
	return s.verifyPayment(ctx, scope, input, http.DefaultClient)
}

func (s InvoiceService) verifyPayment(ctx context.Context, scope InvoiceScope, input InvoiceOrderRef, client *http.Client) error {
	if err := invoiceValidateScope(scope); err != nil {
		return err
	}
	if input.SourceType != "topup" || input.OrderID == "" || len(input.OrderID) > 255 || strings.TrimSpace(input.OrderID) != input.OrderID {
		return invoiceError(400, "VALIDATION_FAILED")
	}
	db := s.database(ctx)
	var order model.TopUp
	if err := db.Where("tenant_id = ? AND user_id = ? AND trade_no = ?", scope.TenantID, scope.UserID, input.OrderID).First(&order).Error; err != nil {
		return err
	}
	if order.PaymentProvider != model.PaymentProviderEpay || order.Status != common.TopUpStatusSuccess {
		return invoiceError(409, "PAYMENT_VERIFICATION_FAILED")
	}
	// Resolve aliases and existing anomalies before sending any merchant request.
	rows, err := s.candidateRowsFor(ctx, scope, db, false, []InvoiceOrderRef{input}, true)
	if err != nil {
		return err
	}
	found := false
	for _, row := range rows {
		if row.SourceType == input.SourceType && row.OrderID == input.OrderID {
			found = true
		}
	}
	if !found {
		return invoiceError(409, "PAYMENT_VERIFICATION_FAILED")
	}
	var proof model.InvoicePaymentReceipt
	err = db.Where("top_up_id = ?", order.Id).First(&proof).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if err == nil && proof.Status != "verified" {
		return invoiceError(409, "PAYMENT_EXCEPTION")
	}
	// HTTPS authenticates the configured single merchant gateway; credentials and
	// raw provider responses must never reach logs, error messages or the browser.
	receipt, paidAt, err := queryInvoiceEpayPayment(ctx, client, operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey, order)
	if err != nil {
		return err
	}
	err = model.ReconcileInvoiceTopUpPayment(ctx, db, scope.TenantID, scope.UserID, order.TradeNo, receipt, paidAt)
	if errors.Is(err, model.ErrResellerFinanceInvalidInput) {
		return invoiceError(409, "PAYMENT_VERIFICATION_FAILED")
	}
	if errors.Is(err, model.ErrInvoicePaymentException) {
		return invoiceError(409, "PAYMENT_EXCEPTION")
	}
	return err
}

// queryInvoiceEpayPayment uses the merchant order-query protocol documented at
// https://zpayz.cn/doc.html#d5. Like the signed CNY checkout callback, status=1
// establishes successful payment. External refunds still require the existing
// financial reconciliation/hold workflow; this protocol has no refund ledger.
func queryInvoiceEpayPayment(ctx context.Context, client *http.Client, address, merchantID, key string, order model.TopUp) (model.TopUpPaymentReceipt, int64, error) {
	var receipt model.TopUpPaymentReceipt
	unavailable := invoiceError(503, "PAYMENT_VERIFICATION_UNAVAILABLE")
	failed := invoiceError(409, "PAYMENT_VERIFICATION_FAILED")
	base, err := url.Parse(address)
	if err != nil || base.Scheme != "https" || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || merchantID == "" || key == "" {
		return receipt, 0, unavailable
	}
	base.Path = path.Join(base.Path, "api.php")
	base.RawPath = ""
	base.RawQuery = url.Values{"act": {"order"}, "pid": {merchantID}, "key": {key}, "out_trade_no": {order.TradeNo}}.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, base.String(), nil)
	if err != nil {
		return receipt, 0, unavailable
	}
	request.Header.Set("Accept", "application/json")
	bounded := *client
	bounded.Timeout = 10 * time.Second
	bounded.Jar = nil
	bounded.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := bounded.Do(request)
	if err != nil {
		return receipt, 0, unavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return receipt, 0, unavailable
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, (64<<10)+1))
	if err != nil || len(body) > 64<<10 {
		return receipt, 0, unavailable
	}
	var data struct {
		Code         common.RawMessage `json:"code"`
		Status       common.RawMessage `json:"status"`
		MerchantID   common.RawMessage `json:"pid"`
		TradeNo      string            `json:"trade_no"`
		OrderNo      string            `json:"out_trade_no"`
		Money        string            `json:"money"`
		Currency     string            `json:"currency"`
		EndTime      string            `json:"endtime"`
		RefundMoney  common.RawMessage `json:"refund_money"`
		RefundAmount common.RawMessage `json:"refund_amount"`
		RefundStatus common.RawMessage `json:"refund_status"`
	}
	if common.Unmarshal(body, &data) != nil {
		return receipt, 0, unavailable
	}
	if common.JsonRawMessageToString(data.Code) != "1" || common.JsonRawMessageToString(data.Status) != "1" || common.JsonRawMessageToString(data.MerchantID) != merchantID || data.OrderNo != order.TradeNo || data.TradeNo == "" || (data.Currency != "" && data.Currency != "CNY") {
		return receipt, 0, failed
	}
	// Reject explicit refund signals if a compatible gateway supplies them.
	for _, value := range []common.RawMessage{data.RefundMoney, data.RefundAmount, data.RefundStatus} {
		if len(value) == 0 || string(value) == "null" {
			continue
		}
		minor, parseErr := model.ParseCNYMinor(common.JsonRawMessageToString(value))
		if parseErr != nil || minor != 0 {
			return receipt, 0, failed
		}
	}
	if strings.ContainsAny(data.Money, "eE+-") {
		return receipt, 0, failed
	}
	minor, err := model.ParseCNYMinor(data.Money)
	if err != nil || minor <= 0 {
		return receipt, 0, failed
	}
	paidAt := order.CompleteTime
	if paidAt <= 0 {
		// This CNY gateway returns its local Beijing time without an offset.
		completed, parseErr := time.ParseInLocation("2006-01-02 15:04:05", data.EndTime, time.FixedZone("Asia/Shanghai", 8*60*60))
		if parseErr != nil {
			return receipt, 0, failed
		}
		paidAt = completed.Unix()
	}
	if paidAt <= 0 || paidAt > time.Now().Unix() || (order.CreateTime > 0 && paidAt < order.CreateTime) {
		return receipt, 0, failed
	}
	receipt = model.TopUpPaymentReceipt{ProviderReceiptID: data.TradeNo, AmountMinor: minor, Currency: "CNY"}
	return receipt, paidAt, nil
}
