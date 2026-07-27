package service

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

type VideoSampleReferenceSnapshot struct {
	AssetID    int64  `json:"asset_id"`
	Role       string `json:"role"`
	RequestKey string `json:"request_key"`
}

func newVideoSampleReferenceSnapshots(
	assetIDs []int64,
	inputs []VideoReferenceInputSpec,
) ([]VideoSampleReferenceSnapshot, error) {
	if len(assetIDs) != len(inputs) {
		return nil, ErrInvalidVideoSample
	}
	snapshots := make([]VideoSampleReferenceSnapshot, 0, len(assetIDs))
	for index, assetID := range assetIDs {
		if assetID <= 0 {
			return nil, ErrInvalidVideoSample
		}
		snapshots = append(snapshots, VideoSampleReferenceSnapshot{
			AssetID: assetID, Role: inputs[index].Role, RequestKey: inputs[index].RequestKey,
		})
	}
	return snapshots, nil
}

func decodeVideoSampleReferenceSnapshots(
	encoded string,
	fallback []VideoReferenceInputSpec,
) ([]VideoSampleReferenceSnapshot, error) {
	var snapshots []VideoSampleReferenceSnapshot
	if err := common.UnmarshalJsonStr(encoded, &snapshots); err == nil {
		if err := validateVideoSampleReferenceSnapshots(snapshots); err != nil {
			return nil, err
		}
		return snapshots, nil
	}
	var assetIDs []int64
	if err := common.UnmarshalJsonStr(encoded, &assetIDs); err != nil || len(assetIDs) != len(fallback) {
		return nil, ErrInvalidVideoSample
	}
	return newVideoSampleReferenceSnapshots(assetIDs, fallback)
}

func decodeVideoSampleReferenceAssetIDs(encoded string) ([]int64, error) {
	var snapshots []VideoSampleReferenceSnapshot
	if err := common.UnmarshalJsonStr(encoded, &snapshots); err == nil {
		if err := validateVideoSampleReferenceSnapshots(snapshots); err != nil {
			return nil, err
		}
		assetIDs := make([]int64, 0, len(snapshots))
		for _, snapshot := range snapshots {
			assetIDs = append(assetIDs, snapshot.AssetID)
		}
		return assetIDs, nil
	}
	var assetIDs []int64
	if err := common.UnmarshalJsonStr(encoded, &assetIDs); err != nil {
		return nil, fmt.Errorf("decode video sample references: %w", err)
	}
	for _, assetID := range assetIDs {
		if assetID <= 0 {
			return nil, ErrInvalidVideoSample
		}
	}
	return assetIDs, nil
}

func validateVideoSampleReferenceSnapshots(snapshots []VideoSampleReferenceSnapshot) error {
	seen := make(map[int64]struct{}, len(snapshots))
	for _, snapshot := range snapshots {
		if snapshot.AssetID <= 0 || !videoParameterKeyPattern.MatchString(snapshot.RequestKey) {
			return ErrInvalidVideoSample
		}
		switch snapshot.Role {
		case model.VideoTaskAssetRoleReference, model.VideoTaskAssetRoleFirstFrame, model.VideoTaskAssetRoleLastFrame:
		default:
			return ErrInvalidVideoSample
		}
		if _, duplicate := seen[snapshot.AssetID]; duplicate {
			return ErrInvalidVideoSample
		}
		seen[snapshot.AssetID] = struct{}{}
	}
	return nil
}

func videoSampleReferenceSnapshotsMatch(
	snapshots []VideoSampleReferenceSnapshot,
	inputs []VideoReferenceInputSpec,
) bool {
	if len(snapshots) != len(inputs) {
		return false
	}
	inputsByRole := make(map[string]VideoReferenceInputSpec, len(inputs))
	for _, input := range inputs {
		inputsByRole[input.Role] = input
	}
	for _, snapshot := range snapshots {
		input, exists := inputsByRole[snapshot.Role]
		if !exists || snapshot.RequestKey != input.RequestKey {
			return false
		}
	}
	return true
}
