package service

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

func sampleReferenceInputs(snapshots []VideoSampleReferenceSnapshot) []VideoStudioReferenceAssetInput {
	result := make([]VideoStudioReferenceAssetInput, 0, len(snapshots))
	for _, snapshot := range snapshots {
		position := snapshot.Position
		result = append(result, VideoStudioReferenceAssetInput{AssetID: snapshot.AssetID, Role: snapshot.Role, Position: &position})
	}
	return result
}

type VideoSampleReferenceSnapshot struct {
	AssetID    int64  `json:"asset_id"`
	Role       string `json:"role"`
	Position   int    `json:"position"`
	RequestKey string `json:"-"`
}

type videoSampleReferenceDocument struct {
	ReferenceAssets []VideoSampleReferenceSnapshot `json:"reference_assets"`
}

func decodeVideoSampleReferenceSnapshots(encoded string, _ []VideoReferenceInputSpec) ([]VideoSampleReferenceSnapshot, error) {
	var document videoSampleReferenceDocument
	if err := common.UnmarshalJsonStr(encoded, &document); err != nil || document.ReferenceAssets == nil {
		return nil, ErrInvalidVideoSample
	}
	if err := validateVideoSampleReferenceSnapshots(document.ReferenceAssets); err != nil {
		return nil, err
	}
	return document.ReferenceAssets, nil
}

// Asset lifecycle checks must still see historical IDs; they do not turn old
// rows into reusable Studio requests or infer roles from today's profile.
func decodeVideoSampleReferenceAssetIDs(encoded string) ([]int64, error) {
	var snapshots []VideoSampleReferenceSnapshot
	if current, err := decodeVideoSampleReferenceSnapshots(encoded, nil); err == nil {
		snapshots = current
	} else if err := common.UnmarshalJsonStr(encoded, &snapshots); err != nil {
		var ids []int64
		if err := common.UnmarshalJsonStr(encoded, &ids); err != nil {
			return nil, fmt.Errorf("decode video sample references: %w", err)
		}
		for _, id := range ids {
			if id <= 0 {
				return nil, ErrInvalidVideoSample
			}
		}
		return ids, nil
	}
	ids := make([]int64, 0, len(snapshots))
	for _, snapshot := range snapshots {
		if snapshot.AssetID <= 0 {
			return nil, ErrInvalidVideoSample
		}
		ids = append(ids, snapshot.AssetID)
	}
	return ids, nil
}

func validateVideoSampleReferenceSnapshots(snapshots []VideoSampleReferenceSnapshot) error {
	positions := map[string]map[int]bool{}
	for _, snapshot := range snapshots {
		if snapshot.AssetID <= 0 || snapshot.Position < 0 {
			return ErrInvalidVideoSample
		}
		switch snapshot.Role {
		case model.VideoTaskAssetRoleReferenceImage, model.VideoTaskAssetRoleReferenceVideo, model.VideoTaskAssetRoleReferenceAudio, model.VideoTaskAssetRoleFirstFrame, model.VideoTaskAssetRoleLastFrame:
		default:
			return ErrInvalidVideoSample
		}
		if positions[snapshot.Role] == nil {
			positions[snapshot.Role] = map[int]bool{}
		}
		if positions[snapshot.Role][snapshot.Position] {
			return ErrInvalidVideoSample
		}
		positions[snapshot.Role][snapshot.Position] = true
	}
	for _, rolePositions := range positions {
		for position := range rolePositions {
			if position >= len(rolePositions) {
				return ErrInvalidVideoSample
			}
		}
	}
	return nil
}
