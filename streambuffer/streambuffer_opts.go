package streambuffer

import "time"

// StreamBufferOption defines an option for configuring StreamBuffer.
type StreamBufferOption func(*StreamBuffer)

// WithWindow sets the time window for frame retention.
func WithWindow(d time.Duration) StreamBufferOption {
	return func(sb *StreamBuffer) {
		if d > 0 {
			sb.window = d
		}
	}
}

// WithCapacity sets the maximum number of frames the buffer can hold.
func WithCapacity(n int) StreamBufferOption {
	return func(sb *StreamBuffer) {
		if n > 0 {
			sb.capacity = n
		}
	}
}

// WithFrameSize sets the expected frame size hint for memory allocation.
func WithFrameSize(size int) StreamBufferOption {
	return func(sb *StreamBuffer) {
		if size > 0 {
			sb.frameSize = size
		}
	}
}

// WithMaxRecycleSize sets the maximum buffer size to recycle.
func WithMaxRecycleSize(size int) StreamBufferOption {
	return func(sb *StreamBuffer) {
		if size > 0 {
			sb.maxRecycleSize = size
		}
	}
}

// WithInputBuffer sets the input channel buffer capacity.
func WithInputBuffer(size int) StreamBufferOption {
	return func(sb *StreamBuffer) {
		if sb.input == nil && size > 0 {
			sb.input = make(chan []byte, size)
		}
	}
}
