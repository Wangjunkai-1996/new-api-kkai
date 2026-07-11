package operation_setting

import (
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
)

const PolicyIncidentOptionPrefix = "policy_incident_setting."

type PolicyIncidentSetting struct {
	DisableClientTokenPersistently  bool `json:"disable_client_token_persistently"`
	IsolateUpstreamOnPolicyIncident bool `json:"isolate_upstream_on_policy_incident"`
}

var policyIncidentSettingSnapshot atomic.Pointer[PolicyIncidentSetting]

func init() {
	setting := DefaultPolicyIncidentSetting()
	policyIncidentSettingSnapshot.Store(&setting)
}

func DefaultPolicyIncidentSetting() PolicyIncidentSetting {
	return PolicyIncidentSetting{
		DisableClientTokenPersistently:  false,
		IsolateUpstreamOnPolicyIncident: false,
	}
}

func GetPolicyIncidentSetting() PolicyIncidentSetting {
	snapshot := policyIncidentSettingSnapshot.Load()
	if snapshot == nil {
		return DefaultPolicyIncidentSetting()
	}
	return *snapshot
}

func PublishPolicyIncidentSetting(setting PolicyIncidentSetting) error {
	policyIncidentSettingSnapshot.Store(&setting)
	return nil
}

func PublishPolicyIncidentSettingFailClosed() PolicyIncidentSetting {
	setting := DefaultPolicyIncidentSetting()
	policyIncidentSettingSnapshot.Store(&setting)
	return setting
}

func IsPolicyIncidentOption(key string) bool {
	return strings.HasPrefix(key, PolicyIncidentOptionPrefix)
}

func PolicyIncidentSettingOptions(setting PolicyIncidentSetting) map[string]string {
	return map[string]string{
		PolicyIncidentOptionPrefix + "disable_client_token_persistently":   strconv.FormatBool(setting.DisableClientTokenPersistently),
		PolicyIncidentOptionPrefix + "isolate_upstream_on_policy_incident": strconv.FormatBool(setting.IsolateUpstreamOnPolicyIncident),
	}
}

func BuildPolicyIncidentSettingCandidate(base PolicyIncidentSetting, values map[string]string) (PolicyIncidentSetting, error) {
	candidate := base
	for key, value := range values {
		if !IsPolicyIncidentOption(key) {
			continue
		}
		field := strings.TrimPrefix(key, PolicyIncidentOptionPrefix)
		if err := applyPolicyIncidentSettingValue(&candidate, field, value); err != nil {
			return PolicyIncidentSetting{}, err
		}
	}
	return candidate, nil
}

func ValidatePolicyIncidentOption(key string, value string) error {
	if !IsPolicyIncidentOption(key) {
		return nil
	}
	_, err := BuildPolicyIncidentSettingCandidate(GetPolicyIncidentSetting(), map[string]string{key: value})
	return err
}

func applyPolicyIncidentSettingValue(candidate *PolicyIncidentSetting, field string, value string) error {
	if candidate == nil {
		return fmt.Errorf("invalid policy incident candidate")
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fmt.Errorf("invalid policy incident boolean option %s", field)
	}
	switch field {
	case "disable_client_token_persistently":
		candidate.DisableClientTokenPersistently = parsed
	case "isolate_upstream_on_policy_incident":
		candidate.IsolateUpstreamOnPolicyIncident = parsed
	default:
		return fmt.Errorf("unknown policy incident option %s", field)
	}
	return nil
}
