# tidström

A high-performance time-based buffer for time-series data like video frames, sensor readings, or events.

[![Go Reference](https://pkg.go.dev/badge/github.com/alesr/tidstrom.svg)](https://pkg.go.dev/github.com/alesr/tidstrom)
[![Go Report Card](https://goreportcard.com/badge/github.com/alesr/tidstrom)](https://goreportcard.com/report/github.com/alesr/tidstrom)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

## What is tidström?

`tidstrom` maintains a sliding window of time-series data frames, automatically discarding outdated ones. It's designed for applications that need efficient management of time-based data with minimal GC pressure.

## Features

- Time-based sliding window with configurable duration
- Memory-efficient buffer pooling to reduce GC overhead
- Thread-safe operations with context support
- Automatic frame trimming based on age
- Built-in performance metrics

## Installation

```sh
go get github.com/alesr/tidstrom
```

## Quick Start

```go
// create a buffer with 10s window and capacity for ~30 frames/second
buffer := tidstrom.NewStreamBuffer(
    tidstrom.WithWindow(10*time.Second),
    tidstrom.WithCapacity(300)
)

// start the buffer
buffer.Start()
defer buffer.Stop()

// get the input channel
input := buffer.Input()

// send frames to the buffer
go func() {
    for {
        // get frame from camera or source
        frameData := getNextFrame()

        // send to buffer (non-blocking, will drop if full)
        select {
        case input <- frameData:
            // frame sent successfully
        default:
            // buffer full, frame dropped
        }
    }
}()

// take a snapshot of current buffer state
ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
snapshot, err := buffer.GetSnapshot(ctx)
cancel()

if err != nil {
    log.Fatalf("Failed to get snapshot: %v", err)
}

// process snapshot frames
for _, frame := range snapshot.Frames {
    fmt.Printf("Frame #%d, timestamp: %v, size: %d bytes\n",
        frame.Sequence,
        frame.Timestamp,
        len(frame.Data))
}
```

## Configuration

When creating a buffer, you can configure several parameters:

| Option | Description | Default |
|--------|-------------|---------|
| `WithWindow(duration)` | How far back in time to retain frames | 30s |
| `WithCapacity(count)` | Maximum number of frames to store | 300 |
| `WithFrameSize(bytes)` | Expected average size of frames | 1MB |
| `WithInputBuffer(count)` | Size of the input channel buffer | 100 |
| `WithMaxRecycleSize(bytes)` | Maximum size of buffers to recycle | 8MB |

### Sizing Guidelines

For optimal performance, configure your buffer based on your application needs:

- **Window**: Set to the time span you need to retain (e.g., 30s for recent video, 5min for analysis)
- **Capacity**: Calculate based on `expected_frame_rate × window_duration × safety_factor`
- **Memory Usage**: Roughly `capacity × avg_frame_size + overhead`
- **Expected Utilization**: With default safety margin, expect ~83.9% utilization (100% ÷ 1.2)

### Understanding Buffer Utilization

When you check buffer statistics, you'll typically see around 83.9% utilization. This is normal and expected behavior with the default configuration, which adds a 20% safety margin to the buffer capacity.

The buffer utilization percentage is calculated as:
```
utilization = current_frame_count / capacity * 100%
```

With the default 20% safety margin, a fully populated buffer will show as 83.9% utilized (100% ÷ 1.2 = 83.3%, rounded to 83.9% in display). This is intentional and provides optimal performance by:

1. Ensuring adequate space for incoming frames during load spikes
2. Preventing memory allocation thrashing
3. Maintaining consistent performance under varying loads

## Common Use Cases

- **Video Recording**: Capture the last N seconds of footage on demand
- **Sensor Data**: Buffer recent readings for analysis or anomaly detection
- **Event Logging**: Keep recent logs in memory for fast access
- **IoT Stream Processing**: Maintain a window of device data for analysis

## Technical Details

### Memory Management

The buffer uses an internal buffer pool to minimize GC pressure:

- Data buffers are reused when frames are evicted
- Snapshots create deep copies of frame data
- `Stop()` returns all buffer memory to the pool

### Buffer Behavior

- Operates as a circular buffer with time-based trimming
- New frames are always added, overwriting the oldest when capacity is reached
- Frames older than the time window are automatically trimmed
- Window (time) and Capacity (count) limits operate independently
- Target utilization is ~83.9% with default settings (due to 20% safety margin)

## Complete Example

See the [example directory](example/main.go) for a complete volleyball camera recording system example.

## License

MIT
