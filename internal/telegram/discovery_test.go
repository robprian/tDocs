package telegram

import (
	"bufio"
	"strings"
	"testing"
	"time"
)

func TestPromptYesNo(t *testing.T) {
	tests := []struct {
		input      string
		defaultYes bool
		expected   bool
	}{
		{"\n", true, true},
		{"\n", false, false},
		{"y\n", false, true},
		{"Y\n", false, true},
		{"yes\n", false, true},
		{"YES\n", false, true},
		{"n\n", true, false},
		{"N\n", true, false},
		{"no\n", true, false},
		{"other\n", true, false},
		{"other\n", false, false},
	}

	for _, tc := range tests {
		reader := bufio.NewReader(strings.NewReader(tc.input))
		res := promptYesNo(reader, "Test prompt: ", tc.defaultYes)
		if res != tc.expected {
			t.Errorf("promptYesNo(%q, %v) = %v; want %v", tc.input, tc.defaultYes, res, tc.expected)
		}
	}
}

func TestDiscoveredChannelStructure(t *testing.T) {
	cand := DiscoveredChannel{
		ID:            123456789,
		AccessHash:    987654321,
		Title:         "TeleDrive Vault",
		CreatedAt:     time.Now(),
		SnapshotCount: 2,
		LatestSnapshot: &SnapshotInfo{
			MessageID: 42,
			FileName:  "teledrive-backup-20260910-120000.db.gz",
			Size:      1024 * 1024,
		},
	}

	if cand.ID != 123456789 || cand.SnapshotCount != 2 || cand.LatestSnapshot.MessageID != 42 {
		t.Errorf("DiscoveredChannel struct fields mismatch: %+v", cand)
	}
}
