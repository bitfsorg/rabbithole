package daemon

import (
	"sync"
	"time"
)

// LogEntry represents a single log entry in the ring buffer.
type LogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
}

// logBuffer is a thread-safe fixed-size ring buffer for log entries.
type logBuffer struct {
	mu      sync.Mutex
	entries []LogEntry
	cap     int
	pos     int
	full    bool
}

// newLogBuffer creates a new ring buffer with the given capacity.
func newLogBuffer(capacity int) *logBuffer {
	return &logBuffer{
		entries: make([]LogEntry, capacity),
		cap:     capacity,
	}
}

// Add writes a log entry at the current position and advances the cursor.
func (lb *logBuffer) Add(level, message string) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	lb.entries[lb.pos] = LogEntry{
		Timestamp: time.Now(),
		Level:     level,
		Message:   message,
	}
	lb.pos++
	if lb.pos >= lb.cap {
		lb.pos = 0
		lb.full = true
	}
}

// Entries returns log entries in chronological order.
// If level is non-empty, only entries matching that level are returned.
// If limit > 0, at most limit entries are returned (the most recent ones).
func (lb *logBuffer) Entries(limit int, level string) []LogEntry {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	// Collect all entries in chronological order.
	var all []LogEntry
	if lb.full {
		// Ring has wrapped: oldest entry is at pos, read pos..cap then 0..pos.
		for i := lb.pos; i < lb.cap; i++ {
			all = append(all, lb.entries[i])
		}
		for i := 0; i < lb.pos; i++ {
			all = append(all, lb.entries[i])
		}
	} else {
		// Ring hasn't wrapped: entries are 0..pos.
		all = append(all, lb.entries[:lb.pos]...)
	}

	// Filter by level if specified.
	if level != "" {
		filtered := make([]LogEntry, 0, len(all))
		for _, e := range all {
			if e.Level == level {
				filtered = append(filtered, e)
			}
		}
		all = filtered
	}

	// Apply limit (return the most recent entries).
	if limit > 0 && len(all) > limit {
		all = all[len(all)-limit:]
	}

	return all
}
