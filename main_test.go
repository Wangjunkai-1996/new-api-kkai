package main

import "testing"

func TestChannelCacheStartupPlan(t *testing.T) {
	tests := []struct {
		name                 string
		memoryCacheEnabled   bool
		wantInitOnce         bool
		wantSyncInBackground bool
	}{
		{
			name:                 "memory cache disabled",
			memoryCacheEnabled:   false,
			wantInitOnce:         false,
			wantSyncInBackground: false,
		},
		{
			name:                 "memory cache always stays synchronized",
			memoryCacheEnabled:   true,
			wantInitOnce:         true,
			wantSyncInBackground: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			initOnce, syncInBackground := channelCacheStartupPlan(tt.memoryCacheEnabled)
			if initOnce != tt.wantInitOnce {
				t.Fatalf("initOnce = %v, want %v", initOnce, tt.wantInitOnce)
			}
			if syncInBackground != tt.wantSyncInBackground {
				t.Fatalf("syncInBackground = %v, want %v", syncInBackground, tt.wantSyncInBackground)
			}
		})
	}
}
