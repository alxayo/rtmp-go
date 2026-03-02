// Package bufpool tests verify the byte-slice pool used to reduce garbage-collection
// pressure when allocating temporary buffers for RTMP chunk reading/writing.
//
// Key concepts for beginners:
//   - A "pool" recycles byte slices so the Go garbage collector doesn't have to
//     allocate and free them on every RTMP message.
//   - Buffers are bucketed into size classes (128, 4096, 65536, …). A request for
//     64 bytes returns a buffer whose *capacity* is rounded up to the next bucket (128).
//   - sync.Pool (used internally) is safe for concurrent access.
package bufpool

import (
	"fmt"
	"sync"
	"testing"
)

// TestPoolGetReturnsSizedBuffer uses a table-driven pattern to verify that
// Pool.Get returns a byte slice whose length matches the requested size and
// whose capacity is rounded up to the nearest size-class bucket.
//
// Table-driven tests are a Go convention: each row in the "tests" slice
// describes one scenario (input + expected output). The loop runs each row
// as a named sub-test via t.Run so failures show exactly which case broke.
func TestPoolGetReturnsSizedBuffer(t *testing.T) {
	// t.Parallel() allows this test to run concurrently with other tests,
	// speeding up the overall test suite.
	t.Parallel()

	p := New()

	// Each entry defines the requested size and the expected capacity after
	// rounding up to the pool's size-class bucket.
	tests := []struct {
		name        string
		requestSize int
		expectCap   int
	}{
		{name: "small", requestSize: 64, expectCap: 128},
		{name: "exact small", requestSize: 128, expectCap: 128},
		{name: "medium", requestSize: 1024, expectCap: 4096},
		{name: "large", requestSize: 5000, expectCap: 65536},
		{name: "oversized", requestSize: 131072, expectCap: 131072},
		{name: "zero", requestSize: 0, expectCap: 0},
	}

	for _, tc := range tests {
		tc := tc // capture loop variable for parallel sub-tests (required in Go < 1.22)
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			buf := p.Get(tc.requestSize)

			// Zero-size is a special case: the pool returns an empty (nil) slice.
			if tc.requestSize == 0 {
				if len(buf) != 0 || cap(buf) != 0 {
					t.Fatalf("expected zero-length buffer, got len=%d cap=%d", len(buf), cap(buf))
				}
				return
			}

			// len must equal the requested size (usable bytes).
			if len(buf) != tc.requestSize {
				t.Fatalf("expected len=%d, got %d", tc.requestSize, len(buf))
			}

			// cap must match the size-class bucket.
			if cap(buf) != tc.expectCap {
				t.Fatalf("expected cap=%d, got %d", tc.expectCap, cap(buf))
			}
		})
	}
}

// TestPoolPutReusesBuffer verifies that returning a buffer to the pool and
// requesting the same size again yields the *same* underlying memory (pointer
// equality). It also checks that the buffer is zeroed on return so stale data
// from one RTMP message doesn't leak into another.
func TestPoolPutReusesBuffer(t *testing.T) {
	t.Parallel()

	p := New()

	// Get a 200-byte buffer (capacity will be 4096 – the next size class).
	buf := p.Get(200)
	if len(buf) != 200 {
		t.Fatalf("expected len=200, got %d", len(buf))
	}
	buf[0] = 42 // write sentinel value to prove zeroing later

	// Save the pointer to the first byte so we can compare it after reuse.
	ptr := &buf[:1][0]
	p.Put(buf) // return to pool

	// Request the same size again – we expect the pool to hand back the same slice.
	reused := p.Get(200)
	if len(reused) != 200 {
		t.Fatalf("expected len=200, got %d", len(reused))
	}

	if cap(reused) != 4096 {
		t.Fatalf("expected cap=4096, got %d", cap(reused))
	}

	// Pointer comparison: both should reference the same backing array.
	if &reused[:1][0] != ptr {
		t.Fatalf("expected to get the same buffer pointer back from pool")
	}

	// Ensure every byte is zero (pool must clear before recycling).
	for i, v := range reused {
		if v != 0 {
			t.Fatalf("expected buffer to be zeroed, found value %d at index %d", v, i)
		}
	}
}

// TestPoolConcurrentAccess is a stress/race-detector test. It spawns 5
// goroutines that each Get/Put 1000 times with different buffer sizes.
// Running with `go test -race` ensures no data races exist in the pool.
//
// Pattern explanation:
//   - sync.WaitGroup tracks when all goroutines have finished.
//   - A buffered error channel collects per-goroutine failures without
//     blocking, so the main goroutine can report them after wg.Wait().
func TestPoolConcurrentAccess(t *testing.T) {
	t.Parallel()

	p := New()
	var wg sync.WaitGroup
	errCh := make(chan error, 5) // one slot per worker goroutine

	// worker performs 1000 Get→fill→Put cycles for a given buffer size.
	worker := func(size int) {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			buf := p.Get(size)
			if len(buf) != size {
				errCh <- fmt.Errorf("expected len=%d, got %d", size, len(buf))
				return
			}
			if cap(buf) < size {
				errCh <- fmt.Errorf("expected cap >= %d, got %d", size, cap(buf))
				return
			}
			// Fill buffer to exercise memory – helps the race detector
			// catch unsynchronized access.
			for j := range buf {
				buf[j] = byte(i)
			}
			p.Put(buf)
		}
	}

	// Spawn one goroutine per size class.
	sizes := []int{64, 512, 2048, 8192, 40000}
	for _, size := range sizes {
		size := size // capture for goroutine closure
		wg.Add(1)
		go worker(size)
	}

	wg.Wait()
	close(errCh)

	// Report any errors collected from workers.
	for err := range errCh {
		t.Fatal(err)
	}
}
