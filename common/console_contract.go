package common

import _ "embed"

//go:embed console_contract.json
var consoleContractJSON []byte

// ConsoleAPIContract describes the dashboard API, not model relay protocols.
type ConsoleAPIContract struct {
	FormatVersion int      `json:"format_version"`
	APIContracts  []int    `json:"api_contracts"`
	Capabilities  []string `json:"capabilities"`
}

func ConsoleContract() ConsoleAPIContract {
	var profiles map[string]ConsoleAPIContract
	if err := Unmarshal(consoleContractJSON, &profiles); err != nil {
		panic("invalid embedded console contract: " + err.Error())
	}
	return profiles[consoleContractProfile]
}
