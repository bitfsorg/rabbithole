package daemon

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogBuffer_Write(t *testing.T) {
	lb := newLogBuffer(10)

	lb.Add("info", "first")
	lb.Add("warn", "second")
	lb.Add("error", "third")

	entries := lb.Entries(0, "")
	require.Len(t, entries, 3)
	assert.Equal(t, "first", entries[0].Message)
	assert.Equal(t, "info", entries[0].Level)
	assert.Equal(t, "second", entries[1].Message)
	assert.Equal(t, "warn", entries[1].Level)
	assert.Equal(t, "third", entries[2].Message)
	assert.Equal(t, "error", entries[2].Level)
}

func TestLogBuffer_Overflow(t *testing.T) {
	lb := newLogBuffer(2)

	lb.Add("info", "first")
	lb.Add("info", "second")
	lb.Add("info", "third") // overwrites "first"

	entries := lb.Entries(0, "")
	require.Len(t, entries, 2)
	assert.Equal(t, "second", entries[0].Message)
	assert.Equal(t, "third", entries[1].Message)
}

func TestLogBuffer_FilterByLevel(t *testing.T) {
	lb := newLogBuffer(10)

	lb.Add("info", "info message")
	lb.Add("warn", "warn message")
	lb.Add("error", "error message")
	lb.Add("warn", "another warn")

	entries := lb.Entries(0, "warn")
	require.Len(t, entries, 2)
	assert.Equal(t, "warn message", entries[0].Message)
	assert.Equal(t, "another warn", entries[1].Message)
}

func TestLogBuffer_Limit(t *testing.T) {
	lb := newLogBuffer(20)

	for i := 0; i < 10; i++ {
		lb.Add("info", "message")
	}

	entries := lb.Entries(3, "")
	require.Len(t, entries, 3)
}

func TestLogBuffer_Empty(t *testing.T) {
	lb := newLogBuffer(10)

	entries := lb.Entries(0, "")
	assert.Empty(t, entries)

	entries = lb.Entries(5, "info")
	assert.Empty(t, entries)
}
