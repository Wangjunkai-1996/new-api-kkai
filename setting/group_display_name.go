package setting

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
)

// groupDisplayNames maps the stable group key used by the backend to the name
// shown in user-facing selectors.  Keeping this separate from UserUsableGroups
// lets operators change the label without changing any persisted group keys.
var groupDisplayNames = make(map[string]string)
var groupDisplayNamesMutex sync.RWMutex

// ResetGroupDisplayNames clears the in-memory labels. It is used when the
// option map is rebuilt (for example after switching databases) so labels from
// a previous configuration cannot leak into the new runtime.
func ResetGroupDisplayNames() {
	groupDisplayNamesMutex.Lock()
	groupDisplayNames = make(map[string]string)
	groupDisplayNamesMutex.Unlock()
}

// GetGroupDisplayNamesCopy returns a snapshot of the configured display names.
func GetGroupDisplayNamesCopy() map[string]string {
	groupDisplayNamesMutex.RLock()
	defer groupDisplayNamesMutex.RUnlock()

	copyDisplayNames := make(map[string]string, len(groupDisplayNames))
	for groupName, displayName := range groupDisplayNames {
		copyDisplayNames[groupName] = displayName
	}
	return copyDisplayNames
}

// GroupDisplayNames2JSONString serializes the configured display names for the
// option store and the admin settings API.
func GroupDisplayNames2JSONString() string {
	jsonBytes, err := common.Marshal(GetGroupDisplayNamesCopy())
	if err != nil {
		common.SysLog("error marshalling group display names: " + err.Error())
		return "{}"
	}
	return string(jsonBytes)
}

// UpdateGroupDisplayNamesByJSONString replaces the configured display names.
// Decode before taking the write lock so malformed configuration cannot erase
// the last known-good runtime value.
func UpdateGroupDisplayNamesByJSONString(jsonStr string) error {
	parsed, err := parseGroupDisplayNames(jsonStr)
	if err != nil {
		return err
	}

	groupDisplayNamesMutex.Lock()
	groupDisplayNames = parsed
	groupDisplayNamesMutex.Unlock()
	return nil
}

// ValidateGroupDisplayNamesJSON validates an option value without changing
// the currently active display-name map.
func ValidateGroupDisplayNamesJSON(jsonStr string) error {
	_, err := parseGroupDisplayNames(jsonStr)
	return err
}

// parseGroupDisplayNames validates and normalizes a complete map before it is
// published. Group keys are protocol identifiers, so their spelling is kept
// exactly as configured; only surrounding whitespace on labels is removed.
func parseGroupDisplayNames(jsonStr string) (map[string]string, error) {
	var parsed map[string]string
	if err := common.UnmarshalJsonStr(jsonStr, &parsed); err != nil {
		return nil, err
	}
	if parsed == nil {
		return nil, errors.New("group display names must be a JSON object")
	}

	normalized := make(map[string]string, len(parsed))
	for groupName, displayName := range parsed {
		if strings.TrimSpace(groupName) == "" {
			return nil, errors.New("group display name key cannot be empty")
		}
		displayName = strings.TrimSpace(displayName)
		if displayName == "" {
			return nil, fmt.Errorf("group display name for %q cannot be empty", groupName)
		}
		normalized[groupName] = displayName
	}
	return normalized, nil
}

// GetGroupDisplayName resolves a group's user-facing label.  Explicit display
// names take precedence; existing UserUsableGroups descriptions remain the
// compatibility fallback, followed by the stable group key.
func GetGroupDisplayName(groupName string) string {
	groupDisplayNamesMutex.RLock()
	displayName, configured := groupDisplayNames[groupName]
	groupDisplayNamesMutex.RUnlock()
	if configured && strings.TrimSpace(displayName) != "" {
		return displayName
	}

	description := GetUsableGroupDescription(groupName)
	if strings.TrimSpace(description) != "" {
		return description
	}
	return groupName
}

// GetGroupDisplayNameWithFallback is useful for groups added by a user's
// special-group policy, whose compatibility description is only available at
// the call site.
func GetGroupDisplayNameWithFallback(groupName, fallback string) string {
	groupDisplayNamesMutex.RLock()
	displayName, configured := groupDisplayNames[groupName]
	groupDisplayNamesMutex.RUnlock()
	if configured && strings.TrimSpace(displayName) != "" {
		return displayName
	}
	if strings.TrimSpace(fallback) != "" {
		return fallback
	}
	return GetGroupDisplayName(groupName)
}
