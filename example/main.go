// Example demonstrates using a StreamBuffer to capture the last 30 seconds of video.
package main

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/alesr/tidstrom/streambuffer"
)

func main() {
	fmt.Println("StreamBuffer Example: Volleyball Camera Recorder")
	fmt.Println("Press Ctrl+C to exit or wait for simulation to complete")
	fmt.Println("------------------------------------------------")

	// buffer configuration for 30 seconds of 30fps video at ~1MB per frame
	buffer := streambuffer.NewStreamBuffer(
		streambuffer.WithCapacity(900),
		streambuffer.WithFrameSize(1024*1024),
		streambuffer.WithInputBuffer(60), // buffer 2 seconds of frames
	)

	// context for coordinating graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// handle interrupt signal (ctrl+c)
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt)
	go func() {
		<-signalChan
		fmt.Println("\nShutting down...")
		cancel()
	}()

	// start buffer and ensure cleanup
	buffer.Start()
	defer buffer.Stop()

	// completion signal
	done := make(chan struct{})

	// simulate camera in background
	go simulateCamera(ctx, buffer.Input(), done)

	// simulate highlight button presses
	//
	var wg sync.WaitGroup
	wg.Add(1)
	go simulateHighlightCapture(ctx, buffer, &wg)

	select {
	case <-ctx.Done():
		fmt.Println("Context cancelled, waiting for cleanup...")
	case <-done:
		fmt.Println("Camera simulation completed")
	}

	// ensure highlight capture completes
	wg.Wait()

	// display performance summary
	metrics := buffer.GetMetrics()
	fmt.Println("\nFinal Metrics:")
	fmt.Printf("Frames Processed:   %d\n", metrics.FramesProcessed)
	fmt.Printf("Frames Trimmed:     %d\n", metrics.FramesTrimmed)
	fmt.Printf("Snapshots Captured: %d\n", metrics.SnapshotsSent)
	fmt.Printf("Buffer Utilization: %.1f%%\n", metrics.BufferUtilization*100)
	fmt.Printf("Uptime:             %.1f seconds\n", metrics.Uptime.Seconds())
}

// simulateCamera generates video frames at 30fps and sends them to the buffer.
func simulateCamera(ctx context.Context, input chan<- []byte, done chan<- struct{}) {
	defer close(done)

	var frameCount int
	targetFPS := 30
	frameDuration := time.Second / time.Duration(targetFPS)
	simulationDuration := 20 * time.Second // run for 20 seconds

	fmt.Printf("Starting camera simulation (%d FPS for %v)\n",
		targetFPS, simulationDuration)

	startTime := time.Now()
	ticker := time.NewTicker(frameDuration)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// simulate video frame capture
			frameSize := 100*1024 + rand.Intn(20*1024) // ~100-120KB
			frame := generateVideoFrame(frameCount, frameSize)

			// attempt to send frame, drop if buffer is full
			select {
			case input <- frame:
				frameCount++
				if frameCount%30 == 0 {
					fmt.Printf("Camera: sent %d frames (%.1f seconds of video)\n",
						frameCount, float64(frameCount)/float64(targetFPS))
				}
			case <-ctx.Done():
				return
			default:
				// simulate frame dropping under load
				fmt.Println("Warning: Buffer full, dropping frame")
			}

			if time.Since(startTime) > simulationDuration {
				fmt.Printf("Camera simulation completed after %d frames\n", frameCount)
				return
			}
		}
	}
}

// generateVideoFrame creates a simulated video frame with metadata.
func generateVideoFrame(frameNum int, size int) []byte {
	frame := make([]byte, size)

	// prepend header with frame number and timestamp
	header := fmt.Sprintf("Frame:%d,Time:%d,",
		frameNum, time.Now().UnixNano())

	// write header to frame buffer
	copy(frame, []byte(header))

	// fill remaining space with random data to simulate video content
	for i := len(header); i < size; i++ {
		frame[i] = byte(rand.Intn(256))
	}
	return frame
}

// simulateHighlightCapture triggers highlight capture at random intervals.
func simulateHighlightCapture(ctx context.Context, buffer *streambuffer.StreamBuffer, wg *sync.WaitGroup) {
	defer wg.Done()

	// allow buffer to accumulate some frames before first capture
	select {
	case <-time.After(5 * time.Second):
	case <-ctx.Done():
		return
	}

	// capture three highlights at pseudo-random times
	for i := range 3 {
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(2+rand.Intn(4)) * time.Second):
			fmt.Println("\n🏐 Highlight button pressed! Capturing last 30 seconds...")

			// limit snapshot request time to 500ms
			snapCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
			snapshot, err := buffer.GetSnapshot(snapCtx)
			cancel()

			if err != nil {
				fmt.Printf("❌ Error capturing highlight: %v\n", err)
				continue
			}

			// process the captured highlight
			processHighlight(i+1, *snapshot)
		}
	}
}

// processHighlight analyzes and saves a captured highlight.
func processHighlight(id int, snapshot streambuffer.Snapshot) {
	frameCount := len(snapshot.Frames)
	if frameCount == 0 {
		fmt.Println("❌ No frames in highlight!")
		return
	}

	// calculate total highlight duration
	duration := snapshot.EndTime.Sub(snapshot.StartTime)

	fmt.Printf("✅ Highlight #%d captured:\n", id)
	fmt.Printf("   - %d frames\n", frameCount)
	fmt.Printf("   - Duration: %.1f seconds\n", duration.Seconds())
	fmt.Printf("   - Time span: %s to %s\n",
		snapshot.StartTime.Format("15:04:05.000"),
		snapshot.EndTime.Format("15:04:05.000"))

	// examine boundary frames for demonstration
	firstFrame := snapshot.Frames[0]
	lastFrame := snapshot.Frames[len(snapshot.Frames)-1]

	// get readable header information
	firstHeader := extractHeaderText(firstFrame.Data)
	lastHeader := extractHeaderText(lastFrame.Data)

	fmt.Printf("   - First frame: Sequence=%d, Header=%s\n",
		firstFrame.Sequence, firstHeader)
	fmt.Printf("   - Last frame: Sequence=%d, Header=%s\n",
		lastFrame.Sequence, lastHeader)

	// save to file, upload to cloud, analyze ...
	fmt.Printf("   - Simulating saving highlight to disk...\n")
	time.Sleep(300 * time.Millisecond) // simulate processing time

	fmt.Printf("   - Highlight #%d saved successfully!\n", id)
}

// extractHeaderText extracts the readable portion of a frame header.
func extractHeaderText(data []byte) string {
	// search for the end of the header format (Frame:X,Time:Y,)
	var headerEnd int
	for i, b := range data {
		// find the second comma that terminates our header
		if b == ',' {
			// skip early commas and avoid overshooting a valid header
			if i > 5 && i < 50 {
				// look backwards to see if this might be our second comma
				for j := i - 1; j >= 0; j-- {
					if data[j] == ',' {
						// found complete header
						headerEnd = i + 1
						break
					}
				}
			}
			if headerEnd > 0 {
				break
			}
		}
		// limit search to reasonable length
		if i >= 100 {
			break
		}
	}

	// fallback to reasonable prefix if pattern not found
	if headerEnd == 0 {
		headerEnd = min(50, len(data))
	}

	return string(data[:headerEnd])
}
