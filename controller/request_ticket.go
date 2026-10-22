package controller

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func requestTicketScope(c *gin.Context) (int64, bool) {
	scope, ok := middleware.GetTenantScope(c)
	if !ok || scope.TenantID != model.PlatformTenantID || c.GetInt("id") <= 0 {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "code": "REQUEST_TICKET_FORBIDDEN", "message": "request tickets are available on the main site only"})
		return 0, false
	}
	return scope.TenantID, true
}

func requestTicketError(c *gin.Context, err error) {
	status, code, message := http.StatusInternalServerError, "REQUEST_TICKET_FAILED", "unable to process request ticket"
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		status, code, message = http.StatusNotFound, "REQUEST_TICKET_NOT_FOUND", "request ticket or notification not found"
	case errors.Is(err, model.ErrRequestTicketInvalid):
		status, code, message = http.StatusBadRequest, "REQUEST_TICKET_INVALID", "check the title, description, feedback and pagination limits"
	case errors.Is(err, model.ErrRequestTicketTransition):
		status, code, message = http.StatusBadRequest, "REQUEST_TICKET_TRANSITION", "this status transition is not allowed"
	case errors.Is(err, model.ErrRequestTicketConflict):
		status, code, message = http.StatusConflict, "REQUEST_TICKET_CONFLICT", "the request ticket has changed; refresh and try again"
	default:
		common.SysError("request ticket operation failed: " + err.Error())
	}
	c.JSON(status, gin.H{"success": false, "code": code, "message": message})
}

func requestTicketPage(c *gin.Context) (*common.PageInfo, bool) {
	page := common.GetPageQuery(c)
	if page.Page < 1 || page.PageSize < 1 || page.Page-1 > int(^uint(0)>>1)/page.PageSize {
		requestTicketError(c, model.ErrRequestTicketInvalid)
		return nil, false
	}
	return page, true
}

func CreateRequestTicket(c *gin.Context) {
	tenantID, ok := requestTicketScope(c)
	if !ok {
		return
	}
	var input struct {
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32<<10)
	if err := common.DecodeJsonSingle(c.Request.Body, &input); err != nil {
		requestTicketError(c, model.ErrRequestTicketInvalid)
		return
	}
	db := model.DB.WithContext(c.Request.Context())
	ticket, err := model.CreateRequestTicket(db, tenantID, c.GetInt("id"), input.Title, input.Description)
	if err != nil {
		requestTicketError(c, err)
		return
	}
	detail, err := model.GetRequestTicketDetail(db, tenantID, c.GetInt("id"), ticket.ID)
	if err != nil {
		requestTicketError(c, err)
		return
	}
	common.ApiSuccess(c, detail)
}

func ListRequestTickets(c *gin.Context) { listRequestTickets(c, false) }

func AdminListRequestTickets(c *gin.Context) { listRequestTickets(c, true) }

func listRequestTickets(c *gin.Context, admin bool) {
	tenantID, ok := requestTicketScope(c)
	if !ok {
		return
	}
	page, ok := requestTicketPage(c)
	if !ok {
		return
	}
	userID := c.GetInt("id")
	if admin {
		userID = 0
	}
	items, total, err := model.ListRequestTickets(model.DB.WithContext(c.Request.Context()), tenantID, userID, c.Query("status"), page.GetStartIdx(), page.GetPageSize())
	if err != nil {
		requestTicketError(c, err)
		return
	}
	page.SetItems(items)
	page.SetTotal(int(total))
	common.ApiSuccess(c, page)
}

func GetRequestTicket(c *gin.Context) { getRequestTicket(c, false) }

func AdminGetRequestTicket(c *gin.Context) { getRequestTicket(c, true) }

func getRequestTicket(c *gin.Context, admin bool) {
	tenantID, ok := requestTicketScope(c)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		requestTicketError(c, model.ErrRequestTicketInvalid)
		return
	}
	userID := c.GetInt("id")
	if admin {
		userID = 0
	}
	detail, err := model.GetRequestTicketDetail(model.DB.WithContext(c.Request.Context()), tenantID, userID, id)
	if err != nil {
		requestTicketError(c, err)
		return
	}
	common.ApiSuccess(c, detail)
}

func AdminTransitionRequestTicket(c *gin.Context) {
	tenantID, ok := requestTicketScope(c)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	var input struct {
		Status   string `json:"status"`
		Feedback string `json:"feedback"`
		Version  int64  `json:"version"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32<<10)
	if err != nil || id <= 0 || common.DecodeJsonSingle(c.Request.Body, &input) != nil {
		requestTicketError(c, model.ErrRequestTicketInvalid)
		return
	}
	db := model.DB.WithContext(c.Request.Context())
	ticket, err := model.TransitionRequestTicket(db, tenantID, id, c.GetInt("id"), input.Version, input.Status, input.Feedback)
	if err != nil {
		requestTicketError(c, err)
		return
	}
	// Email runs only after the durable status/event transaction has committed.
	if common.SMTPServer != "" {
		go service.NotifyRequestTicketUpdate(*ticket, input.Feedback)
	}
	detail, err := model.GetRequestTicketDetail(db, tenantID, 0, ticket.ID)
	if err != nil {
		requestTicketError(c, err)
		return
	}
	common.ApiSuccess(c, detail)
}

func ListRequestTicketNotifications(c *gin.Context) {
	tenantID, ok := requestTicketScope(c)
	if !ok {
		return
	}
	page, ok := requestTicketPage(c)
	if !ok {
		return
	}
	items, total, unread, err := model.ListRequestTicketNotifications(model.DB.WithContext(c.Request.Context()), tenantID, c.GetInt("id"), page.GetStartIdx(), page.GetPageSize())
	if err != nil {
		requestTicketError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"items": items, "total": total, "unread_count": unread, "page": page.Page, "page_size": page.PageSize})
}

func ReadRequestTicketNotification(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		requestTicketError(c, model.ErrRequestTicketInvalid)
		return
	}
	readRequestTicketNotifications(c, id)
}

func ReadAllRequestTicketNotifications(c *gin.Context) { readRequestTicketNotifications(c, 0) }

func readRequestTicketNotifications(c *gin.Context, eventID int64) {
	tenantID, ok := requestTicketScope(c)
	if !ok {
		return
	}
	if err := model.ReadRequestTicketNotifications(model.DB.WithContext(c.Request.Context()), tenantID, c.GetInt("id"), eventID); err != nil {
		requestTicketError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{})
}
