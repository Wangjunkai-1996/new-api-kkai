package main

import "testing"

func TestChannelCacheStartupPlan(t *testing.T) {
	tests := []struct {
		name                    string
		memoryCacheEnabled      bool
		backgroundTasksDisabled bool
		wantInitOnce            bool
		wantSyncInBackground    bool
	}{
		{
			name:                    "memory cache disabled",
			memoryCacheEnabled:      false,
			backgroundTasksDisabled: false,
			wantInitOnce:            false,
			wantSyncInBackground:    false,
		},
		{
			name:                    "memory cache sync enabled",
			memoryCacheEnabled:      true,
			backgroundTasksDisabled: false,
			wantInitOnce:            true,
			wantSyncInBackground:    true,
		},
		{
			name:                    "background tasks disabled still initializes cache",
			memoryCacheEnabled:      true,
			backgroundTasksDisabled: true,
			wantInitOnce:            true,
			wantSyncInBackground:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			initOnce, syncInBackground := channelCacheStartupPlan(tt.memoryCacheEnabled, tt.backgroundTasksDisabled)
			if initOnce != tt.wantInitOnce {
				t.Fatalf("initOnce = %v, want %v", initOnce, tt.wantInitOnce)
			}
			if syncInBackground != tt.wantSyncInBackground {
				t.Fatalf("syncInBackground = %v, want %v", syncInBackground, tt.wantSyncInBackground)
			}
		})
	}
}
