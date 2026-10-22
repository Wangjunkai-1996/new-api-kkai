package controller

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func requestTicketControllerDB(t *testing.T) *gorm.DB {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.RequestTicket{}, &model.RequestTicketEvent{}, &model.User{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	previousDB, previousSMTP := model.DB, common.SMTPServer
	model.DB, common.SMTPServer = db, ""
	t.Cleanup(func() {
		model.DB, common.SMTPServer = previousDB, previousSMTP
		require.NoError(t, sqlDB.Close())
	})
	return db
}

func requestTicketHTTP(handler gin.HandlerFunc, method, path, body string, tenantID int64, userID int, id int64) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Request = httptest.NewRequest(method, path, strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("id", userID)
	common.SetContextKey(ctx, constant.ContextKeyTenantContext, model.TenantContext{TenantID: tenantID})
	if id > 0 {
		ctx.Params = gin.Params{{Key: "id", Value: strconv.FormatInt(id, 10)}}
	}
	handler(ctx)
	return response
}

func TestRequestTicketHTTPDetailConflictAndOwnership(t *testing.T) {
	requestTicketControllerDB(t)
	created := requestTicketHTTP(CreateRequestTicket, http.MethodPost, "/api/requests", `{"title":"  改进搜索  ","description":"  希望可以按时间筛选  "}`, 0, 7, 0)
	require.Equal(t, http.StatusOK, created.Code, created.Body.String())
	var result struct {
		Success bool                      `json:"success"`
		Data    model.RequestTicketDetail `json:"data"`
	}
	require.NoError(t, common.Unmarshal(created.Body.Bytes(), &result))
	require.True(t, result.Success)
	assert.Equal(t, "改进搜索", result.Data.Ticket.Title)
	assert.Equal(t, "希望可以按时间筛选", result.Data.Ticket.Description)
	require.Len(t, result.Data.Events, 1)
	assert.Zero(t, result.Data.Ticket.UnreadCount)
	id := result.Data.Ticket.ID
	transitioned := requestTicketHTTP(AdminTransitionRequestTicket, http.MethodPatch, "/api/requests/admin/1/status", `{"status":"accepted","feedback":"已受理，会安排评估。","version":1}`, 0, 1, id)
	require.Equal(t, http.StatusOK, transitioned.Code, transitioned.Body.String())
	require.NoError(t, common.Unmarshal(transitioned.Body.Bytes(), &result))
	assert.Equal(t, model.RequestTicketAccepted, result.Data.Ticket.Status)
	assert.EqualValues(t, 2, result.Data.Ticket.Version)
	require.Len(t, result.Data.Events, 2)
	assert.Equal(t, "已受理，会安排评估。", result.Data.Events[1].Feedback)
	assert.Zero(t, result.Data.Events[1].ReadAt)

	stale := requestTicketHTTP(AdminTransitionRequestTicket, http.MethodPatch, "/api/requests/admin/1/status", `{"status":"rejected","feedback":"另一位管理员的旧操作","version":1}`, 0, 2, id)
	assert.Equal(t, http.StatusConflict, stale.Code)
	assert.Contains(t, stale.Body.String(), `"code":"REQUEST_TICKET_CONFLICT"`)

	for _, identity := range []struct {
		tenantID int64
		userID   int
		status   int
	}{{0, 8, http.StatusNotFound}, {42, 7, http.StatusForbidden}, {0, 0, http.StatusForbidden}} {
		response := requestTicketHTTP(GetRequestTicket, http.MethodGet, "/api/requests/1", "", identity.tenantID, identity.userID, id)
		assert.Equal(t, identity.status, response.Code)
	}
	detail := requestTicketHTTP(GetRequestTicket, http.MethodGet, "/api/requests/1", "", 0, 7, id)
	require.Equal(t, http.StatusOK, detail.Code)
	require.NoError(t, common.Unmarshal(detail.Body.Bytes(), &result))
	assert.EqualValues(t, 1, result.Data.Ticket.UnreadCount, "reading a detail does not mark it read")
	otherUserRead := requestTicketHTTP(ReadRequestTicketNotification, http.MethodPost, "/api/requests/notifications/2/read", "", 0, 8, result.Data.Events[1].ID)
	assert.Equal(t, http.StatusNotFound, otherUserRead.Code)
	read := requestTicketHTTP(ReadRequestTicketNotification, http.MethodPost, "/api/requests/notifications/2/read", "", 0, 7, result.Data.Events[1].ID)
	assert.Equal(t, http.StatusOK, read.Code)
}

func TestRequestTicketHTTPListAndValidation(t *testing.T) {
	db := requestTicketControllerDB(t)
	_, err := model.CreateRequestTicket(db, 0, 7, "标题", "描述")
	require.NoError(t, err)
	_, err = model.CreateRequestTicket(db, 0, 8, "另一个用户", "私有描述")
	require.NoError(t, err)
	for _, list := range []struct {
		handler gin.HandlerFunc
		total   int
	}{{ListRequestTickets, 1}, {AdminListRequestTickets, 2}} {
		response := requestTicketHTTP(list.handler, http.MethodGet, "/api/requests?p=1&page_size=1", "", 0, 7, 0)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		var result struct {
			Data struct {
				Items    []model.RequestTicketSummary `json:"items"`
				Total    int                          `json:"total"`
				Page     int                          `json:"page"`
				PageSize int                          `json:"page_size"`
			} `json:"data"`
		}
		require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
		assert.Len(t, result.Data.Items, 1)
		assert.Equal(t, 1, result.Data.Page)
		assert.Equal(t, 1, result.Data.PageSize)
		assert.Equal(t, list.total, result.Data.Total)
	}
	for _, endpoint := range []struct {
		handler gin.HandlerFunc
		path    string
		body    string
	}{
		{CreateRequestTicket, "/api/requests", `{"title":"","description":"描述"}`},
		{CreateRequestTicket, "/api/requests", `{"title":"标题","description":"` + strings.Repeat("a", 33<<10) + `"}`},
		{CreateRequestTicket, "/api/requests", `{`},
		{CreateRequestTicket, "/api/requests", `{"title":"标题","description":"描述"} {"title":"trailing"}`},
		{ListRequestTickets, "/api/requests?p=-1", ""},
		{ListRequestTickets, "/api/requests?page_size=-1", ""},
		{ListRequestTickets, "/api/requests?status=unexpected", ""},
		{AdminTransitionRequestTicket, "/api/requests/admin/1/status", `{"status":"accepted","feedback":" ","version":1}`},
	} {
		response := requestTicketHTTP(endpoint.handler, http.MethodPost, endpoint.path, endpoint.body, 0, 7, 1)
		assert.Equal(t, http.StatusBadRequest, response.Code, endpoint.path+response.Body.String())
	}
	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/requests", nil)
	ctx.Set("id", 7)
	ListRequestTickets(ctx)
	assert.Equal(t, http.StatusForbidden, response.Code, "missing trusted tenant context fails closed")
}
