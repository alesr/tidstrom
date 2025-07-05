// Package videocapture provides high-level video capture functionality using
// GoCV that integrates with the streambuffer time-based buffer.
package videocapture

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/alesr/tidstrom/streambuffer"
	"gocv.io/x/gocv"
)

// CaptureOptions configures the video capture behavior.
type CaptureOptions struct {
	DeviceID     int
	Width        int
	Height       int
	FPS          int
	BufferWindow time.Duration
	JPEGQuality  int
	OutputDir    string
	CreateVideo  bool
}

// DefaultOptions returns a reasonable set of defaults for video capture.
func DefaultOptions() CaptureOptions {
	return CaptureOptions{
		DeviceID:     0,
		Width:        640,
		Height:       480,
		FPS:          30,
		BufferWindow: 5 * time.Second,
		JPEGQuality:  100,
		OutputDir:    "snapshots",
		CreateVideo:  true,
	}
}

// Capture provides a high-level interface for video capture with a stream buffer.
type Capture struct {
	opts       CaptureOptions
	buffer     *streambuffer.StreamBuffer
	webcam     *gocv.VideoCapture
	frameCount int

	// stats logging
	statLogInterval int // how often to log stats (in frames)

	// state management
	ctx        context.Context
	cancelFunc context.CancelFunc
	wg         sync.WaitGroup
	running    bool
	mu         sync.Mutex
}

// New creates a new video capture instance with the given options.
func New(opts CaptureOptions) *Capture {
	if opts.OutputDir != "" {
		os.MkdirAll(opts.OutputDir, 0755)
	}

	// reasonable defaults for any unset values
	if opts.Width <= 0 {
		opts.Width = 640
	}
	if opts.Height <= 0 {
		opts.Height = 480
	}
	if opts.FPS <= 0 {
		opts.FPS = 30
	}
	if opts.BufferWindow <= 0 {
		opts.BufferWindow = 5 * time.Second
	}
	if opts.JPEGQuality <= 0 || opts.JPEGQuality > 100 {
		opts.JPEGQuality = 90
	}
	return &Capture{
		opts:            opts,
		statLogInterval: opts.FPS * 10, // every 10 seconds
	}
}

// Start initializes the camera and begins capturing frames in a background goroutine.
func (c *Capture) Start() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.running {
		return errors.New("capture already running")
	}

	webcam, err := gocv.OpenVideoCapture(c.opts.DeviceID)
	if err != nil {
		return fmt.Errorf("failed to open video capture device: %w", err)
	}

	webcam.Set(gocv.VideoCaptureFrameWidth, float64(c.opts.Width))
	webcam.Set(gocv.VideoCaptureFrameHeight, float64(c.opts.Height))
	webcam.Set(gocv.VideoCaptureFPS, float64(c.opts.FPS))

	actualWidth := webcam.Get(gocv.VideoCaptureFrameWidth)
	actualHeight := webcam.Get(gocv.VideoCaptureFrameHeight)
	actualFPS := webcam.Get(gocv.VideoCaptureFPS)

	fmt.Printf("Camera initialized: %.0fx%.0f @ %.0f FPS\n",
		actualWidth, actualHeight, actualFPS)

	//calculate buffer size based on FPS and window duration
	bufferSize := int(float64(c.opts.FPS) * c.opts.BufferWindow.Seconds() * 2) // double the size for safety
	buffer := streambuffer.NewStreamBuffer(
		streambuffer.WithWindow(c.opts.BufferWindow),
		streambuffer.WithCapacity(bufferSize),
		streambuffer.WithFrameSize(c.opts.Width*c.opts.Height/5), // rough JPEG size estimate
		streambuffer.WithInputBuffer(c.opts.FPS),                 // buffer 1 second of frames
	)

	buffer.Start()

	c.buffer = buffer
	c.webcam = webcam
	c.ctx, c.cancelFunc = context.WithCancel(context.Background())
	c.running = true
	c.frameCount = 0

	c.wg.Add(1)
	go c.captureLoop()

	return nil
}

// Stop halts video capture and releases resources.
func (c *Capture) Stop() {
	c.mu.Lock()
	running := c.running
	c.mu.Unlock()

	if !running {
		return
	}

	c.cancelFunc()

	c.wg.Wait()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.buffer != nil {
		c.buffer.Stop()
		c.buffer = nil
	}

	if c.webcam != nil {
		c.webcam.Close()
		c.webcam = nil
	}
	c.running = false
}

// SaveSnapshot captures the current buffer contents and saves them to disk.
// It returns the path to the saved snapshot directory and the video file path if created.
func (c *Capture) SaveSnapshot(name string) (string, string, error) {
	c.mu.Lock()
	if !c.running || c.buffer == nil {
		c.mu.Unlock()
		return "", "", errors.New("capture not running")
	}
	buffer := c.buffer
	opts := c.opts
	c.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	snapshot, err := buffer.GetSnapshot(ctx)
	if err != nil {
		return "", "", fmt.Errorf("failed to get snapshot: %w", err)
	}

	// create snapshot directory
	timestamp := time.Now().Format("20060102_150405")
	if name == "" {
		name = "snapshot"
	}

	snapshotDir := filepath.Join(c.opts.OutputDir, fmt.Sprintf("%s_%s", name, timestamp))
	if err := os.MkdirAll(snapshotDir, 0755); err != nil {
		return "", "", fmt.Errorf("failed to create output directory: %w", err)
	}

	// save frames as individual JPEGs
	var frameCount int
	for i, frame := range snapshot.Frames {
		if len(frame.Data) == 0 {
			continue
		}

		framePath := filepath.Join(snapshotDir, fmt.Sprintf("frame_%04d.jpg", i))
		if err := os.WriteFile(framePath, frame.Data, 0644); err != nil {
			return snapshotDir, "", fmt.Errorf("error saving frame %d: %w", i, err)
		}
		frameCount++
	}

	// create video if option is enabled and we have frames
	var videoPath string
	if opts.CreateVideo && frameCount > 0 {
		videoPath, err = createVideo(snapshotDir, name, opts.FPS)
		if err != nil {
			fmt.Printf("Warning: Failed to create video: %v\n", err)
			// continue even if video creation fails
		}
	}

	// save info file (after video creation so we can include video info)
	infoPath := filepath.Join(snapshotDir, "info.txt")
	infoFile, err := os.Create(infoPath)
	if err != nil {
		return snapshotDir, videoPath, fmt.Errorf("failed to create info file: %w", err)
	}
	defer infoFile.Close()

	duration := snapshot.EndTime.Sub(snapshot.StartTime)
	fmt.Fprintf(infoFile, "Snapshot: %s\n", name)
	fmt.Fprintf(infoFile, "Captured: %s\n", timestamp)
	fmt.Fprintf(infoFile, "Frames: %d\n", frameCount)
	fmt.Fprintf(infoFile, "Duration: %.2f seconds\n", duration.Seconds())
	fmt.Fprintf(infoFile, "Time range: %s to %s\n",
		snapshot.StartTime.Format(time.RFC3339Nano),
		snapshot.EndTime.Format(time.RFC3339Nano))

	if videoPath != "" {
		fmt.Fprintf(infoFile, "Video: %s\n", filepath.Base(videoPath))
		fmt.Fprintf(infoFile, "Video FPS: %d\n", opts.FPS)
	}

	fmt.Printf("Saved %d frames to %s\n", frameCount, snapshotDir)
	if videoPath != "" {
		fmt.Printf("Created video: %s\n", videoPath)
	}
	return snapshotDir, videoPath, nil
}

// BufferMetrics returns the current metrics from the underlying buffer.
func (c *Capture) BufferMetrics() (streambuffer.Metrics, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running || c.buffer == nil {
		return streambuffer.Metrics{}, errors.New("capture not running")
	}
	return c.buffer.GetMetrics(), nil
}

// IsRunning returns whether the capture is currently active.
func (c *Capture) IsRunning() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running
}

// captureLoop runs in a background goroutine to continuously capture frames.
func (c *Capture) captureLoop() {
	defer c.wg.Done()

	// create reusable matrix for frame capture
	img := gocv.NewMat()
	defer img.Close()

	input := c.buffer.Input()
	ticker := time.NewTicker(time.Second / time.Duration(c.opts.FPS))
	defer ticker.Stop()

	fmt.Println("Starting video capture...")
	fmt.Println("Type commands at the prompt below.")
	fmt.Print("> ")

	for {
		select {
		case <-c.ctx.Done():
			fmt.Println("Stopping video capture...")
			return

		case <-ticker.C:
			// frame from webcam
			c.mu.Lock()
			webcam := c.webcam
			c.mu.Unlock()

			if webcam == nil {
				continue
			}

			if ok := webcam.Read(&img); !ok || img.Empty() {
				fmt.Println("Warning: Failed to read frame")
				continue
			}

			frameData, err := matToJPEG(img, c.opts.JPEGQuality)
			if err != nil {
				fmt.Printf("Error encoding frame: %v\n", err)
				continue
			}

			// send to buffer
			select {
			case input <- frameData:
				c.mu.Lock()
				c.frameCount++
				frameCount := c.frameCount
				c.mu.Unlock()

				// only print stats every 10 seconds
				if frameCount%(c.opts.FPS*10) == 0 {
					fmt.Printf("Captured %d frames (%.1f seconds)\n",
						frameCount, float64(frameCount)/float64(c.opts.FPS))
					// Reprint the prompt so it's easier to enter commands
					fmt.Print("> ")
				}
			default:
				fmt.Println("Warning: Buffer full, dropping frame")
			}
		}
	}
}

// matToJPEG converts a GoCV Mat to JPEG bytes.
func matToJPEG(mat gocv.Mat, quality int) ([]byte, error) {
	img, err := mat.ToImage()
	if err != nil {
		return nil, fmt.Errorf("error converting Mat to Image: %w", err)
	}

	buf := new(bytes.Buffer)
	if err := jpeg.Encode(buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, fmt.Errorf("error encoding JPEG: %w", err)
	}
	return buf.Bytes(), nil
}

// createVideo generates a video file from frames in the snapshot directory.
// It returns the path to the created video file.
func createVideo(snapshotDir, name string, fps int) (string, error) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return "", fmt.Errorf("ffmpeg not found: %w", err)
	}

	videoPath := filepath.Join(snapshotDir, name+".mp4")

	cmd := exec.Command(
		"ffmpeg",
		"-y",                   // overwrite output file if it exists
		"-hide_banner",         // hide FFmpeg banner
		"-loglevel", "warning", // only show warnings and errors
		"-framerate", fmt.Sprintf("%d", fps), // set input frame rate
		"-i", filepath.Join(snapshotDir, "frame_%04d.jpg"), // input pattern
		"-c:v", "libx264", // use H.264 codec
		"-profile:v", "high", // high profile for better quality
		"-crf", "18", // quality level (lower is better)
		"-preset", "medium", // encoding speed/compression trade-off
		"-pix_fmt", "yuv420p", // pixel format for compatibility
		"-vf", "pad=ceil(iw/2)*2:ceil(ih/2)*2", // ensure even dimensions
		videoPath, // output file
	)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("ffmpeg error: %w - %s", err, stderr.String())
	}

	if _, err := os.Stat(videoPath); err != nil {
		return "", fmt.Errorf("video file not created: %w", err)
	}
	return videoPath, nil
}
