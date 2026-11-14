package service

import (
	"context"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

type videoStudioAPIContextKey struct{}

func WithVideoStudioAPI3(ctx context.Context) context.Context {
	return context.WithValue(ctx, videoStudioAPIContextKey{}, true)
}

func IsVideoStudioAPI3(ctx context.Context) bool {
	legacy, _ := ctx.Value(videoStudioAPIContextKey{}).(bool)
	return legacy
}

// This frozen snapshot keeps existing tabs' quotes stable when an administrator
// enables the new capability profile. It is stored inside existing JSON, never
// supplied by API4 clients or inferred from a newly configured profile.
type videoModelAPI3Snapshot struct {
	Specification     VideoModelSpec `json:"specification"`
	DefaultParameters map[string]any `json:"default_parameters"`
}

func decodeVideoModelProfileAPI3(profile model.KKAIVideoModelProfile) (VideoModelSpec, map[string]any, error) {
	var document struct {
		API3 *videoModelAPI3Snapshot `json:"api3_snapshot"`
	}
	if err := common.UnmarshalJsonStr(profile.Specification, &document); err != nil {
		return VideoModelSpec{}, nil, err
	}
	if document.API3 != nil {
		if err := ValidateVideoModelSpecAPI3(document.API3.Specification, document.API3.DefaultParameters); err != nil {
			return VideoModelSpec{}, nil, err
		}
		return document.API3.Specification, document.API3.DefaultParameters, nil
	}
	spec, defaults, err := decodeVideoModelProfile(profile)
	if err != nil {
		return VideoModelSpec{}, nil, err
	}
	if spec.ReferencePolicy != nil {
		return VideoModelSpec{}, nil, ErrVideoModelProfileNotFound
	}
	if err := ValidateVideoModelSpecAPI3(spec, defaults); err != nil {
		return VideoModelSpec{}, nil, err
	}
	return spec, defaults, nil
}

func preserveVideoModelAPI3Snapshot(previous model.KKAIVideoModelProfile, encoded string) (string, error) {
	var stored map[string]any
	if err := common.UnmarshalJsonStr(previous.Specification, &stored); err != nil {
		return "", err
	}
	snapshot, exists := stored["api3_snapshot"]
	if !exists {
		spec, defaults, err := decodeVideoModelProfileAPI3(previous)
		if err != nil {
			return encoded, nil
		}
		snapshot = videoModelAPI3Snapshot{Specification: spec, DefaultParameters: defaults}
	}
	var next map[string]any
	if err := common.UnmarshalJsonStr(encoded, &next); err != nil {
		return "", err
	}
	next["api3_snapshot"] = snapshot
	result, err := common.Marshal(next)
	return string(result), err
}

func updateVideoModelAPI3Snapshot(previous model.KKAIVideoModelProfile, updates map[string]any, spec VideoModelSpec, defaults map[string]any) error {
	var stored map[string]any
	if err := common.UnmarshalJsonStr(previous.Specification, &stored); err != nil {
		return err
	}
	if _, exists := stored["api3_snapshot"]; !exists {
		return nil
	}
	stored["api3_snapshot"] = videoModelAPI3Snapshot{Specification: spec, DefaultParameters: defaults}
	encoded, err := common.Marshal(stored)
	if err != nil {
		return fmt.Errorf("encode API3 capability snapshot: %w", err)
	}
	updates["specification"] = string(encoded)
	updates["specification_version"] = previous.SpecificationVersion
	updates["default_parameters"] = previous.DefaultParameters
	return nil
}
