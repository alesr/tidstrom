package main

import (
	"testing"
)

// TestExtractHeaderText verifies the header extraction logic handles various input formats.
func TestExtractHeaderText(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		expected string
	}{
		{
			name:     "valid header",
			data:     []byte("Frame:123,Time:1623456789,some binary data follows"),
			expected: "Frame:123,Time:1623456789,",
		},
		{
			name:     "incomplete header with one comma",
			data:     []byte("Frame:123,incomplete header"),
			expected: "Frame:123,incomplete header",
		},
		{
			name:     "no commas",
			data:     []byte("No commas in this data"),
			expected: "No commas in this data",
		},
		{
			name:     "empty data",
			data:     []byte{},
			expected: "",
		},
		{
			name:     "binary data after header",
			data:     append([]byte("Frame:456,Time:987654321,"), []byte{0x00, 0xFF, 0x7F, 0x80}...),
			expected: "Frame:456,Time:987654321,",
		},
		{
			name:     "very long header exceeding search limit",
			data:     createLongTestData(),
			expected: "Frame:999,Time:9999999999,", // should return just the header part
		},
		{
			name:     "binary data before header",
			data:     append([]byte{0xAA, 0xBB, 0xCC}, []byte("Frame:789,Time:555555555,")...),
			expected: string(append([]byte{0xAA, 0xBB, 0xCC}, []byte("Frame:789,Time:555555555,")...)),
		},
		{
			name:     "multiple commas in non-standard format",
			data:     []byte("This,has,multiple,commas,but,not,in,the,expected,format"),
			expected: "This,has,",
		},
		{
			name:     "header at exactly search limit",
			data:     createExactSearchLimitData(),
			expected: "Frame:111,Time:1111111111,",
		},
		{
			name:     "header with special characters",
			data:     []byte("Frame:42,Time:1234567890,!@#$%^&*()"),
			expected: "Frame:42,Time:1234567890,",
		},
		{
			name:     "exactly 50 bytes data for fallback",
			data:     createExact50ByteData(),
			expected: string(createExact50ByteData()),
		},
		{
			name:     "data with non-ASCII characters",
			data:     []byte("Frame:999,Time:9876543210,こんにちは世界"),
			expected: "Frame:999,Time:9876543210,",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractHeaderText(tt.data)
			if result != tt.expected {
				t.Errorf("extractHeaderText() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// createLongTestData creates test data exceeding the search limit.
func createLongTestData() []byte {
	// start with valid header
	data := []byte("Frame:999,Time:9999999999,")

	// pad to exceed 100-byte search limit
	for len(data) < 150 {
		data = append(data, 'x')
	}

	return data
}

// createExactSearchLimitData builds a buffer exactly at the search boundary.
func createExactSearchLimitData() []byte {
	data := []byte("Frame:111,Time:1111111111,")

	// ensure total size is exactly 100 bytes
	padding := make([]byte, 100-len(data))
	for i := range padding {
		padding[i] = 'y'
	}

	return append(data, padding...)
}

// createExact50ByteData makes a 50-byte slice for testing the prefix fallback.
func createExact50ByteData() []byte {
	data := []byte("Exactly50BytesOfDataForTestingTheFallbackBehavior.")

	// adjust to exactly 50 bytes
	if len(data) < 50 {
		data = append(data, make([]byte, 50-len(data))...)
	} else if len(data) > 50 {
		data = data[:50]
	}

	return data
}
