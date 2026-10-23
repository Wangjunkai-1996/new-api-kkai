package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

func GetWalletFundingSummary(c *gin.Context) {
	if !walletSchemaReady(c) {
		return
	}
	summary, err := model.GetWalletFundingSummary(c.Request.Context(), walletScope(c))
	if err != nil {
		walletModelError(c, err)
		return
	}
	quota, err := decimal.NewFromString(summary.RebateCreditedQuota)
	if err != nil || common.QuotaPerUnit <= 0 {
		walletError(c, http.StatusServiceUnavailable, "WALLET_STATE_UNAVAILABLE", nil)
		return
	}
	summary.RebateCreditDisplay = dto.WalletCreditDisplay{Amount: quota.Div(decimal.NewFromFloat(common.QuotaPerUnit)).String(), Unit: "USD"}
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		summary.RebateCreditDisplay = dto.WalletCreditDisplay{Amount: summary.RebateCreditedQuota, Unit: "TOKENS"}
	}
	walletSuccess(c, http.StatusOK, summary)
}
