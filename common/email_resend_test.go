package common

import (
	"io"
	"net/http"
	"net/mail"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type resendTestTransport func(*http.Request) (*http.Response, error)

func (f resendTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestResendPreservesTenantSenderAndDoesNotFallback(t *testing.T) {
	previousClient := resendEmailClient
	OptionMapRWMutex.Lock()
	previousOptions := OptionMap
	OptionMap = map[string]string{"EmailProvider": "resend", "ResendApiKey": "test-key", "ResendFrom": "Platform <sender@example.com>"}
	OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		resendEmailClient = previousClient
		OptionMapRWMutex.Lock()
		OptionMap = previousOptions
		OptionMapRWMutex.Unlock()
	})
	for _, status := range []int{http.StatusOK, http.StatusTooManyRequests} {
		calls := 0
		resendEmailClient = &http.Client{Transport: resendTestTransport(func(r *http.Request) (*http.Response, error) {
			calls++
			assert.Equal(t, "https://api.resend.com/emails", r.URL.String())
			var payload struct{ From string }
			require.NoError(t, DecodeJson(r.Body, &payload))
			sender, err := mail.ParseAddress(payload.From)
			require.NoError(t, err)
			assert.Equal(t, "代理站 & Company", sender.Name)
			assert.Equal(t, "sender@example.com", sender.Address)
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(`{"id":"mail-test"}`)), Header: make(http.Header)}, nil
		})}
		err := SendEmailWithSenderName("Verification", "receiver@example.com", "<p>test</p>", "代理站 & Company")
		if status == http.StatusOK {
			require.NoError(t, err)
		} else {
			require.Error(t, err)
		}
		assert.Equal(t, 1, calls)
	}
}
