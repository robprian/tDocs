package telegram

import (
	"context"
	"testing"
	"time"
)

func TestSafeLimiter_PaceAndAcquire(t *testing.T) {
	limiter := NewSafeLimiter()
	limiter.pacingDelay = 10 * time.Millisecond

	ctx := context.Background()

	// Verify pacing delay
	start := time.Now()
	if err := limiter.Pace(ctx); err != nil {
		t.Fatalf("Pace failed: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed < 8*time.Millisecond {
		t.Fatalf("Pace completed too quickly: %v", elapsed)
	}

	// Verify sequential lock
	unlock1 := limiter.AcquireUpload()
	unlocked := false
	go func() {
		time.Sleep(20 * time.Millisecond)
		unlocked = true
		unlock1()
	}()

	unlock2 := limiter.AcquireUpload()
	defer unlock2()

	if !unlocked {
		t.Fatalf("AcquireUpload did not block until previous unlock!")
	}
}
