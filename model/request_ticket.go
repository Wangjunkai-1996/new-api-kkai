package model

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
)

const (
	RequestTicketPending    = "pending"
	RequestTicketAccepted   = "accepted"
	RequestTicketScheduling = "scheduling"
	RequestTicketDeveloping = "developing"
	RequestTicketCompleted  = "completed"
	RequestTicketRejected   = "rejected"
)

var (
	ErrRequestTicketInvalid    = errors.New("invalid request ticket input")
	ErrRequestTicketConflict   = errors.New("the request ticket has changed; refresh and try again")
	ErrRequestTicketTransition = errors.New("invalid request ticket status transition")
)

type RequestTicket struct {
	ID          int64  `json:"id" gorm:"primaryKey;autoIncrement"`
	TenantID    int64  `json:"tenant_id" gorm:"not null;index:idx_request_tickets_owner,priority:1;index:idx_request_tickets_status,priority:1"`
	UserID      int    `json:"user_id" gorm:"not null;index:idx_request_tickets_owner,priority:2"`
	Title       string `json:"title" gorm:"type:varchar(80);not null"`
	Description string `json:"description" gorm:"type:text;not null"`
	Status      string `json:"status" gorm:"type:varchar(16);not null;index:idx_request_tickets_status,priority:2"`
	Version     int64  `json:"version" gorm:"not null"`
	CreatedAt   int64  `json:"created_at" gorm:"not null;autoCreateTime:false"`
	UpdatedAt   int64  `json:"updated_at" gorm:"not null;autoUpdateTime:false;index:idx_request_tickets_owner,priority:3;index:idx_request_tickets_status,priority:3"`
}

// Events are also the durable in-site inbox. A submitted event is already read;
// every administrator transition creates a new unread event in the same commit.
type RequestTicketEvent struct {
	ID        int64  `json:"id" gorm:"primaryKey;autoIncrement;index:idx_request_ticket_events_timeline,priority:3;index:idx_request_ticket_events_unread,priority:4"`
	TenantID  int64  `json:"-" gorm:"not null;index:idx_request_ticket_events_timeline,priority:1;index:idx_request_ticket_events_unread,priority:1"`
	TicketID  int64  `json:"ticket_id" gorm:"not null;index:idx_request_ticket_events_timeline,priority:2"`
	UserID    int    `json:"-" gorm:"not null;index:idx_request_ticket_events_unread,priority:2"`
	ActorID   int    `json:"actor_id" gorm:"not null"`
	Status    string `json:"status" gorm:"type:varchar(16);not null"`
	Feedback  string `json:"feedback" gorm:"type:text;not null"`
	CreatedAt int64  `json:"created_at" gorm:"not null;autoCreateTime:false"`
	ReadAt    int64  `json:"read_at" gorm:"not null;index:idx_request_ticket_events_unread,priority:3"`
}

type RequestTicketSummary struct {
	RequestTicket
	LatestFeedback string `json:"latest_feedback"`
	UnreadCount    int64  `json:"unread_count"`
	Username       string `json:"username,omitempty"`
}

type RequestTicketDetail struct {
	Ticket RequestTicketSummary `json:"ticket"`
	Events []RequestTicketEvent `json:"events"`
}

type RequestTicketNotification struct {
	RequestTicketEvent
	EventID int64  `json:"event_id"`
	Title   string `json:"title"`
}

func CreateRequestTicket(db *gorm.DB, tenantID int64, userID int, title, description string) (*RequestTicket, error) {
	title, description = strings.TrimSpace(title), strings.TrimSpace(description)
	if tenantID < 0 || userID <= 0 || !utf8.ValidString(title) || !utf8.ValidString(description) || title == "" || description == "" || utf8.RuneCountInString(title) > 80 || utf8.RuneCountInString(description) > 2000 {
		return nil, ErrRequestTicketInvalid
	}
	now := time.Now().Unix()
	ticket := RequestTicket{TenantID: tenantID, UserID: userID, Title: title, Description: description, Status: RequestTicketPending, Version: 1, CreatedAt: now, UpdatedAt: now}
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&ticket).Error; err != nil {
			return err
		}
		return tx.Create(&RequestTicketEvent{TenantID: tenantID, TicketID: ticket.ID, UserID: userID, ActorID: userID, Status: RequestTicketPending, CreatedAt: now, ReadAt: now}).Error
	})
	return &ticket, err
}

func TransitionRequestTicket(db *gorm.DB, tenantID, ticketID int64, actorID int, version int64, status, feedback string) (*RequestTicket, error) {
	feedback = strings.TrimSpace(feedback)
	if tenantID < 0 || ticketID <= 0 || actorID <= 0 || version <= 0 || feedback == "" || !utf8.ValidString(feedback) || utf8.RuneCountInString(feedback) > 2000 {
		return nil, ErrRequestTicketInvalid
	}
	var ticket RequestTicket
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).Where("tenant_id = ? AND id = ?", tenantID, ticketID).First(&ticket).Error; err != nil {
			return err
		}
		if ticket.Version != version {
			return ErrRequestTicketConflict
		}
		valid := (ticket.Status == RequestTicketPending && (status == RequestTicketAccepted || status == RequestTicketRejected)) ||
			(ticket.Status == RequestTicketAccepted && (status == RequestTicketScheduling || status == RequestTicketDeveloping || status == RequestTicketCompleted)) ||
			(ticket.Status == RequestTicketScheduling && (status == RequestTicketDeveloping || status == RequestTicketCompleted)) ||
			(ticket.Status == RequestTicketDeveloping && status == RequestTicketCompleted)
		if !valid {
			return ErrRequestTicketTransition
		}
		now := time.Now().Unix()
		updated := tx.Model(&RequestTicket{}).Where("tenant_id = ? AND id = ? AND version = ?", tenantID, ticketID, version).
			Updates(map[string]any{"status": status, "version": version + 1, "updated_at": now})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrRequestTicketConflict
		}
		ticket.Status, ticket.Version, ticket.UpdatedAt = status, version+1, now
		return tx.Create(&RequestTicketEvent{TenantID: tenantID, TicketID: ticket.ID, UserID: ticket.UserID, ActorID: actorID, Status: status, Feedback: feedback, CreatedAt: now}).Error
	})
	return &ticket, err
}

// The correlated subqueries use the owner/timeline indexes and keep a list page
// to a fixed number of queries, without fetching each ticket's whole timeline.
func requestTicketSummaryQuery(db *gorm.DB, tenantID int64, userID int) *gorm.DB {
	query := db.Model(&RequestTicket{}).Where("request_tickets.tenant_id = ?", tenantID).Select(`request_tickets.*,
		COALESCE((SELECT feedback FROM request_ticket_events e WHERE e.tenant_id = request_tickets.tenant_id AND e.ticket_id = request_tickets.id AND e.status <> 'pending' ORDER BY e.id DESC LIMIT 1), '') AS latest_feedback,
		(SELECT COUNT(*) FROM request_ticket_events e WHERE e.tenant_id = request_tickets.tenant_id AND e.ticket_id = request_tickets.id AND e.read_at = 0 AND e.status <> 'pending') AS unread_count,
		COALESCE((SELECT username FROM users u WHERE u.id = request_tickets.user_id AND u.tenant_id = request_tickets.tenant_id AND u.deleted_at IS NULL), '') AS username`)
	if userID > 0 {
		query = query.Where("request_tickets.user_id = ?", userID)
	}
	return query
}

// userID=0 is reserved for a route already authorized with AdminAuth.
func ListRequestTickets(db *gorm.DB, tenantID int64, userID int, status string, offset, limit int) ([]RequestTicketSummary, int64, error) {
	if tenantID < 0 || userID < 0 || offset < 0 || limit < 1 || limit > 100 {
		return nil, 0, ErrRequestTicketInvalid
	}
	query := requestTicketSummaryQuery(db, tenantID, userID)
	switch status {
	case "", "all":
	case "active":
		query = query.Where("request_tickets.status IN ?", []string{RequestTicketPending, RequestTicketAccepted, RequestTicketScheduling, RequestTicketDeveloping})
	case RequestTicketPending, RequestTicketAccepted, RequestTicketScheduling, RequestTicketDeveloping, RequestTicketCompleted, RequestTicketRejected:
		query = query.Where("request_tickets.status = ?", status)
	default:
		return nil, 0, ErrRequestTicketInvalid
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	items := make([]RequestTicketSummary, 0)
	err := query.Order("request_tickets.updated_at DESC, request_tickets.id DESC").Offset(offset).Limit(limit).Find(&items).Error
	return items, total, err
}

func GetRequestTicketDetail(db *gorm.DB, tenantID int64, userID int, ticketID int64) (*RequestTicketDetail, error) {
	if tenantID < 0 || userID < 0 || ticketID <= 0 {
		return nil, ErrRequestTicketInvalid
	}
	detail := RequestTicketDetail{Events: make([]RequestTicketEvent, 0)}
	if err := requestTicketSummaryQuery(db, tenantID, userID).Where("request_tickets.id = ?", ticketID).First(&detail.Ticket).Error; err != nil {
		return nil, err
	}
	// Each committed version has one event. Bound the timeline to the selected
	// version so a concurrent transition cannot append a newer status to it.
	err := db.Where("tenant_id = ? AND ticket_id = ? AND user_id = ?", tenantID, ticketID, detail.Ticket.UserID).
		Order("id ASC").Limit(int(detail.Ticket.Version)).Find(&detail.Events).Error
	return &detail, err
}

func ListRequestTicketNotifications(db *gorm.DB, tenantID int64, userID, offset, limit int) ([]RequestTicketNotification, int64, int64, error) {
	if tenantID < 0 || userID <= 0 || offset < 0 || limit < 1 || limit > 100 {
		return nil, 0, 0, ErrRequestTicketInvalid
	}
	query := db.Model(&RequestTicketEvent{}).Where("request_ticket_events.tenant_id = ? AND request_ticket_events.user_id = ? AND request_ticket_events.status <> ?", tenantID, userID, RequestTicketPending)
	var total, unread int64
	if err := query.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, 0, err
	}
	if err := query.Session(&gorm.Session{}).Where("request_ticket_events.read_at = 0").Count(&unread).Error; err != nil {
		return nil, 0, 0, err
	}
	items := make([]RequestTicketNotification, 0)
	err := query.Select("request_ticket_events.*, request_ticket_events.id AS event_id, request_tickets.title").
		Joins("JOIN request_tickets ON request_tickets.id = request_ticket_events.ticket_id AND request_tickets.tenant_id = request_ticket_events.tenant_id AND request_tickets.user_id = request_ticket_events.user_id").
		Order("request_ticket_events.id DESC").Offset(offset).Limit(limit).Find(&items).Error
	return items, total, unread, err
}

// eventID=0 marks all events existing at this request's snapshot. Newer events
// remain unread even when they commit between the MAX(id) and UPDATE queries.
func ReadRequestTicketNotifications(db *gorm.DB, tenantID int64, userID int, eventID int64) error {
	if tenantID < 0 || userID <= 0 || eventID < 0 {
		return ErrRequestTicketInvalid
	}
	query := db.Model(&RequestTicketEvent{}).Where("tenant_id = ? AND user_id = ? AND status <> ?", tenantID, userID, RequestTicketPending)
	if eventID > 0 {
		var event RequestTicketEvent
		if err := query.Session(&gorm.Session{}).Where("id = ?", eventID).First(&event).Error; err != nil {
			return err
		}
		query = query.Where("id = ?", eventID)
	} else {
		var cutoff int64
		if err := query.Session(&gorm.Session{}).Select("COALESCE(MAX(id), 0)").Scan(&cutoff).Error; err != nil {
			return err
		}
		query = query.Where("id <= ?", cutoff)
	}
	return query.Where("read_at = 0").Update("read_at", time.Now().Unix()).Error
}
