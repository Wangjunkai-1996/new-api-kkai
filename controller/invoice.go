package controller

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/kkaimigrate"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Recheck the session and host here as well as in router middleware. These
// financial handlers must remain safe if reused from another route group.
func invoiceScope(c *gin.Context, admin bool) (service.InvoiceScope, bool) {
	identity, session := middleware.GetSessionAuthIdentity(c)
	if !session {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"success": false, "code": "SESSION_REQUIRED", "message": "a dashboard session is required"})
		return service.InvoiceScope{}, false
	}
	tenant, ok := middleware.GetTenantScope(c)
	if !ok || tenant.TenantID != model.PlatformTenantID || (admin && c.GetInt("role") < common.RoleAdminUser) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"success": false, "code": "FORBIDDEN", "message": "invoices are available on the main site only"})
		return service.InvoiceScope{}, false
	}
	if model.DB == nil {
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"success": false, "code": "SCHEMA_NOT_READY", "message": "invoices are unavailable"})
		return service.InvoiceScope{}, false
	}
	return service.InvoiceScope{TenantID: tenant.TenantID, UserID: identity.UserID, Admin: admin}, true
}

func invoiceHTTPError(c *gin.Context, err error) {
	var business *service.InvoiceError
	if errors.As(err, &business) {
		c.JSON(business.Status, gin.H{"success": false, "code": business.Code, "message": business.Message, "field_errors": business.FieldErrors})
		return
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "code": "NOT_FOUND", "message": "invoice resource not found"})
		return
	}
	// Do not log arbitrary driver/storage errors, which can include sensitive SQL
	// parameters, object keys or URLs. The request middleware supplies correlation.
	common.SysError("invoice operation failed")
	c.JSON(http.StatusInternalServerError, gin.H{"success": false, "code": "INVOICE_FAILED", "message": "unable to process invoice operation"})
}

func invoiceDecode(c *gin.Context, target any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	if err := common.DecodeJsonSingle(c.Request.Body, target); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"success": false, "code": "BODY_TOO_LARGE", "message": "request body is too large"})
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "code": "VALIDATION_FAILED", "message": "invalid request body"})
		}
		return false
	}
	return true
}

func invoiceFilter(c *gin.Context) (service.InvoiceFilter, bool) {
	filter := service.InvoiceFilter{P: 1, PageSize: 20}
	if c.ShouldBindQuery(&filter) != nil || filter.P < 1 || filter.PageSize < 1 || filter.PageSize > 100 || filter.P > 1_000_000 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "code": "VALIDATION_FAILED", "message": "invalid pagination or filter"})
		return filter, false
	}
	return filter, true
}

func InvoicePolicy(c *gin.Context) {
	if _, ok := invoiceScope(c, false); !ok {
		return
	}
	policy := (service.InvoiceService{DB: model.DB}).Policy()
	if kkaimigrate.CheckInvoiceSchema(c.Request.Context(), model.DB) != nil {
		policy.Enabled, policy.DisabledReason = false, "SCHEMA_NOT_READY"
	}
	common.ApiSuccess(c, policy)
}

func InvoiceCandidates(c *gin.Context) {
	scope, ok := invoiceScope(c, false)
	if !ok {
		return
	}
	filter, ok := invoiceFilter(c)
	if !ok {
		return
	}
	result, err := (service.InvoiceService{DB: model.DB}).Candidates(c.Request.Context(), scope, filter)
	if err != nil {
		invoiceHTTPError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

func InvoiceQuote(c *gin.Context) {
	scope, ok := invoiceScope(c, false)
	if !ok {
		return
	}
	var input service.InvoiceQuoteInput
	if !invoiceDecode(c, &input) {
		return
	}
	result, err := (service.InvoiceService{DB: model.DB}).Quote(c.Request.Context(), scope, input)
	if err != nil {
		invoiceHTTPError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

func InvoiceCreate(c *gin.Context) {
	scope, ok := invoiceScope(c, false)
	if !ok {
		return
	}
	var input service.InvoiceCreateInput
	if !invoiceDecode(c, &input) {
		return
	}
	result, err := (service.InvoiceService{DB: model.DB}).Create(c.Request.Context(), scope, c.GetHeader("Idempotency-Key"), input)
	if err != nil {
		invoiceHTTPError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

func InvoiceList(admin bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		scope, ok := invoiceScope(c, admin)
		if !ok {
			return
		}
		filter, ok := invoiceFilter(c)
		if !ok {
			return
		}
		result, err := (service.InvoiceService{DB: model.DB}).List(c.Request.Context(), scope, filter)
		if err != nil {
			invoiceHTTPError(c, err)
			return
		}
		common.ApiSuccess(c, result)
	}
}

func InvoiceSummary(admin bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		scope, ok := invoiceScope(c, admin)
		if !ok {
			return
		}
		filter, ok := invoiceFilter(c)
		if !ok {
			return
		}
		result, err := (service.InvoiceService{DB: model.DB}).Summary(c.Request.Context(), scope, filter)
		if err != nil {
			invoiceHTTPError(c, err)
			return
		}
		common.ApiSuccess(c, result)
	}
}

func InvoiceDetail(admin bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		scope, ok := invoiceScope(c, admin)
		if !ok {
			return
		}
		result, err := (service.InvoiceService{DB: model.DB}).Get(c.Request.Context(), scope, c.Param("public_id"))
		if err != nil {
			invoiceHTTPError(c, err)
			return
		}
		common.ApiSuccess(c, result)
	}
}

func InvoiceAction(admin bool, action string) gin.HandlerFunc {
	return func(c *gin.Context) {
		scope, ok := invoiceScope(c, admin)
		if !ok {
			return
		}
		var input service.InvoiceActionInput
		if !invoiceDecode(c, &input) {
			return
		}
		input.DisputeID = c.Param("dispute_id")
		result, err := (service.InvoiceService{DB: model.DB}).Mutate(c.Request.Context(), scope, c.Param("public_id"), action, c.GetHeader("Idempotency-Key"), input)
		if err != nil {
			invoiceHTTPError(c, err)
			return
		}
		common.ApiSuccess(c, result)
	}
}

func InvoiceProfiles(c *gin.Context) {
	scope, ok := invoiceScope(c, false)
	if !ok {
		return
	}
	filter, ok := invoiceFilter(c)
	if !ok {
		return
	}
	result, err := (service.InvoiceService{DB: model.DB}).Profiles(c.Request.Context(), scope, filter)
	if err != nil {
		invoiceHTTPError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

func InvoiceGetProfile(c *gin.Context) {
	scope, ok := invoiceScope(c, false)
	if !ok {
		return
	}
	result, err := (service.InvoiceService{DB: model.DB}).GetProfile(c.Request.Context(), scope, c.Param("id"))
	if err != nil {
		invoiceHTTPError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

func InvoiceSaveProfile(c *gin.Context) {
	scope, ok := invoiceScope(c, false)
	if !ok {
		return
	}
	var input service.InvoiceProfileInput
	if !invoiceDecode(c, &input) {
		return
	}
	result, err := (service.InvoiceService{DB: model.DB}).SaveProfile(c.Request.Context(), scope, c.Param("id"), c.GetHeader("Idempotency-Key"), input)
	if err != nil {
		invoiceHTTPError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

func InvoiceDeleteProfile(c *gin.Context) {
	scope, ok := invoiceScope(c, false)
	if !ok {
		return
	}
	var input struct {
		Version int64 `json:"version"`
	}
	if !invoiceDecode(c, &input) {
		return
	}
	if err := (service.InvoiceService{DB: model.DB}).DeleteProfile(c.Request.Context(), scope, c.Param("id"), c.GetHeader("Idempotency-Key"), input.Version); err != nil {
		invoiceHTTPError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{})
}

func InvoiceNotifications(admin bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		scope, ok := invoiceScope(c, admin)
		if !ok {
			return
		}
		filter, ok := invoiceFilter(c)
		if !ok {
			return
		}
		result, err := (service.InvoiceService{DB: model.DB}).Notifications(c.Request.Context(), scope, filter)
		if err != nil {
			invoiceHTTPError(c, err)
			return
		}
		common.ApiSuccess(c, result)
	}
}

func InvoiceReadNotifications(admin bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		scope, ok := invoiceScope(c, admin)
		if !ok {
			return
		}
		if err := (service.InvoiceService{DB: model.DB}).ReadNotifications(c.Request.Context(), scope, c.Param("event_id")); err != nil {
			invoiceHTTPError(c, err)
			return
		}
		common.ApiSuccess(c, gin.H{})
	}
}

func InvoiceUpload(admin bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		scope, ok := invoiceScope(c, admin)
		if !ok {
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 21<<20)
		parseErr := c.Request.ParseMultipartForm(1 << 20)
		if c.Request.MultipartForm != nil {
			defer c.Request.MultipartForm.RemoveAll()
		}
		if parseErr != nil {
			var tooLarge *http.MaxBytesError
			status, code := http.StatusBadRequest, "VALIDATION_FAILED"
			if errors.As(parseErr, &tooLarge) {
				status, code = http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE"
			}
			c.JSON(status, gin.H{"success": false, "code": code, "message": "invalid upload"})
			return
		}
		files := c.Request.MultipartForm.File["file"]
		if len(files) != 1 || len(c.Request.MultipartForm.File) != 1 {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "code": "VALIDATION_FAILED", "message": "upload one file"})
			return
		}
		file, err := files[0].Open()
		if err != nil {
			invoiceHTTPError(c, err)
			return
		}
		defer file.Close()
		assets, err := service.NewInvoiceAssetServiceFromEnvironment(c.Request.Context(), model.DB)
		if err != nil {
			invoiceHTTPError(c, err)
			return
		}
		result, err := assets.Upload(c.Request.Context(), scope, c.Param("public_id"), c.GetHeader("Idempotency-Key"), c.PostForm("purpose"), files[0].Filename, file)
		if err != nil {
			invoiceHTTPError(c, err)
			return
		}
		common.ApiSuccess(c, result)
	}
}

func InvoiceFile(admin bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		scope, ok := invoiceScope(c, admin)
		if !ok {
			return
		}
		assets, err := service.NewInvoiceAssetServiceFromEnvironment(c.Request.Context(), model.DB)
		if err != nil {
			invoiceHTTPError(c, err)
			return
		}
		attachment, object, err := assets.Open(c.Request.Context(), scope, c.Param("public_id"), c.Param("file_id"))
		if err != nil {
			invoiceHTTPError(c, err)
			return
		}
		defer object.Body.Close()
		disposition := "inline"
		if c.Query("disposition") == "attachment" {
			disposition = "attachment"
		}
		c.Header("Cache-Control", "private, no-store")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Content-Security-Policy", "sandbox; default-src 'none'; frame-ancestors 'self'")
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("Content-Type", attachment.MIMEType)
		c.Header("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": attachment.FileName}))
		c.Header("Content-Length", strconv.FormatInt(attachment.Size, 10))
		c.Status(http.StatusOK)
		_, _ = io.Copy(c.Writer, io.LimitReader(object.Body, attachment.Size))
	}
}
