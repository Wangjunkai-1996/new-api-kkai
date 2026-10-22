package service

import (
	"io"
	"net"
	"net/textproto"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type requestTicketEmailMessage struct {
	recipients []string
	body       string
	err        error
}

func requestTicketEmailFixture(t *testing.T) (*gorm.DB, net.Listener, <-chan requestTicketEmailMessage) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := listener.Addr().(*net.TCPAddr)
	previousDB, previousHost, previousPort := model.DB, common.SMTPServer, common.SMTPPort
	previousAccount, previousFrom, previousToken := common.SMTPAccount, common.SMTPFrom, common.SMTPToken
	previousSSL, previousStartTLS := common.SMTPSSLEnabled, common.SMTPStartTLSEnabled
	model.DB, common.SMTPServer, common.SMTPPort = db, address.IP.String(), address.Port
	common.SMTPAccount, common.SMTPFrom, common.SMTPToken = "", "sender@example.com", ""
	common.SMTPSSLEnabled, common.SMTPStartTLSEnabled = false, false
	t.Cleanup(func() {
		_ = listener.Close()
		model.DB, common.SMTPServer, common.SMTPPort = previousDB, previousHost, previousPort
		common.SMTPAccount, common.SMTPFrom, common.SMTPToken = previousAccount, previousFrom, previousToken
		common.SMTPSSLEnabled, common.SMTPStartTLSEnabled = previousSSL, previousStartTLS
		require.NoError(t, sqlDB.Close())
	})
	messages := make(chan requestTicketEmailMessage, 1)
	go func() {
		var message requestTicketEmailMessage
		defer func() { messages <- message }()
		connection, err := listener.Accept()
		if err != nil {
			message.err = err
			return
		}
		defer connection.Close()
		_ = connection.SetDeadline(time.Now().Add(3 * time.Second))
		protocol := textproto.NewConn(connection)
		message.err = protocol.PrintfLine("220 localhost ESMTP")
		for message.err == nil {
			var command string
			command, message.err = protocol.ReadLine()
			if message.err != nil {
				return
			}
			switch {
			case strings.HasPrefix(command, "EHLO "), strings.HasPrefix(command, "HELO "), strings.HasPrefix(command, "MAIL FROM:"):
				message.err = protocol.PrintfLine("250 OK")
			case strings.HasPrefix(command, "RCPT TO:"):
				message.recipients = append(message.recipients, strings.TrimPrefix(command, "RCPT TO:"))
				message.err = protocol.PrintfLine("250 OK")
			case command == "DATA":
				message.err = protocol.PrintfLine("354 Send data")
				if message.err != nil {
					return
				}
				var body []byte
				body, message.err = io.ReadAll(protocol.DotReader())
				message.body = string(body)
				if message.err == nil {
					message.err = protocol.PrintfLine("250 OK")
				}
			case command == "QUIT":
				message.err = protocol.PrintfLine("221 Bye")
				return
			default:
				message.err = protocol.PrintfLine("502 Unsupported command")
			}
		}
	}()
	return db, listener, messages
}

func TestRequestTicketEmailUsesOnlyOwnerAndEscapesFeedback(t *testing.T) {
	db, _, messages := requestTicketEmailFixture(t)
	owner := model.User{Id: 7, TenantID: 0, Username: "ticket-owner", Email: "owner@example.com"}
	require.NoError(t, db.Create(&owner).Error)
	ticket := model.RequestTicket{ID: 11, UserID: owner.Id, Title: `<img src=x>`, Status: model.RequestTicketAccepted}
	NotifyRequestTicketUpdate(ticket, "<script>alert(1)</script>\n第二行")
	message := <-messages
	require.NoError(t, message.err)
	assert.Equal(t, []string{"<" + owner.Email + ">"}, message.recipients)
	assert.Contains(t, message.body, "工单 #"+strconv.FormatInt(ticket.ID, 10))
	assert.Contains(t, message.body, "&lt;img src=x&gt;")
	assert.Contains(t, message.body, "&lt;script&gt;alert(1)&lt;/script&gt;<br>第二行")
	assert.NotContains(t, message.body, "<script>")
}

func TestRequestTicketEmailRejectsWrongTenantAndUnsafeRecipients(t *testing.T) {
	for _, test := range []struct {
		name     string
		email    string
		tenantID int64
	}{
		{"wrong tenant", "owner@example.com", 42},
		{"recipient list", "owner@example.com;other@example.com", 0},
		{"header injection", "owner@example.com\r\nBcc: other@example.com", 0},
		{"comma list", "owner@example.com,other@example.com", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, listener, messages := requestTicketEmailFixture(t)
			require.NoError(t, db.Create(&model.User{Id: 7, Username: "owner", Email: test.email}).Error)
			NotifyRequestTicketUpdate(model.RequestTicket{ID: 11, TenantID: test.tenantID, UserID: 7, Status: model.RequestTicketAccepted}, "private feedback")
			require.NoError(t, listener.Close())
			message := <-messages
			assert.ErrorIs(t, message.err, net.ErrClosed, "must not connect to SMTP for another tenant or unsafe recipient")
			assert.Empty(t, message.recipients)
			assert.Empty(t, message.body)
		})
	}
}
