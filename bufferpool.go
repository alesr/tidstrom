package tidstrom

import "sync"

// bufferPool provides a pool of reusable byte slices to reduce GC pressure.
type bufferPool struct {
	pool    sync.Pool
	maxSize int
}

// bufferPoolOption defines an option for configuring bufferPool.
type bufferPoolOption func(*bufferPool)

// withMaxBufferSize sets the maximum size of buffers that will be recycled.
func withMaxBufferSize(maxSize int) bufferPoolOption {
	return func(bp *bufferPool) {
		if maxSize > 0 {
			bp.maxSize = maxSize
		}
	}
}

// newBufferPool creates a new buffer pool with the given size hint and optional configurations.
func newBufferPool(sizeHint int, opts ...bufferPoolOption) *bufferPool {
	bp := bufferPool{
		pool: sync.Pool{
			New: func() any {
				return make([]byte, 0, sizeHint)
			},
		},
		maxSize: defaultMaxBufferSize,
	}
	for _, opt := range opts {
		opt(&bp)
	}
	return &bp
}

// get retrieves a byte slice from the pool.
func (p *bufferPool) get() []byte {
	buf := p.pool.Get().([]byte)
	return buf[:0] // preserve capacity
}

// put returns a buffer to the pool if it's not too large.
func (p *bufferPool) put(buf []byte) {
	if buf != nil && cap(buf) <= p.maxSize {
		p.pool.Put(buf)
	}
}
