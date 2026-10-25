package telegram

import (
	"testing"
)

func TestParseRange(t *testing.T) {
	fileSize := int64(1000)

	// 1. Empty range -> full file
	start, end, hasRange, err := ParseRange("", fileSize)
	if err != nil || hasRange || start != 0 || end != 999 {
		t.Fatalf("Empty range test failed: start=%d end=%d hasRange=%v err=%v", start, end, hasRange, err)
	}

	// 2. Explicit range
	start, end, hasRange, err = ParseRange("bytes=100-200", fileSize)
	if err != nil || !hasRange || start != 100 || end != 200 {
		t.Fatalf("Explicit range failed: start=%d end=%d hasRange=%v err=%v", start, end, hasRange, err)
	}

	// 3. Open-ended range
	start, end, hasRange, err = ParseRange("bytes=500-", fileSize)
	if err != nil || !hasRange || start != 500 || end != 999 {
		t.Fatalf("Open-ended range failed: start=%d end=%d hasRange=%v err=%v", start, end, hasRange, err)
	}

	// 4. Suffix range
	start, end, hasRange, err = ParseRange("bytes=-200", fileSize)
	if err != nil || !hasRange || start != 800 || end != 999 {
		t.Fatalf("Suffix range failed: start=%d end=%d hasRange=%v err=%v", start, end, hasRange, err)
	}

	// 5. Invalid ranges
	_, _, _, err = ParseRange("bytes=800-200", fileSize)
	if err == nil {
		t.Fatalf("Expected error for inverted range 800-200, got nil")
	}

	_, _, _, err = ParseRange("bytes=2000-", fileSize)
	if err == nil {
		t.Fatalf("Expected error for out-of-bounds start, got nil")
	}
}
