package setting

import (
	"sync"

	"github.com/QuantumNous/new-api/common"
)

var userUsableGroups = map[string]string{
	"default": "默认分组",
	"vip":     "vip分组",
}
var userUsableGroupsMutex sync.RWMutex

func GetUserUsableGroupsCopy() map[string]string {
	userUsableGroupsMutex.RLock()
	defer userUsableGroupsMutex.RUnlock()

	copyUserUsableGroups := make(map[string]string)
	for k, v := range userUsableGroups {
		copyUserUsableGroups[k] = v
	}
	return copyUserUsableGroups
}

func UserUsableGroups2JSONString() string {
	jsonBytes, err := common.Marshal(GetUserUsableGroupsCopy())
	if err != nil {
		common.SysLog("error marshalling user groups: " + err.Error())
		return "{}"
	}
	return string(jsonBytes)
}

func UpdateUserUsableGroupsByJSONString(jsonStr string) error {
	// Decode before taking the write lock. A malformed admin setting must not
	// clear the last known-good group map.
	var parsed map[string]string
	if err := common.UnmarshalJsonStr(jsonStr, &parsed); err != nil {
		return err
	}
	if parsed == nil {
		parsed = make(map[string]string)
	}

	userUsableGroupsMutex.Lock()
	userUsableGroups = parsed
	userUsableGroupsMutex.Unlock()
	return nil
}

func GetUsableGroupDescription(groupName string) string {
	userUsableGroupsMutex.RLock()
	defer userUsableGroupsMutex.RUnlock()

	if desc, ok := userUsableGroups[groupName]; ok {
		return desc
	}
	return groupName
}
