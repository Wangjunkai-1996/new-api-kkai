package service

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

var (
	ErrInvalidImageRelayResponse  = errors.New("invalid image relay response")
	ErrImageRelayResponseTooLarge = errors.New("image relay response exceeds the configured size limit")
)

type ImageRelayResult struct {
	URL           string
	Base64        string
	RevisedPrompt string
}

func ParseImageRelayResponseFile(path string, maxBytes int64, expectedCount int) ([]ImageRelayResult, error) {
	if strings.TrimSpace(path) == "" || maxBytes <= 0 || expectedCount <= 0 {
		return nil, ErrInvalidImageRelayResponse
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 {
		return nil, ErrInvalidImageRelayResponse
	}
	if info.Size() > maxBytes {
		return nil, ErrImageRelayResponseTooLarge
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open image relay response: %w", err)
	}
	defer file.Close()
	var response dto.ImageResponse
	if err := common.DecodeJsonSingle(io.LimitReader(file, maxBytes+1), &response); err != nil {
		return nil, ErrInvalidImageRelayResponse
	}
	if len(response.Data) == 0 || len(response.Data) > expectedCount {
		return nil, ErrInvalidImageRelayResponse
	}
	results := make([]ImageRelayResult, 0, len(response.Data))
	for _, item := range response.Data {
		item.Url = strings.TrimSpace(item.Url)
		item.B64Json = strings.TrimSpace(item.B64Json)
		if (item.Url == "") == (item.B64Json == "") {
			return nil, ErrInvalidImageRelayResponse
		}
		results = append(results, ImageRelayResult{
			URL: item.Url, Base64: item.B64Json, RevisedPrompt: strings.TrimSpace(item.RevisedPrompt),
		})
	}
	return results, nil
}
