package topuprecovery

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const maximumProviderResponseBytes = 64 * 1024

var ErrInvalidProviderEvidence = errors.New("invalid EPay success evidence")

type EPayProvider struct {
	client    *http.Client
	endpoint  *url.URL
	partnerID string
	key       string
}

type epayOrderResponse struct {
	Code           int    `json:"code"`
	Status         int    `json:"status"`
	TradeNo        string `json:"trade_no"`
	ServiceTradeNo string `json:"out_trade_no"`
	PaymentType    string `json:"type"`
	EndTime        string `json:"endtime"`
}

type optionRow struct {
	Key   string `gorm:"column:key"`
	Value string `gorm:"column:value"`
}

func (optionRow) TableName() string {
	return "options"
}

func NewEPayProviderFromDatabase(db *gorm.DB, client *http.Client) (*EPayProvider, error) {
	if db == nil {
		return nil, ErrInvalidProviderEvidence
	}
	address, err := loadRequiredOption(db, "PayAddress")
	if err != nil {
		return nil, err
	}
	partnerID, err := loadRequiredOption(db, "EpayId")
	if err != nil {
		return nil, err
	}
	key, err := loadRequiredOption(db, "EpayKey")
	if err != nil {
		return nil, err
	}
	endpoint, err := url.Parse(address)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" {
		return nil, fmt.Errorf("%w: PayAddress must be HTTPS", ErrInvalidProviderEvidence)
	}
	endpoint.Path = path.Join(endpoint.Path, "api.php")
	endpoint.RawQuery = ""
	endpoint.Fragment = ""
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	isolatedClient := *client
	isolatedClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &EPayProvider{
		client:    &isolatedClient,
		endpoint:  endpoint,
		partnerID: partnerID,
		key:       key,
	}, nil
}

func (provider *EPayProvider) Lookup(ctx context.Context, serviceTradeNo string) (ProviderOrder, error) {
	if provider == nil || strings.TrimSpace(serviceTradeNo) == "" {
		return ProviderOrder{}, ErrInvalidProviderEvidence
	}
	requestURL := *provider.endpoint
	query := requestURL.Query()
	query.Set("act", "order")
	query.Set("pid", provider.partnerID)
	query.Set("key", provider.key)
	query.Set("out_trade_no", serviceTradeNo)
	requestURL.RawQuery = query.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return ProviderOrder{}, err
	}
	response, err := provider.client.Do(request)
	if err != nil {
		return ProviderOrder{}, fmt.Errorf("%w: request failed", ErrInvalidProviderEvidence)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return ProviderOrder{}, fmt.Errorf("%w: HTTP status %d", ErrInvalidProviderEvidence, response.StatusCode)
	}

	raw, err := io.ReadAll(io.LimitReader(response.Body, maximumProviderResponseBytes+1))
	if err != nil || len(raw) > maximumProviderResponseBytes {
		return ProviderOrder{}, fmt.Errorf("%w: malformed response", ErrInvalidProviderEvidence)
	}
	var payload epayOrderResponse
	if err := common.Unmarshal(raw, &payload); err != nil {
		return ProviderOrder{}, fmt.Errorf("%w: malformed response", ErrInvalidProviderEvidence)
	}
	if payload.Code != 1 || payload.Status != 1 || payload.ServiceTradeNo != serviceTradeNo ||
		strings.TrimSpace(payload.TradeNo) == "" || strings.TrimSpace(payload.EndTime) == "" {
		return ProviderOrder{}, ErrInvalidProviderEvidence
	}
	completedAt, err := time.ParseInLocation(
		"2006-01-02 15:04:05",
		payload.EndTime,
		time.FixedZone("Asia/Shanghai", 8*60*60),
	)
	if err != nil {
		return ProviderOrder{}, fmt.Errorf("%w: invalid endtime", ErrInvalidProviderEvidence)
	}
	return ProviderOrder{
		Code:           payload.Code,
		Status:         payload.Status,
		TradeNo:        payload.TradeNo,
		ServiceTradeNo: payload.ServiceTradeNo,
		PaymentType:    payload.PaymentType,
		EndTime:        payload.EndTime,
		CompletedAt:    completedAt.Unix(),
	}, nil
}

func loadRequiredOption(db *gorm.DB, name string) (string, error) {
	value, found, err := loadOptionalOption(db, name)
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("required option %s is missing", name)
	}
	return value, nil
}

func loadOptionalOption(db *gorm.DB, name string) (string, bool, error) {
	if db == nil || strings.TrimSpace(name) == "" {
		return "", false, fmt.Errorf("load option: invalid input")
	}
	row := optionRow{}
	if err := db.Where(map[string]any{"key": name}).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("load option %s: %w", name, err)
	}
	value := strings.TrimSpace(row.Value)
	if value == "" {
		return "", false, fmt.Errorf("option %s is empty", name)
	}
	return value, true, nil
}
