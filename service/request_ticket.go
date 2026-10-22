package service

import (
	"context"
	"fmt"
	"html"
	"net/mail"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// In-site events are authoritative; SMTP is intentionally best effort and is
// never part of the ticket transaction or its successful HTTP response.
func NotifyRequestTicketUpdate(ticket model.RequestTicket, feedback string) {
	if common.SMTPServer == "" || model.DB == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var user model.User
	if err := model.DB.WithContext(ctx).Select("email").Where("id = ? AND tenant_id = ?", ticket.UserID, ticket.TenantID).First(&user).Error; err != nil {
		common.SysError(fmt.Sprintf("request ticket %d email recipient lookup failed: %v", ticket.ID, err))
		return
	}
	if user.Email == "" {
		return
	}
	// The shared SMTP helper accepts recipient lists. Ticket feedback belongs to
	// exactly one account, including when an old/admin-edited email is malformed.
	recipient, err := mail.ParseAddress(user.Email)
	if err != nil || recipient.Address != user.Email || strings.ContainsAny(user.Email, "\r\n;") {
		common.SysError(fmt.Sprintf("request ticket %d has an invalid email recipient", ticket.ID))
		return
	}
	statusLabel := map[string]string{
		model.RequestTicketAccepted: "已受理", model.RequestTicketScheduling: "排期中",
		model.RequestTicketDeveloping: "开发中", model.RequestTicketCompleted: "已完成", model.RequestTicketRejected: "未采纳",
	}[ticket.Status]
	content := fmt.Sprintf("<p>你的需求工单 #%d「%s」有了新的反馈。</p><p>当前状态：%s</p><p>%s</p><p>登录站内通知查看完整工单动态。</p>", ticket.ID, html.EscapeString(ticket.Title), html.EscapeString(statusLabel), strings.ReplaceAll(html.EscapeString(strings.TrimSpace(feedback)), "\n", "<br>"))
	if err := common.SendEmail("需求工单有新进展", recipient.Address, content); err != nil {
		common.SysError(fmt.Sprintf("request ticket %d email delivery failed: %v", ticket.ID, err))
	}
}
