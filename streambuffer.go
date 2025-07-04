// Package tidstrom provides a high-performance time-based buffer for time-series data.
package tidstrom

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// defaultMaxBufferSize is the threshold for buffer recycling.
	defaultMaxBufferSize = 8 * 1024 * 1024 // 8MB

	// defaultWindowDuration is the default retention period.
	defaultWindowDuration = 30 * time.Second

	// defaultBufferCapacity is the default frame capacity.
	defaultBufferCapacity = 300
)

// Frame represents a single data entry with timing and sequence metadata.
type Frame struct {
	Data      []byte    // actual frame data
	Timestamp time.Time // capture time
	Sequence  uint64    // unique monotonic ID
}

// Snapshot contains a point-in-time copy of frames within the buffer.
type Snapshot struct {
	Frames    []Frame   // ordered collection of frames
	StartTime time.Time // timestamp of oldest frame
	EndTime   time.Time // timestamp of newest frame
	Timestamp time.Time // when snapshot was created
}

// snapshotRequest bundles the context and result channel for a snapshot request.
type snapshotRequest struct {
	resultChan chan<- Snapshot // where to send the result
	ctx        context.Context // for cancellation
}

// StreamBuffer continuously processes incoming data frames, maintaining
// a time window of recent frames that can be captured as snapshots on demand.
type StreamBuffer struct {
	// configuration
	window         time.Duration
	capacity       int
	bufferPool     *bufferPool
	frameSize      int // hint for expected frame size
	maxRecycleSize int // maximum size of buffers to recycle

	// internal state
	frames       []Frame     // circular buffer
	head         int         // next write position
	count        int         // valid frame count
	nextSeq      uint64      // sequence counter
	running      atomic.Bool // running state
	finalStopped atomic.Bool // permanent stop flag

	// synchronization
	mu         sync.RWMutex
	shutdownMu sync.Mutex

	// channels
	input    chan []byte          // incoming frames
	snapReq  chan snapshotRequest // snapshot requests
	shutdown chan struct{}

	// metrics
	framesProcessed atomic.Uint64
	framesDropped   atomic.Uint64
	framesTrimmed   atomic.Uint64
	snapshotsSent   atomic.Uint64
	creationTime    time.Time
	lastFrameTime   time.Time
}

// NewStreamBuffer creates a new StreamBuffer with the specified options.
// The returned StreamBuffer is not started; call Start() to begin processing.
func NewStreamBuffer(opts ...StreamBufferOption) *StreamBuffer {
	sb := &StreamBuffer{
		window:         defaultWindowDuration,
		capacity:       defaultBufferCapacity,
		frameSize:      1024 * 1024, // 1MB
		maxRecycleSize: defaultMaxBufferSize,
		nextSeq:        0,
		creationTime:   time.Now(),
		lastFrameTime:  time.Time{},
		snapReq:        make(chan snapshotRequest, 10),
		shutdown:       make(chan struct{}),
	}

	for _, opt := range opts {
		opt(sb)
	}

	sb.frames = make([]Frame, sb.capacity)
	sb.bufferPool = newBufferPool(sb.frameSize, withMaxBufferSize(sb.maxRecycleSize))

	if sb.input == nil {
		sb.input = make(chan []byte, 100)
	}
	return sb
}

// Start begins processing incoming frames in a background goroutine.
func (sb *StreamBuffer) Start() {
	if sb.finalStopped.Load() {
		return // prevent restart after Stop
	}

	if sb.running.CompareAndSwap(false, true) {
		sb.shutdownMu.Lock()
		if sb.shutdown == nil {
			sb.shutdown = make(chan struct{})
		}
		sb.shutdownMu.Unlock()
		go sb.processLoop()
	}
}

// Stop halts processing and releases resources. Once stopped, the buffer cannot be restarted.
func (sb *StreamBuffer) Stop() {
	if sb.running.CompareAndSwap(true, false) {
		sb.finalStopped.Store(true)

		sb.shutdownMu.Lock()
		if sb.shutdown != nil {
			close(sb.shutdown)
			sb.shutdown = nil
		}
		sb.shutdownMu.Unlock()

		sb.mu.Lock()
		for i := range sb.count {
			idx := (sb.head - sb.count + i + sb.capacity) % sb.capacity
			if sb.frames[idx].Data != nil {
				sb.bufferPool.put(sb.frames[idx].Data)
				sb.frames[idx].Data = nil
			}
		}
		sb.mu.Unlock()
	}
}

// processLoop is the main event loop handling frames and snapshot requests.
func (sb *StreamBuffer) processLoop() {
	defer func() {
		sb.running.Store(false)
	}()

	for {
		sb.shutdownMu.Lock()
		shutdownCh := sb.shutdown
		sb.shutdownMu.Unlock()

		if shutdownCh == nil {
			return
		}

		select {
		case <-shutdownCh:
			return

		case frame, ok := <-sb.input:
			if !ok {
				return
			}
			sb.processFrame(frame)

		case req := <-sb.snapReq:
			select {
			case <-req.ctx.Done():
				// context already canceled
			default:
				snapshot := sb.createSnapshot()
				select {
				case req.resultChan <- snapshot:
					sb.snapshotsSent.Add(1)
				case <-req.ctx.Done():
					// free snapshot memory on cancellation
					for i := range snapshot.Frames {
						if snapshot.Frames[i].Data != nil {
							sb.bufferPool.put(snapshot.Frames[i].Data)
							snapshot.Frames[i].Data = nil
						}
					}
				}
			}
		}
	}
}

// processFrame adds a new frame to the buffer and trims old frames.
func (sb *StreamBuffer) processFrame(data []byte) {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	now := time.Now()

	if sb.count == sb.capacity {
		// recycle memory from the frame we're about to overwrite
		oldestIdx := sb.head % sb.capacity
		if sb.frames[oldestIdx].Data != nil {
			sb.bufferPool.put(sb.frames[oldestIdx].Data)
			sb.frames[oldestIdx].Data = nil
		}
	}

	// store copy of frame data
	newBuf := sb.bufferPool.get()
	newBuf = append(newBuf, data...)

	frame := Frame{
		Data:      newBuf,
		Timestamp: now,
		Sequence:  sb.nextSeq,
	}
	sb.nextSeq++

	// add to circular buffer
	sb.frames[sb.head] = frame
	sb.head = (sb.head + 1) % sb.capacity

	if sb.count < sb.capacity {
		sb.count++
	}

	sb.framesProcessed.Add(1)
	sb.lastFrameTime = now

	// trim frames older than the window duration
	cutoff := now.Add(-sb.window)
	oldest := (sb.head - sb.count + sb.capacity) % sb.capacity
	trimmed := 0

	for i := range sb.count {
		idx := (oldest + i) % sb.capacity
		if !sb.frames[idx].Timestamp.Before(cutoff) {
			break // remaining frames are still within the window
		}

		// recycle buffer
		if sb.frames[idx].Data != nil {
			sb.bufferPool.put(sb.frames[idx].Data)
			sb.frames[idx].Data = nil
		}
		trimmed++
	}
	if trimmed > 0 {
		sb.count -= trimmed
		sb.framesTrimmed.Add(uint64(trimmed))
	}
}

// createSnapshot returns a deep copy of the current buffer contents.
func (sb *StreamBuffer) createSnapshot() Snapshot {
	sb.mu.RLock()
	defer sb.mu.RUnlock()

	if sb.count == 0 {
		return Snapshot{
			Frames:    []Frame{},
			StartTime: time.Time{},
			EndTime:   time.Time{},
			Timestamp: time.Now(),
		}
	}

	frames := make([]Frame, sb.count)
	oldest := (sb.head - sb.count + sb.capacity) % sb.capacity
	var startTime, endTime time.Time

	for i := range sb.count {
		srcIdx := (oldest + i) % sb.capacity
		srcFrame := sb.frames[srcIdx]

		// make a deep copy of frame data
		dataCopy := sb.bufferPool.get()
		dataCopy = append(dataCopy, srcFrame.Data...)

		frames[i] = Frame{
			Data:      dataCopy,
			Timestamp: srcFrame.Timestamp,
			Sequence:  srcFrame.Sequence,
		}

		if i == 0 {
			startTime = srcFrame.Timestamp
		}
		if i == sb.count-1 {
			endTime = srcFrame.Timestamp
		}
	}

	return Snapshot{
		Frames:    frames,
		StartTime: startTime,
		EndTime:   endTime,
		Timestamp: time.Now(),
	}
}

// Input returns the channel to which data should be sent.
// The StreamBuffer will continuously process data from this channel.
func (sb *StreamBuffer) Input() chan<- []byte {
	return sb.input
}

// GetSnapshot returns a point-in-time copy of the buffer contents.
// It respects context cancellation for timeout support.
func (sb *StreamBuffer) GetSnapshot(ctx context.Context) (Snapshot, error) {
	if !sb.running.Load() || sb.finalStopped.Load() {
		return Snapshot{}, errors.New("stream buffer is not running")
	}

	sb.shutdownMu.Lock()
	hasShutdown := sb.shutdown != nil
	sb.shutdownMu.Unlock()

	if !hasShutdown {
		return Snapshot{}, errors.New("stream buffer is shutting down")
	}

	resultChan := make(chan Snapshot, 1)
	req := snapshotRequest{
		resultChan: resultChan,
		ctx:        ctx,
	}

	select {
	case sb.snapReq <- req:
	case <-ctx.Done():
		return Snapshot{}, ctx.Err()
	}

	select {
	case snapshot := <-resultChan:
		return snapshot, nil
	case <-ctx.Done():
		return Snapshot{}, ctx.Err()
	}
}

// Metrics contains performance statistics for a StreamBuffer.
type Metrics struct {
	FramesProcessed   uint64        // total frames added
	FramesDropped     uint64        // frames dropped due to buffer full
	FramesTrimmed     uint64        // frames removed due to age
	SnapshotsSent     uint64        // snapshots successfully delivered
	BufferUtilization float64       // current buffer fullness (0.0-1.0)
	Uptime            time.Duration // time since creation
	FrameCount        int           // current frame count
	Capacity          int           // maximum frames
	WindowDuration    time.Duration // retention window
	LastFrameTime     time.Time     // timestamp of newest frame
}

// GetMetrics returns current performance statistics.
func (sb *StreamBuffer) GetMetrics() Metrics {
	sb.mu.RLock()
	count := sb.count
	capacity := sb.capacity
	sb.mu.RUnlock()

	utilization := 0.0
	if capacity > 0 {
		utilization = float64(count) / float64(capacity)
	}
	return Metrics{
		FramesProcessed:   sb.framesProcessed.Load(),
		FramesDropped:     sb.framesDropped.Load(),
		FramesTrimmed:     sb.framesTrimmed.Load(),
		SnapshotsSent:     sb.snapshotsSent.Load(),
		BufferUtilization: utilization,
		Uptime:            time.Since(sb.creationTime),
		FrameCount:        count,
		Capacity:          capacity,
		WindowDuration:    sb.window,
		LastFrameTime:     sb.lastFrameTime,
	}
}

// IsRunning returns whether the StreamBuffer is currently running.
func (sb *StreamBuffer) IsRunning() bool {
	return sb.running.Load() && !sb.finalStopped.Load()
}
