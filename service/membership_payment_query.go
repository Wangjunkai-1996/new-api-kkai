package service

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

type membershipEpayQuery struct {
	Paid            bool
	ReceiptID       string
	AmountMinor     int64
	Currency        string
	ProviderPaidAt  int64
	ValidationError string
}

// queryMembershipEpay uses only the authenticated original merchant query.
// Query payment-method aliases are not callback signatures or order identity.
func queryMembershipEpay(ctx context.Context, client *http.Client, order *model.MembershipOrder, snapshot *MembershipEpaySnapshot) (membershipEpayQuery, error) {
	var result membershipEpayQuery
	if order == nil || snapshot == nil || order.Provider != model.PaymentProviderEpay || order.Currency != "CNY" || snapshot.MerchantID == "" || snapshot.APIKey == "" {
		return result, ErrMembershipPaymentEvidence
	}
	base, err := url.Parse(snapshot.APIURL)
	if err != nil || base.Scheme != "https" || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return result, ErrMembershipPaymentUnavailable
	}
	base.Path = path.Join(base.Path, "api.php")
	base.RawPath = ""
	base.RawQuery = url.Values{"act": {"order"}, "pid": {snapshot.MerchantID}, "key": {snapshot.APIKey}, "out_trade_no": {order.TradeNo}}.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, base.String(), nil)
	if err != nil {
		return result, ErrMembershipPaymentUnavailable
	}
	request.Header.Set("Accept", "application/json")
	if client == nil {
		client = http.DefaultClient
	}
	bounded := *client
	bounded.Timeout = 10 * time.Second
	bounded.Jar = nil
	bounded.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := bounded.Do(request)
	if err != nil {
		return result, ErrMembershipPaymentUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return result, ErrMembershipPaymentUnavailable
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, (64<<10)+1))
	if err != nil || len(body) > 64<<10 {
		return result, ErrMembershipPaymentUnavailable
	}
	if common.GetJsonType(body) == "string" {
		var inner string
		if common.Unmarshal(body, &inner) != nil {
			return result, ErrMembershipPaymentUnavailable
		}
		body = []byte(inner)
	}
	if common.GetJsonType(body) != "object" {
		return result, ErrMembershipPaymentUnavailable
	}
	var unique map[string]common.RawMessage
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
	// Allow provider extensions while rejecting duplicate decoded keys at every
	// depth, including escaped names, and trailing JSON values.
	if common.UnmarshalStrict(body, &unique) != nil || common.Unmarshal(body, &data) != nil {
		return result, ErrMembershipPaymentUnavailable
	}
	// A merchant may not have created the checkout yet, or may temporarily
	// reject queries. Neither response proves that this order can never be paid.
	if common.JsonRawMessageToString(data.Code) != "1" {
		return result, ErrMembershipPaymentUnavailable
	}
	merchantID := common.JsonRawMessageToString(data.MerchantID)
	merchantType := common.GetJsonType(data.MerchantID)
	if (merchantType != "string" && merchantType != "number") || merchantID == "" || data.OrderNo == "" {
		return result, ErrMembershipPaymentUnavailable
	}
	if merchantID != snapshot.MerchantID || data.OrderNo != order.TradeNo {
		return result, ErrMembershipPaymentEvidence
	}
	status := common.JsonRawMessageToString(data.Status)
	if status == "0" {
		return result, nil
	}
	if status != "1" || strings.TrimSpace(data.TradeNo) != data.TradeNo || data.TradeNo == "" || len(data.TradeNo) > 128 {
		return result, ErrMembershipPaymentUnavailable
	}
	if strings.TrimSpace(data.Money) != data.Money || strings.ContainsAny(data.Money, "eE+-") {
		return result, ErrMembershipPaymentUnavailable
	}
	minor, err := model.ParseCNYMinor(data.Money)
	if err != nil || minor <= 0 {
		return result, ErrMembershipPaymentUnavailable
	}
	result = membershipEpayQuery{Paid: true, ReceiptID: data.TradeNo, AmountMinor: minor, Currency: "CNY"}
	if data.Currency != "" {
		result.Currency = data.Currency
	}
	if result.Currency != "CNY" || minor != order.AmountMinor {
		result.ValidationError = "payment amount or currency mismatch"
	}
	for _, value := range []common.RawMessage{data.RefundMoney, data.RefundAmount, data.RefundStatus} {
		if len(value) == 0 || string(value) == "null" {
			continue
		}
		amount, parseErr := model.ParseCNYMinor(common.JsonRawMessageToString(value))
		if parseErr != nil || amount != 0 {
			result.ValidationError = "provider refund signal requires review"
		}
	}
	completed, err := time.ParseInLocation("2006-01-02 15:04:05", data.EndTime, time.FixedZone("Asia/Shanghai", 8*60*60))
	if err != nil || completed.Unix() <= 0 || completed.Unix() < order.CreatedAt || completed.Unix() > time.Now().Unix() {
		if result.ValidationError == "" {
			result.ValidationError = "provider payment time unavailable"
		}
	} else {
		result.ProviderPaidAt = completed.Unix()
	}
	return result, nil
}
