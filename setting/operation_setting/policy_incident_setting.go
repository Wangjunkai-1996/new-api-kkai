package operation_setting

import "github.com/QuantumNous/new-api/setting/config"

type PolicyIncidentSetting struct {
	DisableClientTokenPersistently  bool `json:"disable_client_token_persistently"`
	IsolateUpstreamOnPolicyIncident bool `json:"isolate_upstream_on_policy_incident"`
}

var policyIncidentSetting = PolicyIncidentSetting{
	DisableClientTokenPersistently:  false,
	IsolateUpstreamOnPolicyIncident: false,
}

func init() {
	config.GlobalConfig.Register("policy_incident_setting", &policyIncidentSetting)
}

func GetPolicyIncidentSetting() *PolicyIncidentSetting {
	return &policyIncidentSetting
}
