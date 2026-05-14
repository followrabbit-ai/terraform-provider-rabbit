package mutex

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestKV_serialisesSameKey holds a lock for a known duration on goroutines that
// share the same key. The total elapsed time must scale with the number of
// goroutines: N×hold time, plus slack.
func TestKV_serialisesSameKey(t *testing.T) {
	t.Parallel()
	const goroutines = 8
	const hold = 25 * time.Millisecond

	kv := New()
	var wg sync.WaitGroup
	start := time.Now()
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			kv.Lock("k")
			time.Sleep(hold)
			kv.Unlock("k")
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)
	minExpected := time.Duration(goroutines) * hold
	if elapsed < minExpected {
		t.Fatalf("expected at least %s elapsed for serialised locks, got %s", minExpected, elapsed)
	}
}

// TestKV_independentKeys ensures different keys do not block each other.
func TestKV_independentKeys(t *testing.T) {
	t.Parallel()
	const goroutines = 8
	const hold = 25 * time.Millisecond

	kv := New()
	var wg sync.WaitGroup
	start := time.Now()
	for i := 0; i < goroutines; i++ {
		key := string(rune('a' + i))
		wg.Add(1)
		go func() {
			defer wg.Done()
			kv.Lock(key)
			time.Sleep(hold)
			kv.Unlock(key)
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)
	// Independent locks should finish in ~hold (with slack), not goroutines*hold.
	if elapsed > 4*hold {
		t.Fatalf("independent locks shouldn't serialise; took %s for hold %s", elapsed, hold)
	}
}

// TestKV_race triggers the race detector on contended Lock/Unlock cycles
// across many keys.
func TestKV_race(t *testing.T) {
	t.Parallel()
	kv := New()
	var counter atomic.Int64
	var wg sync.WaitGroup
	for k := 0; k < 4; k++ {
		key := string(rune('a' + k))
		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func(key string) {
				defer wg.Done()
				kv.Lock(key)
				counter.Add(1)
				kv.Unlock(key)
			}(key)
		}
	}
	wg.Wait()
	if got := counter.Load(); got != 4*50 {
		t.Fatalf("counter got=%d want=%d", got, 4*50)
	}
}
