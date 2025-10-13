package bufpool

import (
	"sync"
	"testing"
)

func TestPoolGetReturnsSizedBuffer(t *testing.T) {
	t.Parallel()

	p := New()

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
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			buf := p.Get(tc.requestSize)
			if tc.requestSize == 0 {
				if len(buf) != 0 || cap(buf) != 0 {
					t.Fatalf("expected zero-length buffer, got len=%d cap=%d", len(buf), cap(buf))
				}
				return
			}

			if len(buf) != tc.requestSize {
				t.Fatalf("expected len=%d, got %d", tc.requestSize, len(buf))
			}

			if cap(buf) != tc.expectCap {
				t.Fatalf("expected cap=%d, got %d", tc.expectCap, cap(buf))
			}
		})
	}
}

func TestPoolPutReusesBuffer(t *testing.T) {
	t.Parallel()

	p := New()

	buf := p.Get(200)
	if len(buf) != 200 {
		t.Fatalf("expected len=200, got %d", len(buf))
	}
	buf[0] = 42

	ptr := &buf[:1][0]
	p.Put(buf)

	reused := p.Get(200)
	if len(reused) != 200 {
		t.Fatalf("expected len=200, got %d", len(reused))
	}

	if cap(reused) != 4096 {
		t.Fatalf("expected cap=4096, got %d", cap(reused))
	}

	if &reused[:1][0] != ptr {
		t.Fatalf("expected to get the same buffer pointer back from pool")
	}

	for i, v := range reused {
		if v != 0 {
			t.Fatalf("expected buffer to be zeroed, found value %d at index %d", v, i)
		}
	}
}

func TestPoolConcurrentAccess(t *testing.T) {
	t.Parallel()

	p := New()
	var wg sync.WaitGroup

	worker := func(size int) {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			buf := p.Get(size)
			if len(buf) != size {
				t.Fatalf("expected len=%d, got %d", size, len(buf))
			}
			if cap(buf) < size {
				t.Fatalf("expected cap >= %d, got %d", size, cap(buf))
			}
			for j := range buf {
				buf[j] = byte(i)
			}
			p.Put(buf)
		}
	}

	sizes := []int{64, 512, 2048, 8192, 40000}
	for _, size := range sizes {
		size := size
		wg.Add(1)
		go worker(size)
	}

	wg.Wait()
}
