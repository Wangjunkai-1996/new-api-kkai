//go:build windows

package service

import "github.com/QuantumNous/new-api/common"

func videoAvailableBytesForPath(string) (uint64, error) {
	info := common.GetDiskSpaceInfo()
	if info.Free == 0 {
		return 0, ErrVideoTemporaryStorageUnavailable
	}
	return info.Free, nil
}
