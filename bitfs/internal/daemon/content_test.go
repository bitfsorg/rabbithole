package daemon

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestContainsPathTraversal_URLEncoded(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		expect bool
	}{
		{"literal dot-dot", "/foo/../bar", true},
		{"leading dot-dot", "/../etc/passwd", true},
		{"percent-encoded dot-dot lowercase", "/foo/%2e%2e/bar", true},
		{"percent-encoded dot-dot uppercase", "/foo/%2E%2E/bar", true},
		{"percent-encoded slash", "/foo%2F..%2Fbar", true},
		{"double percent-encoded", "/foo/%252e%252e/bar", true},
		{"clean path", "/foo/bar/baz", false},
		{"single dot", "/foo/./bar", false},
		{"dot in name", "/foo/bar..baz", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expect, containsPathTraversal(tt.path))
		})
	}
}
