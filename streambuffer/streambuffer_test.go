package streambuffer

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamBufferInitialization(t *testing.T) {
	// test default settings
	sb := NewStreamBuffer()
	assert.Equal(t, 30*time.Second, sb.window, "should use default window duration")
	assert.Equal(t, 300, sb.capacity, "should use default capacity")
	assert.False(t, sb.running.Load(), "should not be running initially")

	// test custom options
	customWindow := 10 * time.Second
	customCapacity := 200
	customFrameSize := 2 * 1024 * 1024
	customRecycleSize := 4 * 1024 * 1024
	customInputBuffer := 50

	sb = NewStreamBuffer(
		WithWindow(customWindow),
		WithCapacity(customCapacity),
		WithFrameSize(customFrameSize),
		WithMaxRecycleSize(customRecycleSize),
		WithInputBuffer(customInputBuffer),
	)

	assert.Equal(t, customWindow, sb.window, "should use custom window")
	assert.Equal(t, customCapacity, sb.capacity, "should use custom capacity")
	assert.Equal(t, customFrameSize, sb.frameSize, "should use custom frame size")
	assert.Equal(t, customRecycleSize, sb.bufferPool.maxSize, "should use custom recycle size")
	assert.Equal(t, customInputBuffer, cap(sb.input), "should use custom input buffer size")
}

func TestStreamBufferStartStop(t *testing.T) {
	sb := NewStreamBuffer()
	assert.False(t, sb.IsRunning(), "should not be running before start")

	sb.Start()
	assert.True(t, sb.IsRunning(), "should be running after start")

	// verify idempotent start
	sb.Start()
	assert.True(t, sb.IsRunning(), "should still be running after second start")

	sb.Stop()
	assert.False(t, sb.IsRunning(), "should not be running after stop")

	// verify idempotent stop
	sb.Stop()
	assert.False(t, sb.IsRunning(), "should still not be running after second stop")
}

func TestStreamBufferBasicFrameProcessing(t *testing.T) {
	sb := NewStreamBuffer(WithWindow(5 * time.Second))
	sb.Start()
	defer sb.Stop()

	input := sb.Input()

	// add test frames with timestamps
	for i := range 10 {
		frame := fmt.Appendf(nil, "Frame %d with some data", i)
		input <- frame
		time.Sleep(100 * time.Millisecond) // ensure sequential timestamps
	}

	// wait for processing
	time.Sleep(100 * time.Millisecond)

	// get and verify snapshot
	ctx := context.Background()
	snapshot, err := sb.GetSnapshot(ctx)
	require.NoError(t, err, "should get snapshot without error")
	assert.Len(t, snapshot.Frames, 10, "snapshot should contain all 10 frames")

	// verify frame content and metadata
	for i, frame := range snapshot.Frames {
		expectedData := fmt.Sprintf("Frame %d with some data", i)
		assert.Equal(t, expectedData, string(frame.Data), "frame data should match")
		assert.Equal(t, uint64(i), frame.Sequence, "sequence numbers should be in order")

		if i > 0 {
			assert.True(t, frame.Timestamp.After(snapshot.Frames[i-1].Timestamp),
				"frame timestamps should be in chronological order")
		}
	}

	// verify snapshot boundaries
	assert.Equal(t, snapshot.Frames[0].Timestamp, snapshot.StartTime, "snapshot start time should match first frame")
	assert.Equal(t, snapshot.Frames[9].Timestamp, snapshot.EndTime, "snapshot end time should match last frame")

	// check metrics
	metrics := sb.GetMetrics()
	assert.Equal(t, uint64(10), metrics.FramesProcessed, "should have processed 10 frames")
	assert.Equal(t, uint64(0), metrics.FramesDropped, "should not have dropped any frames")
	assert.Equal(t, uint64(0), metrics.FramesTrimmed, "should not have trimmed any frames")
	assert.Equal(t, uint64(1), metrics.SnapshotsSent, "should have sent 1 snapshot")
}

func TestStreamBufferTimeWindowTrimming(t *testing.T) {
	sb := NewStreamBuffer(
		WithWindow(2*time.Second),
		WithCapacity(100), // prevent capacity-based trimming
	)
	sb.Start()
	defer sb.Stop()

	input := sb.Input()

	// phase 1: add initial frames
	for i := range 5 {
		input <- fmt.Appendf(nil, "Early frame %d", i)
	}

	// wait 1.5s (frames still within window)
	time.Sleep(1500 * time.Millisecond)

	// phase 2: add middle frames
	for i := range 5 {
		input <- fmt.Appendf(nil, "Middle frame %d", i)
	}

	// wait 1s more (early frames now outside 2s window)
	time.Sleep(1000 * time.Millisecond)

	// phase 3: add final frames
	for i := range 5 {
		input <- fmt.Appendf(nil, "Late frame %d", i)
	}

	time.Sleep(100 * time.Millisecond)

	// verify trimming behavior
	ctx := context.Background()
	snapshot, err := sb.GetSnapshot(ctx)
	require.NoError(t, err)

	// verify old frames removed
	for _, frame := range snapshot.Frames {
		assert.NotContains(t, string(frame.Data), "Early frame",
			"early frames should have been trimmed")
	}

	assert.Equal(t, 10, len(snapshot.Frames), "should have 10 frames (middle + late)")

	// verify trim metrics
	metrics := sb.GetMetrics()
	assert.Equal(t, uint64(15), metrics.FramesProcessed, "should have processed 15 frames")
	assert.Equal(t, uint64(5), metrics.FramesTrimmed, "should have trimmed 5 frames")
}

func TestStreamBufferCapacityAndOverflow(t *testing.T) {
	sb := NewStreamBuffer(
		WithWindow(1*time.Hour), // isolate capacity-based pruning
		WithCapacity(5),         // small capacity to force overflow
	)
	sb.Start()
	defer sb.Stop()

	input := sb.Input()

	// add twice the capacity
	for i := range 10 {
		input <- fmt.Appendf(nil, "Frame %d", i)
		time.Sleep(10 * time.Millisecond)
	}

	time.Sleep(100 * time.Millisecond)

	// verify circular buffer behavior
	ctx := context.Background()
	snapshot, err := sb.GetSnapshot(ctx)
	require.NoError(t, err)

	assert.Equal(t, 5, len(snapshot.Frames), "snapshot should contain only the last 5 frames")

	// verify only newest frames remain
	for i, frame := range snapshot.Frames {
		expectedIdx := i + 5
		expectedData := fmt.Sprintf("Frame %d", expectedIdx)
		assert.Equal(t, expectedData, string(frame.Data),
			"frame data should match the latest frames")
	}

	// check buffer stats
	metrics := sb.GetMetrics()
	assert.Equal(t, uint64(10), metrics.FramesProcessed, "should have processed all 10 frames")
	assert.Equal(t, 5, metrics.FrameCount, "should have 5 frames in buffer")
	assert.Equal(t, 1.0, metrics.BufferUtilization, "buffer should be 100% utilized")
}

func TestStreamBufferConcurrency(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping concurrency test in short mode")
	}

	sb := NewStreamBuffer(
		WithCapacity(1000),
		WithInputBuffer(1000),
	)
	sb.Start()
	defer sb.Stop()

	const numProducers = 5
	const framesPerProducer = 1000
	const snapshotCount = 10

	var wg sync.WaitGroup
	wg.Add(numProducers + 1)

	// run multiple producers concurrently
	for p := range numProducers {
		go func(producerID int) {
			defer wg.Done()

			input := sb.Input()
			for i := range framesPerProducer {
				data := fmt.Appendf(nil, "Producer %d - Frame %d", producerID, i)
				input <- data

				// introduce timing variability
				if rand.Intn(100) < 10 {
					time.Sleep(time.Duration(rand.Intn(5)) * time.Millisecond)
				}
			}
		}(p)
	}

	// concurrent snapshot reader
	go func() {
		defer wg.Done()

		for range snapshotCount {
			time.Sleep(100 * time.Millisecond)

			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			snapshot, err := sb.GetSnapshot(ctx)
			cancel()

			if err != nil {
				t.Errorf("Failed to get snapshot: %v", err)
				continue
			}

			if len(snapshot.Frames) == 0 {
				t.Error("Got empty snapshot")
			}
		}
	}()

	wg.Wait()

	// verify total throughput
	metrics := sb.GetMetrics()
	assert.Equal(t, uint64(numProducers*framesPerProducer), metrics.FramesProcessed,
		"should have processed all frames")
	assert.GreaterOrEqual(t, metrics.SnapshotsSent, uint64(snapshotCount),
		"should have sent at least the requested snapshots")
}

func TestStreamBufferContextCancellation(t *testing.T) {
	sb := NewStreamBuffer()
	sb.Start()
	defer sb.Stop()

	// prepare test data
	input := sb.Input()
	for i := range 5 {
		input <- fmt.Appendf(nil, "Frame %d", i)
	}

	// case 1: already canceled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := sb.GetSnapshot(ctx)
	assert.Error(t, err, "snapshot with cancelled context should return error")

	// case 2: immediately expiring timeout
	ctx, cancel = context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	_, err = sb.GetSnapshot(ctx)
	assert.Error(t, err, "snapshot with short timeout should return error")

	// allow processing to complete
	time.Sleep(100 * time.Millisecond)

	// case 3: successful retrieval
	ctx = context.Background()
	snapshot, err := sb.GetSnapshot(ctx)
	assert.NoError(t, err, "snapshot with valid context should succeed")
	assert.Len(t, snapshot.Frames, 5, "should have all 5 frames")
}

func TestStreamBufferShutdown(t *testing.T) {
	sb := NewStreamBuffer()
	sb.Start()

	// populate buffer
	input := sb.Input()
	for i := range 5 {
		input <- fmt.Appendf(nil, "Frame %d", i)
	}

	// queue additional frames
	var framesQueued int
	additionalFrames := 100
FrameLoop:
	for i := range additionalFrames {
		select {
		case input <- fmt.Appendf(nil, "Extra frame %d", i):
			// successfully queued
			framesQueued++
		default:
			// channel full
			break FrameLoop
		}
	}

	// stop with pending work
	sb.Stop()

	// verify operations fail after stop
	_, err := sb.GetSnapshot(context.Background())
	assert.Error(t, err, "should not be able to get snapshot after stopping")

	// verify restart prevention
	sb.Start()
	time.Sleep(10 * time.Millisecond)
	assert.False(t, sb.IsRunning(), "should not be able to restart after buffer is stopped")
}

// TestBufferPoolRecycling verifies buffer recycling respects size limits.
func TestBufferPoolRecycling(t *testing.T) {
	customMaxSize := 2 * 1024 * 1024 // 2MB recycling threshold
	sb := NewStreamBuffer(
		WithMaxRecycleSize(customMaxSize),
	)
	sb.Start()
	defer sb.Stop()

	input := sb.Input()

	// small frame (below recycling threshold)
	smallData := make([]byte, 1024)
	rand.Read(smallData)
	input <- smallData

	// large frame (exceeds recycling threshold)
	largeData := make([]byte, 3*1024*1024)
	rand.Read(largeData)
	input <- largeData

	time.Sleep(100 * time.Millisecond)

	// verify both frames are stored
	snapshot, err := sb.GetSnapshot(context.Background())
	require.NoError(t, err)

	assert.Equal(t, 2, len(snapshot.Frames), "should have both frames")
	assert.Equal(t, 1024, len(snapshot.Frames[0].Data), "first frame should be small")
	assert.Equal(t, 3*1024*1024, len(snapshot.Frames[1].Data), "second frame should be large")
}
