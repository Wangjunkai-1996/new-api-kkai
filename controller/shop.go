package controller

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var shopKeyPattern = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)
var shopIntegerPattern = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)

func shopDB(c *gin.Context) *gorm.DB { return model.DB.WithContext(c.Request.Context()) }
func shopScope(c *gin.Context) model.ShopScope {
	return model.ShopScope{UserID: c.GetInt("id"), TenantID: common.GetContextKeyInt64(c, constant.ContextKeyTenantID), Admin: c.GetBool("shop_admin")}
}
func shopHeaders(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
}
func shopSuccess(c *gin.Context, status int, data any) {
	shopHeaders(c)
	c.JSON(status, gin.H{"success": true, "message": "", "data": data})
}
func shopError(c *gin.Context, status int, code string) {
	shopHeaders(c)
	c.AbortWithStatusJSON(status, gin.H{"success": false, "code": code, "message": code})
}
func shopModelError(c *gin.Context, err error) {
	status, code := http.StatusInternalServerError, "SHOP_INTERNAL_ERROR"
	switch {
	case errors.Is(err, model.ErrShopNotFound), errors.Is(err, gorm.ErrRecordNotFound):
		status, code = 404, "SHOP_NOT_FOUND"
	case errors.Is(err, model.ErrShopInvalid):
		status, code = 400, "SHOP_VALIDATION_FAILED"
	case errors.Is(err, model.ErrShopSecret), errors.Is(err, service.ErrShopSecretUnavailable):
		status, code = 503, "SHOP_SECRET_UNAVAILABLE"
	case errors.Is(err, service.ErrShopChannelUnavailable):
		status, code = 503, "SHOP_CHANNEL_UNAVAILABLE"
	case errors.Is(err, model.ErrShopRevealBlocked):
		status, code = 403, "SHOP_REVEAL_BLOCKED"
	case errors.Is(err, model.ErrShopRevealExpired):
		status, code = 410, "SHOP_REVEAL_EXPIRED"
	default:
		for _, known := range []error{model.ErrShopVersion, model.ErrShopIdempotency, model.ErrShopPaused, model.ErrShopProductChanged, model.ErrShopStock, model.ErrShopCapacity, model.ErrShopPendingLimit, model.ErrShopAction, model.ErrShopPaymentUnresolved, model.ErrShopEvidence, model.ErrShopRefund, model.ErrShopResponsibilities} {
			if errors.Is(err, known) {
				status, code = 409, known.Error()
				break
			}
		}
	}
	// Never attach driver/provider errors to Gin's error list: they can contain
	// SQL arguments, encrypted inventory, buyer input, or checkout credentials.
	var rejected *service.ShopPaymentNotCommittedError
	if errors.As(err, &rejected) && status >= 400 && status < 500 {
		shopHeaders(c)
		c.AbortWithStatusJSON(status, gin.H{"success": false, "code": code, "message": code, "data": gin.H{"request_outcome": "not_committed"}})
		return
	}
	shopError(c, status, code)
}
func ShopSchemaGuard(c *gin.Context) {
	shopHeaders(c)
	if err := service.CheckShopRuntimeSchema(c.Request.Context(), model.DB); err != nil {
		shopError(c, 503, "SHOP_SCHEMA_UNAVAILABLE")
		return
	}
	c.Next()
}
func shopBind(c *gin.Context, target any, maxBytes int64) bool {
	kind, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || kind != "application/json" {
		shopError(c, 400, "SHOP_VALIDATION_FAILED")
		return false
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxBytes+1))
	if err != nil || int64(len(body)) > maxBytes || !strings.HasPrefix(strings.TrimSpace(string(body)), "{") || common.UnmarshalStrict(body, target) != nil {
		shopError(c, 400, "SHOP_VALIDATION_FAILED")
		return false
	}
	return true
}
func shopVersion(value string) (int64, error) {
	if !shopIntegerPattern.MatchString(value) {
		return 0, model.ErrShopInvalid
	}
	version, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, model.ErrShopInvalid
	}
	return version, nil
}
func shopCommand(c *gin.Context, input any, action, target, expectedVersion string) (model.ShopCommand, bool) {
	scope := shopScope(c)
	command := model.ShopCommand{ActorID: scope.UserID, TenantID: scope.TenantID, Admin: scope.Admin, Key: c.GetHeader("Idempotency-Key"), Now: time.Now().Unix()}
	if !shopKeyPattern.MatchString(command.Key) {
		shopError(c, 400, "SHOP_VALIDATION_FAILED")
		return command, false
	}
	var err error
	if expectedVersion == "" {
		createsResource := action == "create-order" || action == "create-aftersale" || action == "settings" || (action == "category" || action == "product") && target == ""
		if !createsResource {
			shopModelError(c, model.ErrShopInvalid)
			return command, false
		}
	} else {
		command.ExpectedVersion, err = shopVersion(expectedVersion)
		if err != nil {
			shopModelError(c, err)
			return command, false
		}
	}
	previous, err := model.GetShopIdempotency(shopDB(c), scope, action, target, command.Key)
	if err != nil {
		shopModelError(c, err)
		return command, false
	}
	hashVersion := ""
	if previous != nil {
		hashVersion = previous.RequestHashKeyVersion
	}
	command.RequestHash, command.HashKeyVersion, err = service.ShopRequestHash(input, action+":"+target, hashVersion)
	if err != nil {
		shopModelError(c, model.ErrShopSecret)
		return command, false
	}
	if previous != nil && (previous.RequestHash != command.RequestHash || previous.RequestHashKeyVersion != command.HashKeyVersion) {
		shopModelError(c, model.ErrShopIdempotency)
		return command, false
	}
	return command, true
}
func shopPagination(c *gin.Context) (page, size int, ok bool) {
	page, size = 1, 20
	var err error
	if value := c.Query("page"); value != "" {
		page, err = strconv.Atoi(value)
		if err != nil {
			shopError(c, 400, "SHOP_VALIDATION_FAILED")
			return
		}
	}
	if value := c.Query("page_size"); value != "" {
		size, err = strconv.Atoi(value)
		if err != nil {
			shopError(c, 400, "SHOP_VALIDATION_FAILED")
			return
		}
	}
	if page < 1 || page > 10000 || size < 1 || size > 100 || len([]rune(c.Query("q"))) > 100 {
		shopError(c, 400, "SHOP_VALIDATION_FAILED")
		return
	}
	return page, size, true
}
func shopPage(items any, page, size int, total int64) gin.H {
	return gin.H{"items": items, "page": page, "page_size": size, "total": total}
}
func shopTime(value int64) any {
	if value <= 0 {
		return nil
	}
	return time.Unix(value, 0).UTC().Format(time.RFC3339)
}
func shopID(value int64) string { return strconv.FormatInt(value, 10) }

func shopCommitted(c *gin.Context, command model.ShopCommand, action, target string) (*model.ShopIdempotencyKey, bool) {
	previous, err := model.GetShopIdempotency(shopDB(c), shopScope(c), action, target, command.Key)
	if err != nil {
		shopModelError(c, err)
		return nil, false
	}
	return previous, true
}

func shopConsumeProof(c *gin.Context, scope string, value service.ShopVerificationContext) bool {
	// All shop proofs are session-only, including when the shared verifier also
	// supports access tokens for unrelated operations.
	if _, ok := middleware.GetSessionAuthIdentity(c); !ok {
		shopError(c, 403, "SHOP_SESSION_REQUIRED")
		return false
	}
	raw, err := common.Marshal(value)
	if err != nil {
		shopModelError(c, model.ErrShopInvalid)
		return false
	}
	return middleware.RequireSecurityProof(c, service.VerificationOperation{Scope: scope, Context: raw}) != nil
}
func shopMoney(amount int64, currency string, exponent int) gin.H {
	return gin.H{"amount_minor": shopID(amount), "currency": currency, "exponent": exponent}
}
