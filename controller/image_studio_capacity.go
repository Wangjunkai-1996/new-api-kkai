package controller

import "sync"

type imageStudioSubmissionCapacity struct {
	mu     sync.Mutex
	total  int
	byUser map[int]int
}

var imageStudioCapacity = imageStudioSubmissionCapacity{
	byUser: make(map[int]int),
}

func (capacity *imageStudioSubmissionCapacity) acquire(userID int, globalLimit int, perUserLimit int) bool {
	if userID <= 0 || globalLimit <= 0 || perUserLimit <= 0 {
		return false
	}
	capacity.mu.Lock()
	defer capacity.mu.Unlock()
	if capacity.total >= globalLimit || capacity.byUser[userID] >= perUserLimit {
		return false
	}
	if capacity.byUser == nil {
		capacity.byUser = make(map[int]int)
	}
	capacity.total++
	capacity.byUser[userID]++
	return true
}

func (capacity *imageStudioSubmissionCapacity) release(userID int) {
	if userID <= 0 {
		return
	}
	capacity.mu.Lock()
	defer capacity.mu.Unlock()
	current := capacity.byUser[userID]
	if current <= 0 {
		return
	}
	if current == 1 {
		delete(capacity.byUser, userID)
	} else {
		capacity.byUser[userID] = current - 1
	}
	capacity.total--
}
