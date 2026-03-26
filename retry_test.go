package nekosql

import (
	"context"
	"testing"
	"time"
)

func TestRetry(t *testing.T) {
	attempts := 0
	err := Retry(context.Background(), 3, time.Millisecond, func() error {
		attempts++
		if attempts < 3 {
			return ErrConflict
		}
		return nil
	})
	if err != nil {
		t.Fatalf("retry returned error: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
}
