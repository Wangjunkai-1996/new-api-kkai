package model

import (
	"errors"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func requestTicketTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&RequestTicket{}, &RequestTicketEvent{}, &User{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	return db
}

func TestRequestTicketLifecycleAndDurableInbox(t *testing.T) {
	db := requestTicketTestDB(t)
	ticket, err := CreateRequestTicket(db, 0, 7, "截图功能", "希望支持截图")
	require.NoError(t, err)
	items, total, unread, err := ListRequestTicketNotifications(db, 0, 7, 0, 20)
	require.NoError(t, err)
	assert.Empty(t, items)
	assert.Zero(t, total)
	assert.Zero(t, unread)
	for _, status := range []string{RequestTicketAccepted, RequestTicketScheduling, RequestTicketDeveloping, RequestTicketCompleted} {
		ticket, err = TransitionRequestTicket(db, 0, ticket.ID, 1, ticket.Version, status, "已收到，进展："+status)
		require.NoError(t, err)
	}
	detail, err := GetRequestTicketDetail(db, 0, 7, ticket.ID)
	require.NoError(t, err)
	assert.Len(t, detail.Events, 5)
	assert.EqualValues(t, 4, detail.Ticket.UnreadCount)
	assert.Equal(t, "已收到，进展：completed", detail.Ticket.LatestFeedback)
	listed, listedTotal, err := ListRequestTickets(db, 0, 7, RequestTicketCompleted, 0, 20)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.EqualValues(t, 1, listedTotal)
	assert.Equal(t, detail.Ticket, listed[0])
	active, activeTotal, err := ListRequestTickets(db, 0, 0, "active", 0, 20)
	require.NoError(t, err)
	assert.Empty(t, active)
	assert.Zero(t, activeTotal)
	items, total, unread, err = ListRequestTicketNotifications(db, 0, 7, 0, 20)
	require.NoError(t, err)
	assert.EqualValues(t, 4, total)
	assert.EqualValues(t, 4, unread, "listing notifications and viewing detail must not mark read")
	require.Len(t, items, 4)
	assert.Equal(t, items[0].ID, items[0].EventID)
	assert.Equal(t, ticket.Title, items[0].Title)
	require.NoError(t, ReadRequestTicketNotifications(db, 0, 7, items[0].ID))
	require.NoError(t, ReadRequestTicketNotifications(db, 0, 7, items[0].ID), "mark-read is idempotent")
	_, _, unread, err = ListRequestTicketNotifications(db, 0, 7, 0, 20)
	require.NoError(t, err)
	assert.EqualValues(t, 3, unread)
	require.NoError(t, ReadRequestTicketNotifications(db, 0, 7, 0))
	_, _, unread, err = ListRequestTicketNotifications(db, 0, 7, 0, 20)
	require.NoError(t, err)
	assert.Zero(t, unread)
	_, err = TransitionRequestTicket(db, 0, ticket.ID, 1, ticket.Version, RequestTicketRejected, "不能回退")
	assert.ErrorIs(t, err, ErrRequestTicketTransition)
}

func TestRequestTicketIsolationValidationAndConflicts(t *testing.T) {
	db := requestTicketTestDB(t)
	_, err := CreateRequestTicket(db, 0, 7, strings.Repeat("图", 81), "内容")
	assert.ErrorIs(t, err, ErrRequestTicketInvalid)
	_, err = CreateRequestTicket(db, 0, 7, "标题", strings.Repeat("图", 2001))
	assert.ErrorIs(t, err, ErrRequestTicketInvalid)
	ticket, err := CreateRequestTicket(db, 0, 7, strings.Repeat("图", 80), strings.Repeat("字", 2000))
	require.NoError(t, err, "limits are Unicode characters rather than bytes")
	_, err = TransitionRequestTicket(db, 0, ticket.ID, 1, 1, RequestTicketAccepted, "   ")
	assert.ErrorIs(t, err, ErrRequestTicketInvalid)
	_, err = TransitionRequestTicket(db, 0, ticket.ID, 1, 1, RequestTicketDeveloping, "跳过状态")
	assert.ErrorIs(t, err, ErrRequestTicketTransition)
	_, err = TransitionRequestTicket(db, 0, ticket.ID, 1, 1, RequestTicketRejected, "暂不采纳")
	require.NoError(t, err)
	_, err = TransitionRequestTicket(db, 0, ticket.ID, 1, 1, RequestTicketAccepted, "过期版本")
	assert.ErrorIs(t, err, ErrRequestTicketConflict)
	for _, identity := range []struct {
		tenant int64
		user   int
	}{{0, 8}, {42, 7}} {
		_, err = GetRequestTicketDetail(db, identity.tenant, identity.user, ticket.ID)
		assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
		items, total, err := ListRequestTickets(db, identity.tenant, identity.user, "all", 0, 20)
		require.NoError(t, err)
		assert.Empty(t, items)
		assert.Zero(t, total)
		_, total, unread, err := ListRequestTicketNotifications(db, identity.tenant, identity.user, 0, 20)
		require.NoError(t, err)
		assert.Zero(t, total)
		assert.Zero(t, unread)
		require.NoError(t, ReadRequestTicketNotifications(db, identity.tenant, identity.user, 0))
	}
	detail, err := GetRequestTicketDetail(db, 0, 7, ticket.ID)
	require.NoError(t, err)
	require.Len(t, detail.Events, 2, "failed transitions must not append timeline events")
	for _, identity := range []struct {
		tenant int64
		user   int
	}{{0, 8}, {42, 7}} {
		assert.ErrorIs(t, ReadRequestTicketNotifications(db, identity.tenant, identity.user, detail.Events[1].ID), gorm.ErrRecordNotFound)
	}
	assert.EqualValues(t, 1, detail.Ticket.UnreadCount)
}

func TestRequestTicketForwardTransitions(t *testing.T) {
	for _, test := range []struct {
		name    string
		path    []string
		target  string
		invalid bool
	}{
		{"accepted to developing", []string{RequestTicketAccepted}, RequestTicketDeveloping, false},
		{"accepted to completed", []string{RequestTicketAccepted}, RequestTicketCompleted, false},
		{"scheduling to completed", []string{RequestTicketAccepted, RequestTicketScheduling}, RequestTicketCompleted, false},
		{"same status", []string{RequestTicketAccepted}, RequestTicketAccepted, true},
		{"back to pending", []string{RequestTicketAccepted}, RequestTicketPending, true},
		{"back to accepted", []string{RequestTicketAccepted, RequestTicketScheduling}, RequestTicketAccepted, true},
		{"back to scheduling", []string{RequestTicketAccepted, RequestTicketDeveloping}, RequestTicketScheduling, true},
		{"reject after acceptance", []string{RequestTicketAccepted}, RequestTicketRejected, true},
		{"completed is final", []string{RequestTicketAccepted, RequestTicketCompleted}, RequestTicketDeveloping, true},
		{"rejected is final", []string{RequestTicketRejected}, RequestTicketAccepted, true},
		{"unknown status", []string{RequestTicketAccepted}, "unknown", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := requestTicketTestDB(t)
			ticket, err := CreateRequestTicket(db, 0, 7, "标题", "描述")
			require.NoError(t, err)
			for _, status := range test.path {
				ticket, err = TransitionRequestTicket(db, 0, ticket.ID, 1, ticket.Version, status, "此前进展")
				require.NoError(t, err)
			}
			_, err = TransitionRequestTicket(db, 0, ticket.ID, 1, ticket.Version, test.target, "  最新反馈  ")
			if test.invalid {
				require.ErrorIs(t, err, ErrRequestTicketTransition)
			} else {
				require.NoError(t, err)
			}
			detail, err := GetRequestTicketDetail(db, 0, 7, ticket.ID)
			require.NoError(t, err)
			if test.invalid {
				assert.Equal(t, ticket.Status, detail.Ticket.Status)
				assert.Equal(t, ticket.Version, detail.Ticket.Version)
				assert.Len(t, detail.Events, len(test.path)+1)
				assert.EqualValues(t, len(test.path), detail.Ticket.UnreadCount)
				return
			}
			assert.Equal(t, test.target, detail.Ticket.Status)
			assert.Equal(t, ticket.Version+1, detail.Ticket.Version)
			require.Len(t, detail.Events, len(test.path)+2, "only actual transitions appear in the timeline")
			event := detail.Events[len(detail.Events)-1]
			assert.Equal(t, test.target, event.Status)
			assert.Equal(t, "最新反馈", event.Feedback)
			assert.Zero(t, event.ReadAt)
			assert.EqualValues(t, len(test.path)+1, detail.Ticket.UnreadCount)
			assert.Equal(t, event.Feedback, detail.Ticket.LatestFeedback)
		})
	}
}

func TestRequestTicketEventFailureRollsBackCreateAndTransition(t *testing.T) {
	db := requestTicketTestDB(t)
	ticket, err := CreateRequestTicket(db, 0, 7, "标题", "描述")
	require.NoError(t, err)
	interrupted := errors.New("event storage unavailable")
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("test:fail-ticket-event", func(tx *gorm.DB) {
		if tx.Statement.Table == "request_ticket_events" {
			tx.AddError(interrupted)
		}
	}))
	_, err = TransitionRequestTicket(db, 0, ticket.ID, 1, ticket.Version, RequestTicketAccepted, "已受理")
	assert.ErrorIs(t, err, interrupted)
	_, err = CreateRequestTicket(db, 0, 7, "另一个", "事件失败")
	assert.ErrorIs(t, err, interrupted)
	detail, err := GetRequestTicketDetail(db, 0, 7, ticket.ID)
	require.NoError(t, err)
	assert.Equal(t, RequestTicketPending, detail.Ticket.Status)
	assert.EqualValues(t, 1, detail.Ticket.Version)
	assert.Len(t, detail.Events, 1)
	var count int64
	require.NoError(t, db.Model(&RequestTicket{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
}

func TestRequestTicketReadAllPreservesEventsAfterItsCutoff(t *testing.T) {
	db := requestTicketTestDB(t)
	ticket, err := CreateRequestTicket(db, 0, 7, "标题", "描述")
	require.NoError(t, err)
	_, err = TransitionRequestTicket(db, 0, ticket.ID, 1, ticket.Version, RequestTicketAccepted, "已受理")
	require.NoError(t, err)
	newEvent := RequestTicketEvent{TenantID: 0, TicketID: ticket.ID, UserID: 7, ActorID: 1, Status: RequestTicketScheduling, Feedback: "刚刚排期"}
	// Insert a newer notification after the cutoff SELECT and before its UPDATE.
	// The callback makes this interleaving deterministic without sleeps.
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("test:new-ticket-event", func(tx *gorm.DB) {
		if tx.Statement.Table == "request_ticket_events" {
			tx.AddError(tx.Session(&gorm.Session{NewDB: true}).Create(&newEvent).Error)
		}
	}))
	require.NoError(t, ReadRequestTicketNotifications(db, 0, 7, 0))
	require.NoError(t, db.Callback().Update().Remove("test:new-ticket-event"))
	var events []RequestTicketEvent
	require.NoError(t, db.Where("ticket_id = ? AND status <> ?", ticket.ID, RequestTicketPending).Order("id ASC").Find(&events).Error)
	require.Len(t, events, 2)
	assert.Positive(t, events[0].ReadAt)
	assert.Zero(t, events[1].ReadAt)
	assert.Equal(t, newEvent.ID, events[1].ID)
}
