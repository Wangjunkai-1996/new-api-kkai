package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const switchKey = model.VideoStudioMultimodalOption

var allowedProfiles = map[int64]string{
	1: "sd_2.0_fast_special_720p", 3: "sd_2.0_special_1080p",
	4: "sd_2.0_fast_special_720p_with_video_ref", 5: "sd_2.0_special_720p",
	6: "sd_2.0_special_2k", 7: "sd_2.0_special_4k",
	8: "sd_2.0_special_720p_with_video_ref", 9: "sd_2.0_special_1080p_with_video_ref",
	10: "sd_2.0_special_2k_with_video_ref", 11: "sd_2.0_special_4k_with_video_ref",
	12: "seedance-2.5",
}

type profileState struct {
	ID                int64             `json:"id"`
	Model             string            `json:"model"`
	ExpectedVersion   int               `json:"expected_version"`
	ExpectedSHA256    string            `json:"expected_sha256"`
	Specification     common.RawMessage `json:"specification"`
	DefaultParameters common.RawMessage `json:"default_parameters"`
	Enabled           bool              `json:"enabled"`
}

func validateManifest(input manifest) error {
	if input.Protocol != protocol || input.Operator == "" || len(input.Operator) > 128 || strings.TrimSpace(input.Operator) != input.Operator || strings.ContainsAny(input.Operator, "\r\n\x00") {
		return errors.New("manifest requires the exact protocol and an operator")
	}
	switch input.Action {
	case "profiles":
		if len(input.Profiles) != len(allowedProfiles) || input.Switch != nil || input.ExpectedProfilesSHA256 != "" {
			return errors.New("profiles action requires exactly the eleven allowlisted profiles and no switch action")
		}
		seen := map[int64]bool{}
		for _, item := range input.Profiles {
			if allowedProfiles[item.ID] != item.Model || item.Model == "" || seen[item.ID] || item.ExpectedVersion <= 0 ||
				item.Specification.Version != item.ExpectedVersion+1 || !validSHA256(item.ExpectedSHA256) {
				return errors.New("profile identity, version or fingerprint is invalid")
			}
			seen[item.ID] = true
			if err := validateReferencePolicy(item.Model, item.Specification); err != nil {
				return err
			}
		}
	case "multimodal":
		if len(input.Profiles) != 0 || input.Switch == nil || input.Switch.Value == nil || !validSHA256(input.ExpectedProfilesSHA256) ||
			(input.Switch.Expected != "absent" && input.Switch.Expected != "false" && input.Switch.Expected != "true") {
			return errors.New("multimodal action requires the current profiles fingerprint and an explicit expected/value switch")
		}
	default:
		return errors.New("only profiles and multimodal actions are allowed")
	}
	return nil
}

func validateReferencePolicy(name string, spec service.VideoModelSpec) error {
	p := spec.ReferencePolicy
	requiresVideo := strings.HasSuffix(name, "_with_video_ref")
	if p == nil || p.Image == nil || p.Audio == nil || (p.Video != nil) != requiresVideo || p.MaxTotalCount != nil || !p.RequiresVisual ||
		p.RequiresVideo != requiresVideo || !p.AudioRequiresGenerateAudio || len(spec.ReferenceInputs) != 0 {
		return errors.New("profile requires image/audio and video only for the exact video-reference SKUs")
	}
	if p.Image.MaxCount > 9 || p.Audio.MaxCount > 3 ||
		p.Image.MaxSizeBytes > 20<<20 || p.Audio.MaxSizeBytes > 14_999_999 ||
		p.Image.MinWidth < 301 || p.Image.MaxWidth > 5999 || p.Image.MinHeight < 301 || p.Image.MaxHeight > 5999 ||
		p.Image.MinAspectRatioExclusive < .4 || p.Image.MaxAspectRatioExclusive > 2.5 {
		return errors.New("profile exceeds the reviewed per-media limits")
	}
	if p.Video != nil && (p.Video.MaxCount > 3 || p.Video.MaxSizeBytes > 49_999_999 || p.Video.MinFrameRate < 24 || p.Video.MaxFrameRate > 60) {
		return errors.New("profile exceeds the reviewed video limits")
	}
	for _, media := range []*service.VideoReferenceMediaPolicy{p.Video, p.Audio} {
		if media != nil && (media.MinDurationSeconds < 2 || media.MaxDurationSeconds > 15 || media.MaxTotalDurationSeconds > 15) {
			return errors.New("profile exceeds the reviewed per-media duration limits")
		}
	}
	for kind, media := range map[string]*service.VideoReferenceMediaPolicy{"image": p.Image, "video": p.Video, "audio": p.Audio} {
		if media == nil {
			continue
		}
		for _, mime := range media.AllowedMIMETypes {
			allowed := (kind == "image" && (mime == "image/jpeg" || mime == "image/png" || mime == "image/webp")) ||
				(kind == "video" && (mime == "video/mp4" || mime == "video/quicktime")) ||
				(kind == "audio" && (mime == "audio/mpeg" || mime == "audio/wav"))
			if !allowed {
				return errors.New("profile declares an unreviewed media format")
			}
		}
	}
	return nil
}

func profileFingerprint(profile model.KKAIVideoModelProfile) string {
	encoded, _ := common.Marshal(map[string]any{
		"id": profile.ID, "model": profile.Model, "display_name": profile.DisplayName,
		"description": profile.Description, "provider_label": profile.ProviderLabel,
		"specification_version": profile.SpecificationVersion, "specification": profile.Specification,
		"default_parameters": profile.DefaultParameters, "enabled": profile.Enabled,
		"sort_order": profile.SortOrder, "created_at": profile.CreatedAt, "updated_at": profile.UpdatedAt,
	})
	return fmt.Sprintf("%x", sha256.Sum256(encoded))
}

func profileStates(profiles []model.KKAIVideoModelProfile) ([]profileState, string) {
	states := make([]profileState, 0, len(profiles))
	for _, profile := range profiles {
		states = append(states, profileState{ID: profile.ID, Model: profile.Model, ExpectedVersion: profile.SpecificationVersion,
			ExpectedSHA256: profileFingerprint(profile), Specification: common.RawMessage(profile.Specification),
			DefaultParameters: common.RawMessage(profile.DefaultParameters), Enabled: profile.Enabled})
	}
	encoded, _ := common.Marshal(states)
	return states, fmt.Sprintf("%x", sha256.Sum256(encoded))
}

func operate(ctx context.Context, db *gorm.DB, input manifest, inspect, apply bool, digest string, audit *os.File) (*result, error) {
	if !inspect {
		if err := validateManifest(input); err != nil {
			return nil, err
		}
	}
	if apply && audit == nil {
		return nil, errors.New("apply requires an audit file")
	}
	output := &result{Protocol: protocol, Mode: "preview", Action: input.Action, Operator: input.Operator, ManifestSHA256: digest,
		Version: common.Version, OperatorUID: os.Geteuid(), ObservedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if inspect {
		output.Mode = "inspect"
	}
	ids := make([]int64, 0, len(allowedProfiles))
	for id := range allowedProfiles {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	var operationErr error
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) (callbackErr error) {
		defer func() { operationErr = callbackErr }()
		if tx.Dialector.Name() == "postgres" && tx.Exec("SET LOCAL lock_timeout = '5s'").Error != nil {
			return errors.New("cannot set bounded lock timeout")
		}
		profiles, option, err := model.LoadVideoStudioConfiguration(ctx, tx, ids, apply)
		if err != nil || len(profiles) != len(allowedProfiles) {
			return errors.New("the eleven target profiles or multimodal switch are not available")
		}
		for _, profile := range profiles {
			if allowedProfiles[profile.ID] != profile.Model {
				return errors.New("target profile identity changed")
			}
		}
		output.Switch = "absent"
		if option != nil {
			output.Switch = option.Value
		}
		if output.Switch != "absent" && output.Switch != "false" && output.Switch != "true" {
			return errors.New("stored multimodal switch is malformed")
		}
		output.Profiles, output.ProfilesSHA256 = profileStates(profiles)
		if inspect {
			return nil
		}
		plans := map[int64]profilePlan{}
		for _, item := range input.Profiles {
			plans[item.ID] = item
		}
		prepared := make([]service.VideoModelProfileInput, 0, len(profiles))
		if input.Action == "profiles" {
			if output.Switch != "false" {
				return errors.New("profile preparation requires an explicitly persisted false multimodal switch")
			}
			for _, profile := range profiles {
				item := plans[profile.ID]
				if item.ExpectedVersion != profile.SpecificationVersion || item.ExpectedSHA256 != profileFingerprint(profile) {
					return errors.New("profile version or content drifted; no configuration changed")
				}
				var previous service.VideoModelSpec
				var defaults map[string]any
				if common.UnmarshalJsonStr(profile.Specification, &previous) != nil || common.UnmarshalJsonStr(profile.DefaultParameters, &defaults) != nil {
					return errors.New("stored profile JSON is invalid")
				}
				for index, mode := range previous.Modes {
					if mode == service.VideoModeImageToVideo {
						previous.Modes[index] = service.VideoModeReferenceToVideo
					}
				}
				for index := range previous.Parameters {
					for modeIndex, mode := range previous.Parameters[index].Modes {
						if mode == service.VideoModeImageToVideo {
							previous.Parameters[index].Modes[modeIndex] = service.VideoModeReferenceToVideo
						}
					}
				}
				if !reflect.DeepEqual(previous.Modes, item.Specification.Modes) || !reflect.DeepEqual(previous.Parameters, item.Specification.Parameters) {
					return errors.New("profile upgrade must preserve existing modes and parameter definitions except the reference-mode rename")
				}
				if err := service.ValidateVideoModelSpec(item.Specification, defaults); err != nil {
					return errors.New("profile specification or preserved defaults are invalid")
				}
				if profile.Enabled {
					var count int64
					if tx.Model(&model.Ability{}).Where(&model.Ability{Group: service.VideoStudioTokenGroup, Model: profile.Model, Enabled: true}).Count(&count).Error != nil || count == 0 {
						return errors.New("enabled profile lacks its existing enabled ability")
					}
				}
				prepared = append(prepared, service.VideoModelProfileInput{Model: profile.Model, DisplayName: profile.DisplayName, Description: profile.Description,
					ProviderLabel: profile.ProviderLabel, Enabled: profile.Enabled, SortOrder: profile.SortOrder, DefaultParameters: defaults, Specification: item.Specification})
			}
		} else {
			if output.ProfilesSHA256 != input.ExpectedProfilesSHA256 || output.Switch != input.Switch.Expected {
				return errors.New("profiles or switch drifted; no configuration changed")
			}
			if *input.Switch.Value {
				if output.Switch == "absent" {
					return errors.New("initialize the switch to false before enabling it")
				}
				for _, profile := range profiles {
					var spec service.VideoModelSpec
					var defaults map[string]any
					if common.UnmarshalJsonStr(profile.Specification, &spec) != nil || common.UnmarshalJsonStr(profile.DefaultParameters, &defaults) != nil ||
						validateReferencePolicy(profile.Model, spec) != nil || service.ValidateVideoModelSpec(spec, defaults) != nil {
						return errors.New("all eleven profiles must be prepared before enabling multimodal creation")
					}
				}
			}
		}
		if !apply {
			return nil
		}
		output.Mode = "applying"
		if err := writeAudit(audit, output); err != nil {
			return err
		}
		if input.Action == "profiles" {
			for index, profile := range profiles {
				if _, err := service.UpdateVideoModelProfile(ctx, tx, profile.ID, prepared[index]); err != nil {
					return errors.New("profile service validation or persistence failed; transaction aborted")
				}
			}
			output.Updated = len(profiles)
			if tx.Where("id IN ?", ids).Order("id ASC").Find(&profiles).Error != nil {
				return errors.New("cannot verify updated profiles")
			}
			output.Profiles, output.ProfilesSHA256 = profileStates(profiles)
		} else {
			if output.Switch == "absent" {
				created := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&model.Option{Key: switchKey, Value: "false"})
				if created.Error != nil || created.RowsAffected != 1 {
					return errors.New("switch changed concurrently; transaction aborted")
				}
			}
			previousDB := model.DB
			model.DB = tx
			defer func() { model.DB = previousDB }()
			common.OptionMapRWMutex.Lock()
			previousOptions := common.OptionMap
			common.OptionMap = make(map[string]string)
			common.OptionMapRWMutex.Unlock()
			defer func() {
				common.OptionMapRWMutex.Lock()
				common.OptionMap = previousOptions
				common.OptionMapRWMutex.Unlock()
			}()
			if model.UpdateOption(switchKey, strconv.FormatBool(*input.Switch.Value)) != nil {
				return errors.New("switch persistence failed; transaction aborted")
			}
			output.Switch = strconv.FormatBool(*input.Switch.Value)
		}
		return nil
	}, &sql.TxOptions{ReadOnly: !apply})
	if err != nil {
		if apply {
			output.Mode, output.Updated = "failed_or_commit_unconfirmed", 0
			_ = writeAudit(audit, output)
		}
		if operationErr != nil {
			return nil, operationErr
		}
		return nil, errors.New("configuration commit is unconfirmed; inspect audit and database before retrying")
	}
	if apply {
		output.Mode = "applied"
		if err := writeAudit(audit, output); err != nil {
			return nil, errors.New("configuration committed but final audit failed; inspect database before retrying")
		}
	}
	return output, nil
}

func writeAudit(file *os.File, output *result) error {
	output.ObservedAt = time.Now().UTC().Format(time.RFC3339Nano)
	encoded, err := common.Marshal(output)
	if err != nil {
		return errors.New("cannot encode audit")
	}
	if _, err := fmt.Fprintln(file, string(encoded)); err != nil {
		return errors.New("cannot write audit")
	}
	if file.Sync() != nil {
		return errors.New("cannot persist audit")
	}
	return nil
}
