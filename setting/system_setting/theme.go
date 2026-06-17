package system_setting

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

type ThemeSettings struct {
	Frontend string `json:"frontend"`
}

var themeSettings = ThemeSettings{
	Frontend: "classic",
}

func init() {
	config.GlobalConfig.Register("theme", &themeSettings)
	syncThemeToCommon()
}

func syncThemeToCommon() {
	if forcedTheme := strings.TrimSpace(common.GetEnvOrDefaultString("FORCE_FRONTEND_THEME", "")); forcedTheme != "" {
		common.SetTheme(forcedTheme)
		return
	}
	common.SetTheme(themeSettings.Frontend)
}

func GetThemeSettings() *ThemeSettings {
	return &themeSettings
}

// UpdateAndSyncTheme syncs the theme config to common after DB load.
func UpdateAndSyncTheme() {
	syncThemeToCommon()
}
