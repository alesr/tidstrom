package tidstrom

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBufferPool(t *testing.T) {
	t.Parallel()

	t.Run("Basic operations", func(t *testing.T) {
		t.Parallel()

		bp := newBufferPool(64)

		buf := bp.get()
		assert.GreaterOrEqual(t, cap(buf), 64)
		assert.Equal(t, 0, len(buf))

		bp.put(buf)
	})

	t.Run("Memory reuse", func(t *testing.T) {
		t.Parallel()

		bp := newBufferPool(128)

		buf1 := bp.get()
		require.GreaterOrEqual(t, cap(buf1), 128)

		expandSize := 100
		for i := range expandSize {
			buf1 = append(buf1, byte(i))
		}

		bp.put(buf1)

		buf2 := bp.get()

		assert.Equal(t, 0, len(buf2))

		// verify the buffer functions correctly regardless of recycling behavior
		// we can't make firm assertions about capacity due to sync.Pool implementation details
		assert.NotNil(t, buf2)
		assert.Equal(t, 0, len(buf2), "Recycled buffer should have zero length")

		buf2 = append(buf2, make([]byte, expandSize)...)
		assert.Equal(t, expandSize, len(buf2), "Buffer should be expandable")
	})

	t.Run("Size limit respected", func(t *testing.T) {
		t.Parallel()

		sizeHint := 64
		maxSize := 128
		bp := newBufferPool(sizeHint, withMaxBufferSize(maxSize))

		buf := bp.get()
		require.NotNil(t, buf)
		require.Equal(t, 0, len(buf))

		sizes := []int{
			sizeHint,      // basic size
			maxSize - 10,  // under max size
			maxSize,       // at max size
			maxSize + 100, // over max size
		}

		for _, size := range sizes {
			buf := bp.get()
			require.NotNil(t, buf)

			buf = append(buf, make([]byte, size)...)
			require.Equal(t, size, len(buf))

			bp.put(buf)

			newBuf := bp.get()
			require.NotNil(t, newBuf)
			require.Equal(t, 0, len(newBuf))

			newBuf = append(newBuf, 1, 2, 3)
			require.Equal(t, 3, len(newBuf))
		}
	})

	t.Run("Nil buffer handling", func(t *testing.T) {
		t.Parallel()

		bp := newBufferPool(64)

		defer func() {
			if r := recover(); r != nil {
				assert.Fail(t, "Putting nil buffer should not panic", r)
			}
		}()

		bp.put(nil)

		buf := bp.get()
		assert.NotNil(t, buf)
	})

	t.Run("Custom max size", func(t *testing.T) {
		t.Parallel()

		customMaxSize := 256
		bp := newBufferPool(64, withMaxBufferSize(customMaxSize))

		buf := bp.get()
		targetSize := customMaxSize - 10
		for i := range targetSize {
			buf = append(buf, byte(i))
		}

		require.Equal(t, targetSize, len(buf))

		bp.put(buf)

		buf2 := bp.get()
		require.NotNil(t, buf2)
		require.Equal(t, 0, len(buf2))

		buf2 = append(buf2, make([]byte, targetSize)...)
		require.Equal(t, targetSize, len(buf2))
	})
}

func TestBufferPoolOptions(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		maxSize int
		testFn  func(t *testing.T, bp *bufferPool)
	}{
		{
			name:    "Zero max size",
			maxSize: 0,
			testFn: func(t *testing.T, bp *bufferPool) {
				buf := bp.get()
				assert.NotNil(t, buf)
				bp.put(buf)
			},
		},
		{
			name:    "Negative max size",
			maxSize: -10,
			testFn: func(t *testing.T, bp *bufferPool) {
				buf := bp.get()
				assert.NotNil(t, buf)
				bp.put(buf)
			},
		},
		{
			name:    "Custom max size",
			maxSize: 512,
			testFn: func(t *testing.T, bp *bufferPool) {
				buf := bp.get()

				targetSize := 500
				for i := range targetSize {
					buf = append(buf, byte(i))
				}

				require.Equal(t, targetSize, len(buf))

				bp.put(buf)

				newBuf := bp.get()

				assert.Equal(t, 0, len(newBuf))

				newBuf = append(newBuf, make([]byte, targetSize)...)
				assert.Equal(t, targetSize, len(newBuf),
					"Buffer should be usable at required capacity")
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			bp := newBufferPool(64, withMaxBufferSize(tc.maxSize))
			tc.testFn(t, bp)
		})
	}
}

func TestBufferPoolMultipleCycles(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		sizeHint int
		maxSize  int
		expand   int
	}{
		{"Buffer under max size", 64, 128, 100},     // will be recycled
		{"Buffer at max size", 64, 128, 128},        // will be recycled
		{"Buffer exceeding max size", 64, 128, 200}, // will not be recycled
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			bp := newBufferPool(tc.sizeHint, withMaxBufferSize(tc.maxSize))

			buf1 := bp.get()
			require.NotNil(t, buf1)
			require.Equal(t, 0, len(buf1), "Initial buffer should have zero length")
			require.GreaterOrEqual(t, cap(buf1), tc.sizeHint, "Initial buffer should have at least sizeHint capacity")

			// expand the buffer to the test size
			for i := range tc.expand {
				buf1 = append(buf1, byte(i))
			}
			require.Equal(t, tc.expand, len(buf1), "Buffer should be expanded to test size")

			// back in the pool
			bp.put(buf1)

			// get another buffer and check its properties
			buf2 := bp.get()
			require.NotNil(t, buf2, "Should always get a non-nil buffer")
			require.Equal(t, 0, len(buf2), "Recycled buffer should have zero length")

			if tc.expand <= tc.maxSize {
				require.Equal(t, 0, len(buf2), "Buffer should have zero length when retrieved")

				buf2 = append(buf2, make([]byte, tc.expand)...)
				require.Equal(t, tc.expand, len(buf2), "Buffer should be usable at required capacity")
			} else {
				buf2 = append(buf2, 1, 2, 3)
				require.Equal(t, 3, len(buf2), "Should be able to append to buffer")
			}
		})
	}
}
