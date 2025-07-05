# Tidström Video Demo

A demonstration application that captures video from your webcam and uses the Tidström time-based buffer to save the last 5 seconds of footage on demand.

## Overview

This demo shows how to use the Tidström time-based buffer with real-time video streaming. It:

- Captures video from your webcam
- Buffers the last 5 seconds in memory
- Lets you save snapshots of the buffer on demand
- Shows the power of Tidström for time-series data

## Requirements

- [Go](https://golang.org/dl/) 1.21 or newer
- [GoCV](https://gocv.io) for video capture
- A working webcam
- OpenCV (installed automatically with GoCV)

## Installation

1. Make sure you have Go installed
2. Install GoCV by following the instructions at https://gocv.io/getting-started/
3. Clone this repository
4. Build the demo:

```sh
cd tidstrom
go build -o videodemo ./cmd/videodemo
```

## Running the Demo

Start the demo by running:

```sh
./videodemo
```

The application will:
1. Initialize your webcam at 640x480 resolution, 30 FPS
2. Start capturing video frames
3. Store them in a 5-second rolling buffer
4. Wait for your commands

## Commands

The demo accepts the following commands:

- `save [name]` - Save the current buffer as a snapshot with an optional name
- `stats` - Show buffer statistics (frames processed, buffer utilization, etc.)
- `quit` - Exit the application (or press Ctrl+C)

## Snapshots

When you save a snapshot:

1. All frames in the buffer (up to 5 seconds) are saved to disk
2. Files are stored in the `out/[name]_[timestamp]` directory
3. Each frame is saved as a JPEG image (`frame_0001.jpg`, etc.)
4. An `info.txt` file contains metadata about the snapshot

You can use these frames to:
- Create a video using FFmpeg or other tools
- Analyze the captured footage
- See exactly what happened in the last 5 seconds

## Creating a Video from Snapshots

To create a video from the saved frames using FFmpeg:

```sh
cd out/your_snapshot_directory
ffmpeg -framerate 30 -i frame_%04d.jpg -c:v libx264 -pix_fmt yuv420p output.mp4
```

## Troubleshooting

- **Camera not found**: Make sure your webcam is connected and not in use by another application
- **Black frames**: Some webcams take a moment to adjust exposure; wait a few seconds
- **Low frame rate**: Try reducing the resolution in the code (320x240 instead of 640x480)
- **Memory issues**: Reduce the buffer window duration or lower the frame rate

## License

MIT - See the LICENSE file in the project root for details.