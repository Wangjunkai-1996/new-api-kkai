package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/video_studio_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func configurationFixture(t *testing.T) (*gorm.DB, manifest, []model.KKAIVideoModelProfile) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "config.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	connection, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })
	require.NoError(t, db.AutoMigrate(&model.KKAIVideoModelProfile{}, &model.KKAIVideoSample{}, &model.KKAIVideoAsset{}, &model.Ability{}, &model.Option{}))
	require.NoError(t, db.Create(&model.Option{Key: switchKey, Value: "false"}).Error)
	input := manifest{Protocol: protocol, Operator: "test-operator", Action: "profiles"}
	for id, name := range allowedProfiles {
		modes := []string{service.VideoModeTextToVideo, service.VideoModeImageToVideo}
		if strings.HasSuffix(name, "_with_video_ref") {
			modes = []string{service.VideoModeImageToVideo}
		}
		spec := service.VideoModelSpec{Version: 1, Modes: modes,
			Parameters:      []service.VideoParameterSpec{{Key: "generate_audio", Label: "Audio", Control: service.VideoControlSwitch}},
			ReferenceInputs: []service.VideoReferenceInputSpec{{Role: "reference", RequestKey: "reference_image", Required: true}}}
		encoded, err := common.Marshal(spec)
		require.NoError(t, err)
		profile := model.KKAIVideoModelProfile{ID: id, Model: name, DisplayName: "Preserved " + name, Description: "Preserved description", ProviderLabel: "Seedance",
			SpecificationVersion: 1, Specification: string(encoded), DefaultParameters: `{"generate_audio":false}`, Enabled: id != 12, SortOrder: int(id), CreatedAt: 1, UpdatedAt: 2}
		require.NoError(t, db.Create(&profile).Error)
		require.NoError(t, db.Create(&model.Ability{Group: service.VideoStudioTokenGroup, Model: name, ChannelId: int(id), Enabled: true}).Error)
		spec.Version, spec.ReferenceInputs = 2, nil
		for index, mode := range spec.Modes {
			if mode == service.VideoModeImageToVideo {
				spec.Modes[index] = service.VideoModeReferenceToVideo
			}
		}
		spec.ReferencePolicy = &service.VideoReferencePolicy{
			RequiresVisual: true, RequiresVideo: strings.HasSuffix(name, "_with_video_ref"), AudioRequiresGenerateAudio: true,
			Image: &service.VideoReferenceMediaPolicy{AllowedMIMETypes: []string{"image/png"}, MaxCount: 9, MaxSizeBytes: 20 << 20, MinWidth: 301, MaxWidth: 5999, MinHeight: 301, MaxHeight: 5999, MinAspectRatioExclusive: .4, MaxAspectRatioExclusive: 2.5},
			Video: &service.VideoReferenceMediaPolicy{AllowedMIMETypes: []string{"video/mp4"}, MaxCount: 3, MaxSizeBytes: 49_999_999, MinDurationSeconds: 2, MaxDurationSeconds: 15, MaxTotalDurationSeconds: 15, MinFrameRate: 24, MaxFrameRate: 60},
			Audio: &service.VideoReferenceMediaPolicy{AllowedMIMETypes: []string{"audio/mpeg"}, MaxCount: 3, MaxSizeBytes: 14_999_999, MinDurationSeconds: 2, MaxDurationSeconds: 15, MaxTotalDurationSeconds: 15},
		}
		if !strings.HasSuffix(name, "_with_video_ref") {
			spec.ReferencePolicy.Video = nil
		}
		input.Profiles = append(input.Profiles, profilePlan{ID: id, Model: name, ExpectedVersion: 1, ExpectedSHA256: profileFingerprint(profile), Specification: spec})
	}
	var before []model.KKAIVideoModelProfile
	require.NoError(t, db.Order("id ASC").Find(&before).Error)
	return db, input, before
}

func configurationAudit(t *testing.T) *os.File {
	t.Helper()
	file, err := os.OpenFile(filepath.Join(t.TempDir(), "audit.jsonl"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })
	return file
}

func TestConfigurationPreviewIsReadOnlyAndApplyPreservesAPI3(t *testing.T) {
	db, input, before := configurationFixture(t)
	output, err := operate(t.Context(), db, input, false, false, strings.Repeat("a", 64), nil)
	require.NoError(t, err)
	assert.Equal(t, "preview", output.Mode)
	assert.Zero(t, output.Updated)
	var after []model.KKAIVideoModelProfile
	require.NoError(t, db.Order("id ASC").Find(&after).Error)
	assert.Equal(t, before, after)

	audit := configurationAudit(t)
	output, err = operate(t.Context(), db, input, false, true, strings.Repeat("a", 64), audit)
	require.NoError(t, err)
	assert.Equal(t, "applied", output.Mode)
	assert.Equal(t, 11, output.Updated)
	for _, previous := range before {
		var updated model.KKAIVideoModelProfile
		require.NoError(t, db.First(&updated, previous.ID).Error)
		assert.Equal(t, 2, updated.SpecificationVersion)
		assert.Equal(t, previous.Enabled, updated.Enabled)
		assert.Equal(t, previous.DefaultParameters, updated.DefaultParameters)
		assert.Equal(t, previous.DisplayName, updated.DisplayName)
		assert.Equal(t, previous.Description, updated.Description)
		assert.Equal(t, previous.ProviderLabel, updated.ProviderLabel)
		assert.Equal(t, previous.SortOrder, updated.SortOrder)
		legacy, err := service.GetVideoModelProfileByIDAPI3(t.Context(), db, previous.ID)
		require.NoError(t, err)
		var original service.VideoModelSpec
		require.NoError(t, common.UnmarshalJsonStr(previous.Specification, &original))
		assert.Equal(t, original, legacy.Specification)
		assert.Equal(t, map[string]any{"generate_audio": false}, legacy.DefaultParameters)
	}
	contents, err := os.ReadFile(audit.Name())
	require.NoError(t, err)
	assert.Contains(t, string(contents), `"mode":"applying"`)
	assert.Contains(t, string(contents), `"mode":"applied"`)
	assert.Contains(t, string(contents), `"updated":11`)
}

func TestConfigurationDriftLeavesAllProfilesUnchanged(t *testing.T) {
	for _, column := range []string{"specification_version", "display_name", "default_parameters"} {
		t.Run(column, func(t *testing.T) {
			db, input, _ := configurationFixture(t)
			var value any = "changed"
			if column == "specification_version" {
				value = 9
			}
			if column == "default_parameters" {
				value = `{"generate_audio":true}`
			}
			require.NoError(t, db.Model(&model.KKAIVideoModelProfile{}).Where("id = ?", 12).Update(column, value).Error)
			var before, after []model.KKAIVideoModelProfile
			require.NoError(t, db.Order("id ASC").Find(&before).Error)
			_, err := operate(t.Context(), db, input, false, true, strings.Repeat("a", 64), configurationAudit(t))
			require.ErrorContains(t, err, "drifted")
			require.NoError(t, db.Order("id ASC").Find(&after).Error)
			assert.Equal(t, before, after)
		})
	}
}

func TestConfigurationServiceFailureRollsBackEarlierProfiles(t *testing.T) {
	db, input, before := configurationFixture(t)
	// A new-format published sample incompatible with the replacement contract
	// must fail service validation after earlier profiles have been processed.
	require.NoError(t, db.Create(&model.KKAIVideoSample{ModelProfileID: 12, Mode: service.VideoModeImageToVideo, Status: model.VideoSampleStatusPublished,
		ReferenceAssetIDs: `{"reference_assets":[]}`, Parameters: `{}`, ModelVersion: 1}).Error)
	_, err := operate(t.Context(), db, input, false, true, strings.Repeat("a", 64), configurationAudit(t))
	require.ErrorContains(t, err, "transaction aborted")
	var after []model.KKAIVideoModelProfile
	require.NoError(t, db.Order("id ASC").Find(&after).Error)
	assert.Equal(t, before, after)
}

func TestConfigurationSwitchRequiresPreparedProfilesAndExactPreviousState(t *testing.T) {
	db, input, _ := configurationFixture(t)
	previousEnabled := video_studio_setting.Get().MultimodalEnabled
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{switchKey: strconv.FormatBool(previousEnabled)}))
	})
	inspected, err := operate(t.Context(), db, manifest{}, true, false, "", nil)
	require.NoError(t, err)
	enabled := true
	flagInput := manifest{Protocol: protocol, Operator: "test-operator", Action: "multimodal", ExpectedProfilesSHA256: inspected.ProfilesSHA256,
		Switch: &switchPlan{Expected: "false", Value: &enabled}}
	_, err = operate(t.Context(), db, flagInput, false, true, strings.Repeat("b", 64), configurationAudit(t))
	require.ErrorContains(t, err, "prepared")
	_, err = operate(t.Context(), db, input, false, true, strings.Repeat("a", 64), configurationAudit(t))
	require.NoError(t, err)
	inspected, err = operate(t.Context(), db, manifest{}, true, false, "", nil)
	require.NoError(t, err)
	flagInput.ExpectedProfilesSHA256 = inspected.ProfilesSHA256
	flagInput.Switch.Expected = "true"
	_, err = operate(t.Context(), db, flagInput, false, true, strings.Repeat("b", 64), configurationAudit(t))
	require.ErrorContains(t, err, "drifted")
	flagInput.Switch.Expected = "false"
	output, err := operate(t.Context(), db, flagInput, false, true, strings.Repeat("b", 64), configurationAudit(t))
	require.NoError(t, err)
	assert.Equal(t, "true", output.Switch)
	assert.Zero(t, output.Updated)
	var option model.Option
	require.NoError(t, db.Where("key = ?", switchKey).First(&option).Error)
	assert.Equal(t, "true", option.Value)
}

func TestConfigurationRejectsUnexpectedScopeAndCancellation(t *testing.T) {
	db, input, before := configurationFixture(t)
	input.Profiles = input.Profiles[:10]
	require.Error(t, validateManifest(input))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := operate(ctx, db, manifest{}, true, false, "", nil)
	require.Error(t, err)
	var after []model.KKAIVideoModelProfile
	require.NoError(t, db.Order("id ASC").Find(&after).Error)
	assert.Equal(t, before, after)
}

func TestConfigurationRequiresExactVideoSKUShape(t *testing.T) {
	_, input, _ := configurationFixture(t)
	for _, item := range input.Profiles {
		if item.ID == 1 {
			item.Specification.ReferencePolicy.Video = &service.VideoReferenceMediaPolicy{MaxCount: 3}
			require.ErrorContains(t, validateReferencePolicy(item.Model, item.Specification), "video only")
		}
		if item.ID == 4 {
			item.Specification.ReferencePolicy.Video = nil
			require.ErrorContains(t, validateReferencePolicy(item.Model, item.Specification), "video only")
		}
	}
}

func TestConfigurationInitializesAbsentSwitchWithoutChangingProfiles(t *testing.T) {
	db, _, before := configurationFixture(t)
	require.NoError(t, db.Where("key = ?", switchKey).Delete(&model.Option{}).Error)
	inspected, err := operate(t.Context(), db, manifest{}, true, false, "", nil)
	require.NoError(t, err)
	require.Equal(t, "absent", inspected.Switch)
	disabled := false
	input := manifest{Protocol: protocol, Operator: "test-operator", Action: "multimodal", ExpectedProfilesSHA256: inspected.ProfilesSHA256,
		Switch: &switchPlan{Expected: "absent", Value: &disabled}}
	output, err := operate(t.Context(), db, input, false, true, strings.Repeat("b", 64), configurationAudit(t))
	require.NoError(t, err)
	assert.Equal(t, "false", output.Switch)
	var after []model.KKAIVideoModelProfile
	require.NoError(t, db.Order("id ASC").Find(&after).Error)
	assert.Equal(t, before, after)
	_, err = operate(t.Context(), db, input, false, true, strings.Repeat("b", 64), configurationAudit(t))
	require.ErrorContains(t, err, "drifted")
}

func TestConfigurationAuditFailurePreventsWrites(t *testing.T) {
	db, input, before := configurationFixture(t)
	audit, err := os.Create(filepath.Join(t.TempDir(), "closed-audit.jsonl"))
	require.NoError(t, err)
	require.NoError(t, audit.Close())
	_, err = operate(t.Context(), db, input, false, true, strings.Repeat("a", 64), audit)
	require.ErrorContains(t, err, "cannot write audit")
	var after []model.KKAIVideoModelProfile
	require.NoError(t, db.Order("id ASC").Find(&after).Error)
	assert.Equal(t, before, after)
	var option model.Option
	require.NoError(t, db.Where("key = ?", switchKey).First(&option).Error)
	assert.Equal(t, "false", option.Value)
}
