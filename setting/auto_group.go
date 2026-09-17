package setting

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
)

var autoGroups = []string{
	"default",
}

var autoGroupProfiles = map[string][]string{}
var autoGroupMutex sync.RWMutex
var autoGroupProfileNamePattern = regexp.MustCompile(`^auto(?:[2-9]|[1-9][0-9]+)$`)

var DefaultUseAutoGroup = false

func IsAutoGroup(name string) bool {
	return name == "auto" || ContainsAutoGroupProfile(name)
}

func ContainsAutoGroup(group string) bool {
	autoGroupMutex.RLock()
	defer autoGroupMutex.RUnlock()
	for _, autoGroup := range autoGroups {
		if autoGroup == group {
			return true
		}
	}
	return false
}

func ContainsAutoGroupProfile(name string) bool {
	autoGroupMutex.RLock()
	defer autoGroupMutex.RUnlock()
	_, ok := autoGroupProfiles[name]
	return ok
}

func GetAutoGroupNames() []string {
	autoGroupMutex.RLock()
	defer autoGroupMutex.RUnlock()
	names := make([]string, 0, len(autoGroupProfiles)+1)
	names = append(names, "auto")
	for name := range autoGroupProfiles {
		names = append(names, name)
	}
	sort.Strings(names[1:])
	return names
}

func GetAutoGroupCandidates(name string) []string {
	autoGroupMutex.RLock()
	defer autoGroupMutex.RUnlock()
	if name == "auto" {
		return append([]string(nil), autoGroups...)
	}
	return append([]string(nil), autoGroupProfiles[name]...)
}

func UpdateAutoGroupsByJsonString(jsonString string) error {
	parsed, err := parseAutoGroups(jsonString)
	if err != nil {
		return err
	}
	autoGroupMutex.Lock()
	autoGroups = parsed
	autoGroupMutex.Unlock()
	return nil
}

func ValidateAutoGroupsJSON(jsonString string) error {
	_, err := parseAutoGroups(jsonString)
	return err
}

func AutoGroups2JsonString() string {
	autoGroupMutex.RLock()
	defer autoGroupMutex.RUnlock()
	jsonBytes, err := common.Marshal(autoGroups)
	if err != nil {
		return "[]"
	}
	return string(jsonBytes)
}

func GetAutoGroups() []string {
	autoGroupMutex.RLock()
	defer autoGroupMutex.RUnlock()
	return append([]string(nil), autoGroups...)
}

func GetAutoGroupProfilesCopy() map[string][]string {
	autoGroupMutex.RLock()
	defer autoGroupMutex.RUnlock()
	profiles := make(map[string][]string, len(autoGroupProfiles))
	for name, groups := range autoGroupProfiles {
		profiles[name] = append([]string(nil), groups...)
	}
	return profiles
}

func UpdateAutoGroupProfilesByJsonString(jsonString string) error {
	parsed, err := parseAutoGroupProfiles(jsonString)
	if err != nil {
		return err
	}
	autoGroupMutex.Lock()
	autoGroupProfiles = parsed
	autoGroupMutex.Unlock()
	return nil
}

func ValidateAutoGroupProfilesJSON(jsonString string) error {
	_, err := parseAutoGroupProfiles(jsonString)
	return err
}

func ValidateAutoGroupProfilesJSONAgainstGroups(jsonString string, groupRatios map[string]float64) error {
	profiles, err := parseAutoGroupProfiles(jsonString)
	if err != nil {
		return err
	}
	for name := range profiles {
		if _, exists := groupRatios[name]; exists {
			return fmt.Errorf("auto group profile %q conflicts with a pricing group", name)
		}
	}
	return nil
}

func AutoGroupProfiles2JsonString() string {
	autoGroupMutex.RLock()
	defer autoGroupMutex.RUnlock()
	jsonBytes, err := common.Marshal(autoGroupProfiles)
	if err != nil {
		return "{}"
	}
	return string(jsonBytes)
}

func parseAutoGroups(jsonString string) ([]string, error) {
	var parsed []string
	if err := common.Unmarshal([]byte(jsonString), &parsed); err != nil {
		return nil, err
	}
	if parsed == nil {
		// Legacy null disables auto routing, just like an empty array.
		return []string{}, nil
	}
	return parsed, nil
}

func parseAutoGroupProfiles(jsonString string) (map[string][]string, error) {
	if strings.TrimSpace(jsonString) == "" {
		return map[string][]string{}, nil
	}
	var parsed map[string][]string
	if err := common.Unmarshal([]byte(jsonString), &parsed); err != nil {
		return nil, err
	}
	if parsed == nil {
		return nil, errors.New("auto group profiles must be a JSON object")
	}
	profiles := make(map[string][]string, len(parsed))
	for name, groups := range parsed {
		if !autoGroupProfileNamePattern.MatchString(name) {
			return nil, fmt.Errorf("invalid auto group profile name %q", name)
		}
		if len(groups) == 0 {
			return nil, fmt.Errorf("auto group profile %q cannot be empty", name)
		}
		seen := make(map[string]struct{}, len(groups))
		for i, group := range groups {
			if strings.TrimSpace(group) == "" {
				return nil, fmt.Errorf("auto group profile %q has empty group at index %d", name, i)
			}
			if group == "auto" {
				return nil, fmt.Errorf("auto group profile %q cannot reference virtual group %q", name, group)
			}
			if _, ok := seen[group]; ok {
				return nil, fmt.Errorf("auto group profile %q contains duplicate group %q", name, group)
			}
			seen[group] = struct{}{}
		}
		profiles[name] = append([]string(nil), groups...)
	}
	for name, groups := range profiles {
		for _, group := range groups {
			if _, ok := profiles[group]; ok {
				return nil, fmt.Errorf("auto group profile %q cannot reference profile %q", name, group)
			}
		}
	}
	return profiles, nil
}
