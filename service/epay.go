package service

import (
	"errors"
	"net/url"
	"os"
	"strings"

	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

func GetCallbackAddress() string {
	if operation_setting.CustomCallbackAddress == "" {
		return system_setting.ServerAddress
	}
	return operation_setting.CustomCallbackAddress
}

// MembershipNotifyURL only changes machine notifications. Browser return URLs
// and already persisted checkout forms retain their original destinations.
func MembershipNotifyURL() (*url.URL, error) {
	base := strings.TrimSpace(os.Getenv("KKAI_MEMBERSHIP_NOTIFY_ADDRESS"))
	if base == "" {
		base = GetCallbackAddress()
	}
	address, err := url.Parse(base)
	if err != nil || address.Scheme != "https" || address.Host == "" || address.User != nil || address.RawQuery != "" || address.Fragment != "" || strings.Trim(address.Path, "/") != "" {
		return nil, errors.New("membership notification address must be an HTTPS origin")
	}
	address.Path = "/api/membership/epay/notify"
	return address, nil
}
