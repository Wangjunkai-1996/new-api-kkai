package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
)

func GetGroups(c *gin.Context) {
	groupNames := make([]string, 0)
	// Return the complete configured label map as well as the canonical group
	// list.  A label may temporarily refer to a special/orphaned key while an
	// administrator is editing settings; exposing it here lets admin screens
	// keep that label visible without materializing a new pricing entry.
	displayNames := setting.GetGroupDisplayNamesCopy()
	for groupName := range ratio_setting.GetGroupRatioCopy() {
		groupNames = append(groupNames, groupName)
		if _, ok := displayNames[groupName]; !ok {
			displayNames[groupName] = setting.GetGroupDisplayName(groupName)
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"success":       true,
		"message":       "",
		"data":          groupNames,
		"display_names": displayNames,
	})
}

func GetUserGroups(c *gin.Context) {
	usableGroups := make(map[string]map[string]interface{})
	userGroup := ""
	userId := c.GetInt("id")
	userGroup, _ = model.GetUserGroup(userId, false)
	userUsableGroups := service.GetUserUsableGroups(userGroup)
	for groupName, _ := range ratio_setting.GetGroupRatioCopy() {
		// UserUsableGroups contains the groups that the user can use
		if desc, ok := userUsableGroups[groupName]; ok {
			usableGroups[groupName] = map[string]interface{}{
				"ratio":        service.GetUserGroupRatio(userGroup, groupName),
				"desc":         desc,
				"display_name": setting.GetGroupDisplayNameWithFallback(groupName, desc),
			}
		}
	}
	if _, ok := userUsableGroups["auto"]; ok {
		usableGroups["auto"] = map[string]interface{}{
			"ratio":        "自动",
			"desc":         setting.GetUsableGroupDescription("auto"),
			"display_name": setting.GetGroupDisplayName("auto"),
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    usableGroups,
	})
}
