package system_setting

import (
	"errors"
	"net"
	"net/url"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
	"golang.org/x/net/idna"
	"golang.org/x/net/publicsuffix"
)

type PasskeySettings struct {
	Enabled              bool   `json:"enabled"`
	RPDisplayName        string `json:"rp_display_name"`
	RPID                 string `json:"rp_id"`
	LegacyRPIDs          string `json:"legacy_rp_ids"`
	Origins              string `json:"origins"`
	AllowInsecureOrigin  bool   `json:"allow_insecure_origin"`
	UserVerification     string `json:"user_verification"`
	AttachmentPreference string `json:"attachment_preference"`
}

var ErrPasskeyRPIDInvalid = errors.New("Invalid Passkey domain. Enter a domain without a scheme, port, path or wildcard.")
var ErrPasskeyRPIDUnavailable = errors.New("This Passkey domain is not available on this website. Use its original website or another verification method.")

func (s PasskeySettings) EffectiveRPID() string {
	rpID := strings.TrimSpace(s.RPID)
	if rpID == "" {
		for _, origin := range strings.Split(s.Origins, ",") {
			if parsed, err := url.Parse(strings.TrimSpace(origin)); err == nil && parsed.Host != "" {
				rpID = parsed.Host
				break
			}
		}
	}
	if host, _, err := net.SplitHostPort(rpID); err == nil {
		return host
	}
	return rpID
}

func NormalizePasskeyRPID(value string, configuredOrigins ...string) (string, error) {
	rpID, err := idna.Lookup.ToASCII(strings.TrimSpace(value))
	rpID = strings.ToLower(rpID)
	if err != nil || rpID == "" || len(rpID) > 253 || strings.ContainsAny(rpID, ":/*@?#\\") || net.ParseIP(rpID) != nil {
		return "", ErrPasskeyRPIDInvalid
	}
	for _, label := range strings.Split(rpID, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", ErrPasskeyRPIDInvalid
		}
	}
	if rpID == "localhost" {
		return rpID, nil
	}
	if _, err := publicsuffix.EffectiveTLDPlusOne(rpID); err == nil {
		return rpID, nil
	}
	if _, icann := publicsuffix.PublicSuffix(rpID); icann || strings.Contains(rpID, ".") {
		return "", ErrPasskeyRPIDInvalid
	}
	for _, origins := range configuredOrigins {
		for _, origin := range strings.Split(origins, ",") {
			parsed, err := url.Parse(strings.TrimSpace(origin))
			if err == nil && parsed.Scheme == "https" && parsed.Hostname() == rpID && parsed.Path == "" && parsed.RawQuery == "" && parsed.Fragment == "" {
				return rpID, nil
			}
		}
	}
	return "", ErrPasskeyRPIDInvalid
}

func ParsePasskeyRPIDs(value string, configuredOrigins ...string) ([]string, error) {
	ids := []string{}
	for _, item := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == '\n' || r == '\r' }) {
		if strings.TrimSpace(item) == "" {
			continue
		}
		if _, err := NormalizePasskeyRPID(item, configuredOrigins...); err != nil {
			return nil, err
		}
		item = strings.TrimSpace(item)
		if !slices.Contains(ids, item) {
			ids = append(ids, item)
		}
	}
	return ids, nil
}

func (s PasskeySettings) RelyingPartyIDs() []string {
	ids := []string{}
	if primary := s.EffectiveRPID(); primary != "" {
		ids = append(ids, primary)
	}
	legacy, err := ParsePasskeyRPIDs(s.LegacyRPIDs, s.Origins)
	if err == nil {
		for _, id := range legacy {
			if !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

// WithDefaults returns an effective snapshot without mutating global settings.
func (s PasskeySettings) WithDefaults(serverAddress string) PasskeySettings {
	if strings.TrimSpace(s.RPID) == "" && strings.TrimSpace(serverAddress) != "" {
		if parsed, err := url.Parse(strings.TrimSpace(serverAddress)); err == nil && parsed.Host != "" {
			s.RPID = parsed.Host
		} else {
			s.RPID = strings.TrimSpace(serverAddress)
		}
	}
	if strings.TrimSpace(s.Origins) == "" || s.Origins == "[]" {
		s.Origins = serverAddress
	}
	return s
}

// PasskeySettingsSnapshot returns a copy with effective defaults applied so a
// WebAuthn ceremony cannot observe a mutable settings pointer mid-request.
func PasskeySettingsSnapshot() PasskeySettings {
	common.OptionMapRWMutex.RLock()
	settings, serverAddress := defaultPasskeySettings, ServerAddress
	common.OptionMapRWMutex.RUnlock()
	return settings.WithDefaults(serverAddress)
}

var defaultPasskeySettings = PasskeySettings{
	Enabled:              false,
	RPDisplayName:        common.SystemName,
	RPID:                 "",
	Origins:              "",
	AllowInsecureOrigin:  false,
	UserVerification:     "preferred",
	AttachmentPreference: "",
}

func init() {
	config.GlobalConfig.Register("passkey", &defaultPasskeySettings)
}

func GetPasskeySettings() *PasskeySettings {
	if defaultPasskeySettings.RPID == "" && ServerAddress != "" {
		// 从ServerAddress提取域名作为RPID
		// ServerAddress可能是 "https://newapi.pro" 这种格式
		serverAddr := strings.TrimSpace(ServerAddress)
		if parsed, err := url.Parse(serverAddr); err == nil && parsed.Host != "" {
			defaultPasskeySettings.RPID = parsed.Host
		} else {
			defaultPasskeySettings.RPID = serverAddr
		}
	}
	if defaultPasskeySettings.Origins == "" || defaultPasskeySettings.Origins == "[]" {
		defaultPasskeySettings.Origins = ServerAddress
	}
	return &defaultPasskeySettings
}
