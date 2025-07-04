package main

import (
	"bufio"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/alesr/tidstrom/videocapture"
)

func main() {
	fmt.Println("Tidström Video Demo")
	fmt.Println("===================")
	fmt.Println("This demo captures video from your webcam and buffers the last 5 seconds.")
	fmt.Println("")
	fmt.Println("Commands:")
	fmt.Println("  save [name]  - Save the current buffer as a snapshot")
	fmt.Println("  stats        - Show buffer statistics")
	fmt.Println("  info         - Show detailed buffer configuration")
	fmt.Println("  quit         - Exit the application")
	fmt.Println("")

	// Create video capture with default options (5 second buffer)
	opts := videocapture.DefaultOptions()
	opts.OutputDir = "out" // Save to ./out directory
	capture := videocapture.New(opts)

	// Start video capture
	if err := capture.Start(); err != nil {
		fmt.Printf("Error starting video capture: %v\n", err)
		os.Exit(1)
	}
	defer capture.Stop()

	// Print detailed buffer information
	printBufferInfo(capture)

	// Handle graceful shutdown on Ctrl+C
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("\nShutting down...")
		capture.Stop()
		os.Exit(0)
	}()

	// Create a mutex for console output
	var outputMutex sync.Mutex

	// Create a channel for command results
	resultChan := make(chan string, 5)

	// Handle command results
	go func() {
		for result := range resultChan {
			outputMutex.Lock()
			fmt.Println(result)
			fmt.Print("> ")
			outputMutex.Unlock()
		}
	}()

	// Process user commands in a non-blocking way
	go processCommands(capture, resultChan, &outputMutex)

	// Keep the main thread alive
	fmt.Println("Video capture running. Enter commands below:")
	fmt.Print("> ")

	for capture.IsRunning() {
		time.Sleep(1 * time.Second)
	}

	fmt.Println("Video capture stopped.")
}

// printBufferInfo prints detailed information about the buffer configuration
func printBufferInfo(capture *videocapture.Capture) {
	// Get buffer metrics
	metrics, err := capture.BufferMetrics()
	if err != nil {
		fmt.Printf("Error getting buffer metrics: %v\n", err)
		return
	}

	fmt.Println("\nBuffer Configuration Details:")
	fmt.Println("----------------------------")
	fmt.Printf("Window duration:    %.1f seconds\n", metrics.WindowDuration.Seconds())
	fmt.Printf("Buffer capacity:    %d frames\n", metrics.Capacity)

	// Estimate FPS based on processed frames and uptime
	estimatedFPS := 30.0 // Default assumption
	if metrics.Uptime.Seconds() > 0 && metrics.FramesProcessed > 0 {
		estimatedFPS = float64(metrics.FramesProcessed) / metrics.Uptime.Seconds()
	}

	// Calculate expected frames in window
	expectedFrames := int(estimatedFPS * metrics.WindowDuration.Seconds())
	fmt.Printf("Expected frames:    %d frames (at %.1f FPS)\n", expectedFrames, estimatedFPS)

	// Explain utilization
	expectedUtil := float64(expectedFrames) / float64(metrics.Capacity) * 100
	fmt.Printf("Expected util:      %.1f%%\n", expectedUtil)

	// Explain safety margin if any
	if metrics.Capacity > expectedFrames {
		margin := float64(metrics.Capacity-expectedFrames) / float64(expectedFrames) * 100
		fmt.Printf("Safety margin:      %.1f%%\n", margin)
		fmt.Printf("Target utilization: %.1f%% (100%% ÷ %.2f)\n",
			100/((margin/100)+1), (margin/100)+1)
	}

	fmt.Println("\nNote: If you consistently see ~83.9% utilization, this is expected")
	fmt.Println("behavior when using the default 20% safety margin in buffer sizing.")
	fmt.Println("The buffer is designed to maintain this utilization for optimal performance.")
	fmt.Println()
}

// processCommands reads user input and executes commands
func processCommands(capture *videocapture.Capture, resultChan chan<- string, outputMutex *sync.Mutex) {
	scanner := bufio.NewScanner(os.Stdin)

	for {
		if !capture.IsRunning() {
			return
		}

		if !scanner.Scan() {
			break
		}

		// Get the command
		cmd := scanner.Text()
		parts := strings.Fields(cmd)
		if len(parts) == 0 {
			outputMutex.Lock()
			fmt.Print("> ")
			outputMutex.Unlock()
			continue
		}

		switch strings.ToLower(parts[0]) {
		case "save":
			name := "snapshot"
			if len(parts) > 1 {
				name = parts[1]
			}

			resultChan <- fmt.Sprintf("Saving snapshot '%s'...", name)
			dirPath, videoPath, err := capture.SaveSnapshot(name)
			if err != nil {
				resultChan <- fmt.Sprintf("Error saving snapshot: %v", err)
			} else {
				if videoPath != "" {
					resultChan <- fmt.Sprintf("Snapshot saved to: %s\nVideo created: %s", dirPath, videoPath)
				} else {
					resultChan <- fmt.Sprintf("Snapshot saved to: %s", dirPath)
				}
			}

		case "stats":
			metrics, err := capture.BufferMetrics()
			if err != nil {
				resultChan <- fmt.Sprintf("Error getting metrics: %v", err)
				continue
			}

			// Build statistics message
			var stats strings.Builder
			stats.WriteString("Buffer Statistics:\n")
			stats.WriteString(fmt.Sprintf("  Frames processed: %d\n", metrics.FramesProcessed))
			stats.WriteString(fmt.Sprintf("  Current frames:   %d\n", metrics.FrameCount))
			stats.WriteString(fmt.Sprintf("  Buffer capacity:  %d\n", metrics.Capacity))
			stats.WriteString(fmt.Sprintf("  Utilization:      %.1f%% (%d/%d)\n",
				metrics.BufferUtilization*100, metrics.FrameCount, metrics.Capacity))
			stats.WriteString(fmt.Sprintf("  Frames trimmed:   %d\n", metrics.FramesTrimmed))
			stats.WriteString(fmt.Sprintf("  Window duration:  %.1f seconds\n", metrics.WindowDuration.Seconds()))
			stats.WriteString(fmt.Sprintf("  Uptime:           %.1f seconds\n", metrics.Uptime.Seconds()))

			// Calculate frame rate
			if metrics.Uptime.Seconds() > 0 {
				frameRate := float64(metrics.FramesProcessed) / metrics.Uptime.Seconds()
				stats.WriteString(fmt.Sprintf("  Average FPS:      %.1f\n", frameRate))
			}

			// Show timestamp info if we have frames
			if !metrics.LastFrameTime.IsZero() {
				now := time.Now()
				stats.WriteString(fmt.Sprintf("  Latest frame:     %s\n", metrics.LastFrameTime.Format("15:04:05.000")))
				stats.WriteString(fmt.Sprintf("  Frame age:        %.3f seconds\n", now.Sub(metrics.LastFrameTime).Seconds()))
				windowStart := metrics.LastFrameTime.Add(-metrics.WindowDuration)
				stats.WriteString(fmt.Sprintf("  Window start:     %s\n", windowStart.Format("15:04:05.000")))
			}

			resultChan <- stats.String()

		case "info":
			printBufferInfo(capture)
			resultChan <- "Buffer configuration details printed above."

		case "quit", "exit":
			resultChan <- "Exiting..."
			capture.Stop()
			os.Exit(0)

		default:
			resultChan <- fmt.Sprintf("Unknown command: %s\nAvailable commands: save [name], stats, info, quit", parts[0])
		}

		// A short delay helps prevent UI issues
		time.Sleep(50 * time.Millisecond)
	}
}
