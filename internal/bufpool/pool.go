package bufpool

import "sync"

var sizeClasses = []int{128, 4096, 65536}

type classPool struct {
	size int
	pool *sync.Pool
}

// Pool provides sized byte slices backed by reusable buffers to reduce GC churn.
type Pool struct {
	pools []classPool
}

var defaultPool = New()

// Get acquires a buffer from the package-level default pool.
func Get(size int) []byte {
	return defaultPool.Get(size)
}

// Put releases a buffer back to the package-level default pool.
func Put(buf []byte) {
	defaultPool.Put(buf)
}

// New creates a buffer pool with predefined size classes tailored for RTMP chunking workloads.
func New() *Pool {
	pools := make([]classPool, len(sizeClasses))
	for i, classSize := range sizeClasses {
		size := classSize
		pools[i] = classPool{
			size: size,
			pool: &sync.Pool{
				New: func() any {
					return make([]byte, size)
				},
			},
		}
	}
	return &Pool{pools: pools}
}

// Get returns a byte slice whose length matches the requested size and whose capacity is the
// nearest predefined size class that can accommodate the request. Requests larger than the
// maximum size class allocate a fresh slice without pooling.
func (p *Pool) Get(size int) []byte {
	if p == nil || size <= 0 {
		return nil
	}

	for i := range p.pools {
		class := &p.pools[i]
		if size <= class.size {
			buf := class.pool.Get().([]byte)
			return buf[:size]
		}
	}

	return make([]byte, size)
}

// Put returns the provided buffer to the pool if its capacity matches a predefined size class.
// Buffers that do not match any class are discarded. The buffer is zeroed before reuse to avoid
// leaking data across callers.
func (p *Pool) Put(buf []byte) {
	if p == nil || buf == nil {
		return
	}

	capBuf := cap(buf)
	for i := range p.pools {
		class := &p.pools[i]
		if capBuf == class.size {
			full := buf[:class.size]
			clear(full)
			class.pool.Put(full)
			return
		}
	}
}
