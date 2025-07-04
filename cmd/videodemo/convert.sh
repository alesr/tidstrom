#!/bin/bash
# convert.sh - Convert Tidström snapshot frames to a video file
# Usage: ./convert.sh [snapshot_directory] [output_file.mp4] [framerate]

# Default values
DEFAULT_FPS=30
DEFAULT_OUTPUT="output.mp4"

# Display help
show_help() {
  echo "Tidström Snapshot Converter"
  echo "============================="
  echo "Converts snapshot frames to a video file using FFmpeg."
  echo
  echo "Usage: $0 [snapshot_directory] [output_file.mp4] [framerate]"
  echo
  echo "Arguments:"
  echo "  snapshot_directory  Directory containing frame_*.jpg files (required)"
  echo "  output_file.mp4     Output video filename (default: $DEFAULT_OUTPUT)"
  echo "  framerate           Frames per second (default: $DEFAULT_FPS)"
  echo
  echo "Example:"
  echo "  $0 out/snapshot_20230615_123045 my_video.mp4 30"
  echo
}

# Check if FFmpeg is installed
check_ffmpeg() {
  if ! command -v ffmpeg &> /dev/null; then
    echo "Error: FFmpeg is not installed or not in PATH"
    echo "Please install FFmpeg and try again."
    exit 1
  fi
}

# Check if directory exists and contains frames
check_directory() {
  local dir=$1

  if [[ ! -d "$dir" ]]; then
    echo "Error: Directory '$dir' not found"
    echo "Please provide a valid snapshot directory"
    exit 1
  fi

  # Count frames
  local frame_count=$(ls "$dir"/frame_*.jpg 2>/dev/null | wc -l)
  if [[ $frame_count -eq 0 ]]; then
    echo "Error: No frames found in '$dir'"
    echo "The directory should contain files named frame_0001.jpg, frame_0002.jpg, etc."
    exit 1
  fi

  echo "Found $frame_count frames in '$dir'"
  return $frame_count
}

# Convert frames to video
convert_to_video() {
  local input_dir=$1
  local output_file=$2
  local framerate=$3

  echo "Converting frames to video..."
  echo "- Input: $input_dir"
  echo "- Output: $output_file"
  echo "- Framerate: $framerate FPS"

  ffmpeg -y -hide_banner -loglevel warning \
    -framerate "$framerate" \
    -i "$input_dir/frame_%04d.jpg" \
    -c:v libx264 \
    -profile:v high \
    -crf 18 \
    -pix_fmt yuv420p \
    -vf "pad=ceil(iw/2)*2:ceil(ih/2)*2" \
    "$output_file"

  local result=$?
  if [[ $result -ne 0 ]]; then
    echo "Error: Failed to convert frames to video"
    exit 1
  fi

  echo "Conversion complete!"
  echo "Video saved to: $output_file"

  # Show video info
  ffmpeg -i "$output_file" -hide_banner 2>&1 | grep -E 'Duration|Stream'
}

# Main function
main() {
  # Show help if requested
  if [[ "$1" == "--help" || "$1" == "-h" ]]; then
    show_help
    exit 0
  fi

  # Check arguments
  if [[ $# -lt 1 ]]; then
    echo "Error: Missing snapshot directory"
    echo "Run '$0 --help' for usage information"
    exit 1
  fi

  # Parse arguments
  input_dir="$1"
  output_file="${2:-$DEFAULT_OUTPUT}"
  framerate="${3:-$DEFAULT_FPS}"

  # Check requirements
  check_ffmpeg
  check_directory "$input_dir"

  # Make sure output has .mp4 extension
  if [[ "$output_file" != *.mp4 ]]; then
    output_file="$output_file.mp4"
  fi

  # Convert frames to video
  convert_to_video "$input_dir" "$output_file" "$framerate"
}

# Execute main function
main "$@"
