package model

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/shopspring/decimal"

	"gorm.io/gorm"
)

type TopUp struct {
	Id                        int     `json:"id"`
	TenantID                  int64   `json:"tenant_id" gorm:"not null;default:0;index;uniqueIndex:idx_wallet_topup_request,priority:1"`
	PaidMinor                 int64   `json:"paid_minor" gorm:"type:bigint;not null;default:0"`
	Currency                  string  `json:"currency" gorm:"type:varchar(8);not null;default:''"`
	UserId                    int     `json:"user_id" gorm:"index;uniqueIndex:idx_wallet_topup_request,priority:2"`
	Amount                    int64   `json:"amount"`
	Money                     float64 `json:"money"`
	TradeNo                   string  `json:"trade_no" gorm:"unique;type:varchar(255);index"`
	PaymentMethod             string  `json:"payment_method" gorm:"type:varchar(50)"`
	PaymentProvider           string  `json:"payment_provider" gorm:"type:varchar(50);default:''"`
	CreateTime                int64   `json:"create_time"`
	CompleteTime              int64   `json:"complete_time"`
	Status                    string  `json:"status"`
	WalletLifecycleVersion    int     `json:"-"`
	CurrencyExponent          int     `json:"-"`
	CreditedQuota             int64   `json:"-"`
	CreditDisplayAmount       string  `json:"-" gorm:"type:varchar(64)"`
	CreditDisplayUnit         string  `json:"-" gorm:"type:varchar(32)"`
	PricingMode               string  `json:"-" gorm:"type:varchar(32)"`
	PricingSnapshot           string  `json:"-" gorm:"type:text"`
	PricingNoticeCode         string  `json:"-" gorm:"type:varchar(64)"`
	PayableMinor              *int64  `json:"-"`
	RequestKeyHash            *string `json:"-" gorm:"type:varchar(64);uniqueIndex:idx_wallet_topup_request,priority:3"`
	RequestHash               string  `json:"-" gorm:"type:varchar(64)"`
	ProviderAccountRef        string  `json:"-" gorm:"type:varchar(128)"`
	ProviderEnvironment       string  `json:"-" gorm:"type:varchar(32)"`
	ProviderCredentialVersion string  `json:"-" gorm:"type:varchar(128)"`
	CreateState               string  `json:"-" gorm:"type:varchar(32)"`
	CreateClaimToken          string  `json:"-" gorm:"type:varchar(64)"`
	CreateClaimUntil          int64   `json:"-"`
	ProviderSessionID         string  `json:"-" gorm:"type:varchar(255)"`
	ProviderOrderID           string  `json:"-" gorm:"type:varchar(255)"`
	CheckoutPayload           string  `json:"-" gorm:"type:text"`
	ProviderExpiresAt         int64   `json:"-"`
	PayDeadlineAt             int64   `json:"-"`
	DeadlineSource            string  `json:"-" gorm:"type:varchar(32)"`
	LocalEndedAt              int64   `json:"-"`
	LocalEndReason            string  `json:"-" gorm:"type:varchar(32)"`
	CloseRequestedAt          int64   `json:"-"`
	CloseReason               string  `json:"-" gorm:"type:varchar(32)"`
	ClosedAt                  int64   `json:"-"`
	CloseEvidenceRef          string  `json:"-" gorm:"type:varchar(255)"`
	ProviderPaymentState      string  `json:"-" gorm:"type:varchar(32)"`
	LastCheckedAt             int64   `json:"-"`
	NextCheckAt               int64   `json:"-" gorm:"index:idx_wallet_topup_due"`
	CheckAttempts             int     `json:"-"`
	CheckClaimToken           string  `json:"-" gorm:"type:varchar(64)"`
	CheckClaimUntil           int64   `json:"-"`
	ReasonCode                string  `json:"-" gorm:"type:varchar(64)"`
	ReviewOwner               string  `json:"-" gorm:"type:varchar(128)"`
	ReviewStartedAt           int64   `json:"-"`
	Version                   int64   `json:"-"`
}

const (
	PaymentMethodStripe       = "stripe"
	PaymentMethodCreem        = "creem"
	PaymentMethodWaffo        = "waffo"
	PaymentMethodWaffoPancake = "waffo_pancake"
	PaymentMethodBalance      = "balance"
)

// SetCNYPaymentSnapshot accepts the exact amount sent to Epay. Never derive
// tenant cash from TopUp.Money, which is a legacy floating-point display field.
func (topUp *TopUp) SetCNYPaymentSnapshot(amount string) error {
	minor, err := ParseCNYMinor(amount)
	if err != nil || minor <= 0 {
		return ErrResellerFinanceInvalidInput
	}
	topUp.PaidMinor, topUp.Currency = minor, ResellerCurrencyCNY
	return nil
}

func ParseCNYMinor(amount string) (int64, error) {
	if len(amount) == 0 || len(amount) > 24 || strings.TrimSpace(amount) != amount {
		return 0, ErrResellerFinanceInvalidInput
	}
	value, err := decimal.NewFromString(amount)
	if err != nil || value.IsNegative() || value.Exponent() < -2 || value.Exponent() > 18 {
		return 0, ErrResellerFinanceInvalidInput
	}
	value = value.Mul(decimal.NewFromInt(100))
	if value.GreaterThan(decimal.NewFromInt(math.MaxInt64)) {
		return 0, ErrResellerFinanceInvalidInput
	}
	return value.IntPart(), nil
}

const (
	PaymentProviderEpay         = "epay"
	PaymentProviderStripe       = "stripe"
	PaymentProviderCreem        = "creem"
	PaymentProviderWaffo        = "waffo"
	PaymentProviderWaffoPancake = "waffo_pancake"
	PaymentProviderBalance      = "balance"
	PaymentProviderRedemption   = "redemption"
)

var (
	ErrPaymentMethodMismatch   = errors.New("payment method mismatch")
	ErrTopUpNotFound           = errors.New("topup not found")
	ErrTopUpStatusInvalid      = errors.New("topup status invalid")
	ErrInvalidTopUpQuota       = errors.New("invalid top-up quota")
	ErrTopUpQuotaLimitExceeded = errors.New("top-up quota limit exceeded")
)

func (topUp *TopUp) Insert() error {
	var user User
	if err := DB.Select("id", "tenant_id").First(&user, topUp.UserId).Error; err != nil {
		return err
	}
	if topUp.TenantID != user.TenantID || (user.TenantID > 0 && (topUp.PaymentProvider != PaymentProviderEpay || topUp.Currency != ResellerCurrencyCNY || topUp.PaidMinor <= 0)) {
		return ErrResellerFinanceInvalidInput
	}
	return DB.Create(topUp).Error
}

func topUpQuotaMaxCurrent(creditedQuota int64) (int64, error) {
	if creditedQuota <= 0 {
		return 0, ErrInvalidTopUpQuota
	}
	return int64(math.MaxInt64) - creditedQuota, nil
}

// ValidateTopUpQuotaCapacity performs the user-facing pre-payment check. The
// settlement path repeats the same invariant while holding the user row lock,
// because the wallet balance can change after checkout creation.
func ValidateTopUpQuotaCapacity(userId int, creditedQuota int64) error {
	maxCurrentQuota, err := topUpQuotaMaxCurrent(creditedQuota)
	if err != nil {
		return err
	}

	var user User
	if err := DB.Select("quota").Where("id = ?", userId).First(&user).Error; err != nil {
		return err
	}
	if user.Quota > maxCurrentQuota {
		return ErrTopUpQuotaLimitExceeded
	}
	return nil
}

func (topUp *TopUp) Update() error {
	var err error
	err = DB.Save(topUp).Error
	return err
}

func GetTopUpById(id int) *TopUp {
	var topUp *TopUp
	var err error
	err = DB.Where("id = ?", id).First(&topUp).Error
	if err != nil {
		return nil
	}
	return topUp
}

func GetTopUpByTradeNo(tradeNo string) *TopUp {
	topUp, err := FindTopUpByTradeNo(tradeNo)
	if err != nil {
		return nil
	}
	return topUp
}

func FindTopUpByTradeNo(tradeNo string) (*TopUp, error) {
	topUp := &TopUp{}
	if err := DB.Where("trade_no = ?", tradeNo).First(topUp).Error; err != nil {
		return nil, err
	}
	return topUp, nil
}

func UpdatePendingTopUpStatus(tradeNo string, expectedPaymentProvider string, targetStatus string) error {
	if tradeNo == "" {
		return errors.New("未提供支付单号")
	}

	refCol := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		refCol = `"trade_no"`
	}

	return DB.Transaction(func(tx *gorm.DB) error {
		topUp := &TopUp{}
		if err := lockForUpdate(tx).Where(refCol+" = ?", tradeNo).First(topUp).Error; err != nil {
			return ErrTopUpNotFound
		}
		if expectedPaymentProvider != "" && topUp.PaymentProvider != expectedPaymentProvider {
			return ErrPaymentMethodMismatch
		}
		if topUp.Status != common.TopUpStatusPending {
			return ErrTopUpStatusInvalid
		}

		topUp.Status = targetStatus
		return tx.Save(topUp).Error
	})
}

func Recharge(referenceId string, customerId string, callerIp string) (err error) {
	result, err := FinalizeTopUp(FinalizeTopUpInput{
		TradeNo:          referenceId,
		ExpectedProvider: PaymentProviderStripe,
		Prepare: func(topUp *TopUp, _ *User) (TopUpCompletion, error) {
			quotaDelta, err := quotaFromTopUpMoney(topUp.Money)
			if err != nil {
				return TopUpCompletion{}, err
			}
			return TopUpCompletion{
				QuotaDelta: quotaDelta,
				UserPatch: TopUpUserPatch{
					StripeCustomer: &customerId,
				},
			}, nil
		},
	})
	if err != nil {
		common.SysError("topup failed: " + err.Error())
		return errors.New("充值失败，请稍后重试")
	}
	if !result.AlreadyCompleted {
		RecordTopupLog(result.TopUp.UserId, fmt.Sprintf("使用在线充值成功，充值金额: %v，支付金额：%d", logger.FormatQuota(result.QuotaDelta), result.TopUp.Amount), callerIp, result.TopUp.PaymentMethod, PaymentMethodStripe)
	}
	return nil
}

// topUpQueryWindowSeconds 限制充值记录查询的时间窗口（秒）。
const topUpQueryWindowSeconds int64 = 30 * 24 * 60 * 60

// topUpQueryCutoff 返回允许查询的最早 create_time（秒级 Unix 时间戳）。
func topUpQueryCutoff() int64 {
	return common.GetTimestamp() - topUpQueryWindowSeconds
}

func GetTopUpTodayPaymentTotal(userId *int, now time.Time) (float64, error) {
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	query := DB.Model(&TopUp{}).
		Where("status = ? AND complete_time >= ? AND complete_time < ?", common.TopUpStatusSuccess, start.Unix(), start.AddDate(0, 0, 1).Unix())
	if userId != nil {
		query = query.Where("user_id = ?", *userId)
	}
	var total float64
	err := query.Select("COALESCE(SUM(money), 0)").Scan(&total).Error
	return total, err
}

func GetUserTopUps(userId int, pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	// Start transaction
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	cutoff := topUpQueryCutoff()

	// Get total count within transaction
	err = tx.Model(&TopUp{}).Where("user_id = ? AND create_time >= ?", userId, cutoff).Count(&total).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// Get paginated topups within same transaction
	err = tx.Where("user_id = ? AND create_time >= ?", userId, cutoff).Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// Commit transaction
	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	return topups, total, nil
}

// GetAllTopUps 获取全平台的充值记录（管理员使用，不限制时间窗口）
func GetAllTopUps(pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	if err = tx.Model(&TopUp{}).Count(&total).Error; err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	if err = tx.Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error; err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	return topups, total, nil
}

// searchTopUpCountHardLimit 搜索充值记录时 COUNT 的安全上限，
// 防止对超大表执行无界 COUNT 触发 DoS。
const searchTopUpCountHardLimit = 10000

// SearchUserTopUps 按订单号搜索某用户的充值记录
func SearchUserTopUps(userId int, keyword string, pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	query := tx.Model(&TopUp{}).Where("user_id = ? AND create_time >= ?", userId, topUpQueryCutoff())
	if keyword != "" {
		pattern, perr := sanitizeLikePattern(keyword)
		if perr != nil {
			tx.Rollback()
			return nil, 0, perr
		}
		query = query.Where("trade_no LIKE ? ESCAPE '!'", pattern)
	}

	if err = query.Limit(searchTopUpCountHardLimit).Count(&total).Error; err != nil {
		tx.Rollback()
		common.SysError("failed to count search topups: " + err.Error())
		return nil, 0, errors.New("搜索充值记录失败")
	}

	if err = query.Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error; err != nil {
		tx.Rollback()
		common.SysError("failed to search topups: " + err.Error())
		return nil, 0, errors.New("搜索充值记录失败")
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}
	return topups, total, nil
}

// SearchAllTopUps 按订单号搜索全平台充值记录（管理员使用，不限制时间窗口）
func SearchAllTopUps(keyword string, pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	query := tx.Model(&TopUp{})
	if keyword != "" {
		pattern, perr := sanitizeLikePattern(keyword)
		if perr != nil {
			tx.Rollback()
			return nil, 0, perr
		}
		query = query.Where("trade_no LIKE ? ESCAPE '!'", pattern)
	}

	if err = query.Limit(searchTopUpCountHardLimit).Count(&total).Error; err != nil {
		tx.Rollback()
		common.SysError("failed to count search topups: " + err.Error())
		return nil, 0, errors.New("搜索充值记录失败")
	}

	if err = query.Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error; err != nil {
		tx.Rollback()
		common.SysError("failed to search topups: " + err.Error())
		return nil, 0, errors.New("搜索充值记录失败")
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}
	return topups, total, nil
}

// ManualCompleteTopUp 管理员手动完成订单并给用户充值
func ManualCompleteTopUp(tradeNo string, callerIp string) error {
	result, err := FinalizeTopUp(FinalizeTopUpInput{
		TradeNo: tradeNo,
		Prepare: func(order *TopUp, user *User) (TopUpCompletion, error) {
			completion, err := prepareManualTopUpCompletion(order, user)
			if err != nil {
				return TopUpCompletion{}, err
			}
			if WalletTopUpDeadline(order) <= 0 || WalletTopUpLocalEnded(order, common.GetTimestamp()) {
				return TopUpCompletion{}, ErrWalletAttemptEnded
			}
			return completion, nil
		},
	})
	if err != nil {
		return err
	}
	if !result.AlreadyCompleted {
		RecordTopupLog(result.TopUp.UserId, fmt.Sprintf("管理员补单成功，充值金额: %v，支付金额：%f", logger.FormatQuota(result.QuotaDelta), result.TopUp.Money), callerIp, result.TopUp.PaymentMethod, "admin")
	}
	return nil
}

func RechargeCreem(referenceId string, customerEmail string, customerName string, callerIp string) (err error) {
	_ = customerName
	result, err := FinalizeTopUp(FinalizeTopUpInput{
		TradeNo:          referenceId,
		ExpectedProvider: PaymentProviderCreem,
		Prepare: func(topUp *TopUp, _ *User) (TopUpCompletion, error) {
			quotaDelta, err := quotaFromTopUpCredits(topUp.Amount)
			if err != nil {
				return TopUpCompletion{}, err
			}
			completion := TopUpCompletion{QuotaDelta: quotaDelta}
			if customerEmail != "" {
				completion.UserPatch.EmailIfEmpty = &customerEmail
			}
			return completion, nil
		},
	})
	if err != nil {
		common.SysError("creem topup failed: " + err.Error())
		return errors.New("充值失败，请稍后重试")
	}
	if !result.AlreadyCompleted {
		RecordTopupLog(result.TopUp.UserId, fmt.Sprintf("使用Creem充值成功，充值额度: %v，支付金额：%.2f", result.QuotaDelta, result.TopUp.Money), callerIp, result.TopUp.PaymentMethod, PaymentMethodCreem)
	}
	return nil
}

func RechargeWaffo(tradeNo string, callerIp string) (err error) {
	result, err := FinalizeTopUp(FinalizeTopUpInput{
		TradeNo:          tradeNo,
		ExpectedProvider: PaymentProviderWaffo,
		Prepare: func(topUp *TopUp, _ *User) (TopUpCompletion, error) {
			quotaDelta, err := quotaFromTopUpAmount(topUp.Amount)
			return TopUpCompletion{QuotaDelta: quotaDelta}, err
		},
	})
	if err != nil {
		common.SysError("waffo topup failed: " + err.Error())
		return errors.New("充值失败，请稍后重试")
	}
	if !result.AlreadyCompleted {
		RecordTopupLog(result.TopUp.UserId, fmt.Sprintf("Waffo充值成功，充值额度: %v，支付金额: %.2f", logger.FormatQuota(result.QuotaDelta), result.TopUp.Money), callerIp, result.TopUp.PaymentMethod, PaymentMethodWaffo)
	}
	return nil
}

func RechargeWaffoPancake(tradeNo string) (err error) {
	result, err := FinalizeTopUp(FinalizeTopUpInput{
		TradeNo:          tradeNo,
		ExpectedProvider: PaymentProviderWaffoPancake,
		Prepare: func(topUp *TopUp, _ *User) (TopUpCompletion, error) {
			quotaDelta, err := quotaFromTopUpAmount(topUp.Amount)
			return TopUpCompletion{QuotaDelta: quotaDelta}, err
		},
	})
	if err != nil {
		common.SysError("waffo pancake topup failed: " + err.Error())
		return errors.New("充值失败，请稍后重试")
	}
	if !result.AlreadyCompleted {
		RecordLog(result.TopUp.UserId, LogTypeTopup, fmt.Sprintf("Waffo Pancake充值成功，充值额度: %v，支付金额: %.2f", logger.FormatQuota(result.QuotaDelta), result.TopUp.Money))
	}
	return nil
}

func RechargeEpay(tradeNo string, paymentMethod string, paidAmount ...string) (*FinalizeTopUpResult, error) {
	return rechargeEpay(tradeNo, paymentMethod, nil, paidAmount...)
}

// RechargeEpayVerified is used only after authenticating a successful Epay
// callback. A durable exact receipt, not checkout data, enables invoice eligibility.
func RechargeEpayVerified(tradeNo, paymentMethod, paidAmount, providerReceiptID string) (*FinalizeTopUpResult, error) {
	minor, err := ParseCNYMinor(paidAmount)
	if err != nil || minor <= 0 {
		return nil, ErrResellerFinanceInvalidInput
	}
	return rechargeEpay(tradeNo, paymentMethod, &TopUpPaymentReceipt{ProviderReceiptID: providerReceiptID, AmountMinor: minor, Currency: ResellerCurrencyCNY}, paidAmount)
}

func rechargeEpay(tradeNo, paymentMethod string, receipt *TopUpPaymentReceipt, paidAmount ...string) (*FinalizeTopUpResult, error) {
	return FinalizeTopUp(FinalizeTopUpInput{
		TradeNo:          tradeNo,
		ExpectedProvider: PaymentProviderEpay,
		PaymentReceipt:   receipt,
		Prepare: func(topUp *TopUp, _ *User) (TopUpCompletion, error) {
			if topUp.TenantID > 0 {
				if len(paidAmount) != 1 {
					return TopUpCompletion{}, ErrResellerFinanceInvalidInput
				}
				minor, err := ParseCNYMinor(paidAmount[0])
				if err != nil || minor != topUp.PaidMinor {
					return TopUpCompletion{}, ErrResellerFinanceInvalidInput
				}
			}
			quotaDelta, err := quotaFromTopUpAmount(topUp.Amount)
			if err != nil {
				return TopUpCompletion{}, err
			}
			return TopUpCompletion{
				QuotaDelta:    quotaDelta,
				PaymentMethod: paymentMethod,
			}, nil
		},
	})
}
