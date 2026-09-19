package service

import (
	"strings"

	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

func GetUserUsableGroups(userGroup string) map[string]string {
	groupsCopy := setting.GetUserUsableGroupsCopy()
	if userGroup != "" {
		specialSettings, b := ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.Get(userGroup)
		if b {
			// 处理特殊可用分组
			for specialGroup, desc := range specialSettings {
				if strings.HasPrefix(specialGroup, "-:") {
					// 移除分组
					groupToRemove := strings.TrimPrefix(specialGroup, "-:")
					delete(groupsCopy, groupToRemove)
				} else if strings.HasPrefix(specialGroup, "+:") {
					// 添加分组
					groupToAdd := strings.TrimPrefix(specialGroup, "+:")
					groupsCopy[groupToAdd] = desc
				} else {
					// 直接添加分组
					groupsCopy[specialGroup] = desc
				}
			}
		}
		// 如果userGroup不在UserUsableGroups中，返回UserUsableGroups + userGroup
		if _, ok := groupsCopy[userGroup]; !ok {
			groupsCopy[userGroup] = "用户分组"
		}
	}
	return groupsCopy
}

func GroupInUserUsableGroups(userGroup, groupName string) bool {
	_, ok := GetUserUsableGroups(userGroup)[groupName]
	return ok
}

// IsUserTokenGroupUsable reports whether a token may explicitly use group.
// Empty means that the token follows the user's current group.
func IsUserTokenGroupUsable(userGroup, group string) bool {
	if group == "" {
		return true
	}
	if IsAutoGroup(group) {
		for _, autoGroup := range GetUserAutoGroups(userGroup) {
			if autoGroup == group {
				return true
			}
		}
		return false
	}
	if !GroupInUserUsableGroups(userGroup, group) {
		return false
	}
	return ratio_setting.ContainsGroupRatio(group)
}

// GetUserAutoGroup 根据用户分组获取自动分组设置
func GetUserAutoGroup(userGroup string) []string {
	return GetUserAutoGroupCandidates(userGroup, "auto")
}

// GetUserAutoGroupCandidates returns the configured candidate order for a
// virtual auto group, filtered by the user's usable groups.
func GetUserAutoGroupCandidates(userGroup, autoGroup string) []string {
	if !setting.IsAutoGroup(autoGroup) {
		return []string{}
	}
	usableGroups := GetUserUsableGroups(userGroup)
	candidates := setting.GetAutoGroupCandidates(autoGroup)
	result := make([]string, 0, len(candidates))
	for _, group := range candidates {
		if setting.ContainsAutoGroupProfile(group) {
			continue
		}
		if _, ok := usableGroups[group]; ok {
			result = append(result, group)
		}
	}
	return result
}

// GetUserAutoGroups returns virtual auto groups the user is allowed to use.
func GetUserAutoGroups(userGroup string) []string {
	usableGroups := GetUserUsableGroups(userGroup)
	result := make([]string, 0, len(setting.GetAutoGroupProfilesCopy())+1)
	for _, autoGroup := range setting.GetAutoGroupNames() {
		if _, ok := usableGroups[autoGroup]; ok {
			result = append(result, autoGroup)
		}
	}
	return result
}

func IsAutoGroup(group string) bool {
	return setting.IsAutoGroup(group)
}

// GetUserGroupRatio 获取用户使用某个分组的倍率
// userGroup 用户分组
// group 需要获取倍率的分组
func GetUserGroupRatio(userGroup, group string) float64 {
	ratio, ok := ratio_setting.GetGroupGroupRatio(userGroup, group)
	if ok {
		return ratio
	}
	return ratio_setting.GetGroupRatio(group)
}
