package controller

import (
	"fmt"
	"html"
	"io"
	"mime"
	"net"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"net/textproto"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func captureAccountEmail(t *testing.T, send func()) *mail.Message {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	host, port, err := net.SplitHostPort(listener.Addr().String())
	require.NoError(t, err)
	common.SMTPServer = host
	common.SMTPPort, err = strconv.Atoi(port)
	require.NoError(t, err)
	type delivery struct {
		data []byte
		err  error
	}
	result := make(chan delivery, 1)
	go func() {
		var output delivery
		defer func() { result <- output }()
		conn, err := listener.Accept()
		if err != nil {
			output.err = err
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		server := textproto.NewConn(conn)
		output.err = server.PrintfLine("220 localhost ESMTP")
		for output.err == nil {
			line, err := server.ReadLine()
			if err != nil {
				output.err = err
				return
			}
			switch {
			case line == "DATA":
				if output.err = server.PrintfLine("354 send message"); output.err != nil {
					return
				}
				output.data, output.err = io.ReadAll(server.DotReader())
				if output.err != nil {
					return
				}
				output.err = server.PrintfLine("250 accepted")
			case line == "QUIT":
				output.err = server.PrintfLine("221 bye")
				return
			default:
				output.err = server.PrintfLine("250 localhost")
			}
		}
	}()
	send()
	select {
	case output := <-result:
		require.NoError(t, output.err)
		message, err := mail.ReadMessage(strings.NewReader(string(output.data)))
		require.NoError(t, err)
		return message
	case <-time.After(6 * time.Second):
		t.Fatal("account email was not delivered to loopback SMTP")
		return nil
	}
}

func TestTenantAccountEmailsUseSiteBranding(t *testing.T) {
	previousDB, previousName := model.DB, common.SystemName
	previousServer, previousPort, previousAccount, previousFrom, previousToken := common.SMTPServer, common.SMTPPort, common.SMTPAccount, common.SMTPFrom, common.SMTPToken
	previousSSL, previousStartTLS := common.SMTPSSLEnabled, common.SMTPStartTLSEnabled
	previousDomains, previousAliases := common.EmailDomainRestrictionEnabled, common.EmailAliasRestrictionEnabled
	previousAddress := system_setting.ServerAddress
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Tenant{}, &model.TenantProfile{}))
	model.DB, common.SystemName = db, "Platform Mail"
	common.SMTPAccount, common.SMTPToken, common.SMTPFrom = "", "", "sender@example.com"
	common.SMTPSSLEnabled, common.SMTPStartTLSEnabled = false, false
	common.EmailDomainRestrictionEnabled, common.EmailAliasRestrictionEnabled = false, false
	system_setting.ServerAddress = "https://platform.example.com"
	t.Cleanup(func() {
		model.DB, common.SystemName = previousDB, previousName
		common.SMTPServer, common.SMTPPort, common.SMTPAccount, common.SMTPFrom, common.SMTPToken = previousServer, previousPort, previousAccount, previousFrom, previousToken
		common.SMTPSSLEnabled, common.SMTPStartTLSEnabled = previousSSL, previousStartTLS
		common.EmailDomainRestrictionEnabled, common.EmailAliasRestrictionEnabled = previousDomains, previousAliases
		system_setting.ServerAddress = previousAddress
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.Create(&model.Tenant{ID: 42, Status: model.TenantStatusActive, DisplayName: "Tenant Fallback"}).Error)
	require.NoError(t, db.Create(&model.TenantProfile{TenantID: 42, SiteName: "代理站 & Company"}).Error)
	for _, tenantID := range []int64{42, model.PlatformTenantID} {
		for _, reset := range []bool{false, true} {
			t.Run(fmt.Sprintf("tenant=%d/reset=%v", tenantID, reset), func(t *testing.T) {
				email := fmt.Sprintf("customer-%d-%v@example.com", tenantID, reset)
				if reset {
					username := fmt.Sprintf("customer-%d", tenantID)
					require.NoError(t, db.Create(&model.User{TenantID: tenantID, Username: username, NormalizedUsername: &username, Email: email, NormalizedEmail: &email, AffCode: username}).Error)
				}
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodGet, "https://tenant.example.com/api/verification?email="+email, nil)
				common.SetContextKey(c, constant.ContextKeyTenantContext, model.TenantContext{TenantID: tenantID, Hostname: "tenant.example.com"})
				purpose := common.EmailVerificationPurpose
				if reset {
					purpose = common.PasswordResetPurpose
				}
				t.Cleanup(func() { common.DeleteKey(tenantEmailVerificationKey(c, email), purpose) })
				message := captureAccountEmail(t, func() {
					if reset {
						SendPasswordResetEmail(c)
					} else {
						SendEmailVerification(c)
					}
				})
				assert.Contains(t, recorder.Body.String(), `"success":true`)
				wantName, wantHost := "Platform Mail", "platform.example.com"
				if tenantID > model.PlatformTenantID {
					wantName, wantHost = "代理站 & Company", "tenant.example.com"
				}
				from, err := message.Header.AddressList("From")
				require.NoError(t, err)
				require.Len(t, from, 1)
				assert.Equal(t, wantName, from[0].Name)
				subject, err := new(mime.WordDecoder).DecodeHeader(message.Header.Get("Subject"))
				require.NoError(t, err)
				assert.Contains(t, subject, wantName)
				content, err := io.ReadAll(message.Body)
				require.NoError(t, err)
				assert.Contains(t, string(content), html.EscapeString(wantName))
				if reset {
					assert.Contains(t, string(content), "https://"+wantHost+"/user/reset?")
				}
				if tenantID > model.PlatformTenantID {
					assert.NotContains(t, string(content), "Platform Mail")
					assert.NotContains(t, string(content), "platform.example.com")
				}
				assert.Equal(t, "Platform Mail", common.SystemName)
			})
		}
	}
}
