package service

import (
	"regexp"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/model"
)

var videoMultipartETagPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

func validateCompletedVideoParts(asset model.KKAIVideoAsset, parts []VideoAssetCompletedPart) error {
	expectedParts := videoMultipartPartCount(asset.SizeBytes, asset.UploadPartSize)
	if expectedParts <= 0 || len(parts) != expectedParts {
		return ErrInvalidVideoAssetUpload
	}
	for index, part := range parts {
		if part.PartNumber != int32(index+1) {
			return ErrInvalidVideoAssetUpload
		}
		if _, valid := normalizeVideoMultipartETag(part.ETag); !valid {
			return ErrInvalidVideoAssetUpload
		}
	}
	return nil
}

func validateUploadedVideoParts(asset model.KKAIVideoAsset, parts []VideoAssetUploadedPart, requireComplete bool) error {
	expectedParts := videoMultipartPartCount(asset.SizeBytes, asset.UploadPartSize)
	if expectedParts <= 0 || len(parts) > expectedParts || (requireComplete && len(parts) != expectedParts) {
		return ErrInvalidVideoAssetUpload
	}
	sort.Slice(parts, func(left int, right int) bool { return parts[left].PartNumber < parts[right].PartNumber })
	previousPart := int32(0)
	for _, part := range parts {
		if part.PartNumber <= previousPart || part.PartNumber <= 0 || int(part.PartNumber) > expectedParts ||
			part.SizeBytes != videoMultipartPartLength(asset.SizeBytes, asset.UploadPartSize, int(part.PartNumber)) {
			return ErrInvalidVideoAssetUpload
		}
		if _, valid := normalizeVideoMultipartETag(part.ETag); !valid {
			return ErrInvalidVideoAssetUpload
		}
		previousPart = part.PartNumber
	}
	return nil
}

func normalizeVideoMultipartETag(value string) (string, bool) {
	value = strings.TrimSpace(value)
	startsQuoted := strings.HasPrefix(value, `"`)
	endsQuoted := strings.HasSuffix(value, `"`)
	if startsQuoted != endsQuoted {
		return "", false
	}
	if startsQuoted {
		value = strings.TrimPrefix(value, `"`)
		value = strings.TrimSuffix(value, `"`)
	}
	if !videoMultipartETagPattern.MatchString(value) {
		return "", false
	}
	return strings.ToLower(value), true
}

func videoMultipartPartCount(sizeBytes int64, partSize int64) int {
	if sizeBytes <= 0 || partSize <= 0 {
		return 0
	}
	return int((sizeBytes + partSize - 1) / partSize)
}

func videoMultipartPartLength(sizeBytes int64, partSize int64, partNumber int) int64 {
	if sizeBytes <= 0 || partSize <= 0 || partNumber <= 0 {
		return 0
	}
	offset := int64(partNumber-1) * partSize
	remaining := sizeBytes - offset
	if remaining <= 0 {
		return 0
	}
	if remaining < partSize {
		return remaining
	}
	return partSize
}
