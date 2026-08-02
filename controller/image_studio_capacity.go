package controller

import "sync/atomic"

var imageStudioSubmissionsInFlight atomic.Int64

func acquireImageStudioSubmissionSlot(limit int) bool {
	if limit <= 0 {
		return false
	}
	for {
		current := imageStudioSubmissionsInFlight.Load()
		if current >= int64(limit) {
			return false
		}
		if imageStudioSubmissionsInFlight.CompareAndSwap(current, current+1) {
			return true
		}
	}
}

func releaseImageStudioSubmissionSlot() {
	imageStudioSubmissionsInFlight.Add(-1)
}
