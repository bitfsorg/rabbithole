package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnpublish_Success(t *testing.T) {
	eng := initTestEngine(t)

	// Add a binding first.
	eng.State.SetPublishBinding(&PublishBinding{
		Domain:     "example.com",
		VaultIndex: 0,
		PubKeyHex:  "aabbcc",
		Verified:   true,
	})

	result, err := eng.Unpublish(&UnpublishOpts{Domain: "example.com"})
	require.NoError(t, err)
	assert.Contains(t, result.Message, "Removed")
	assert.Contains(t, result.Message, "example.com")

	// Verify binding is gone.
	assert.Nil(t, eng.State.GetPublishBinding("example.com"))
}

func TestUnpublish_NotFound(t *testing.T) {
	eng := initTestEngine(t)

	_, err := eng.Unpublish(&UnpublishOpts{Domain: "nope.com"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no publish binding")
}

func TestUnpublish_EmptyDomain(t *testing.T) {
	eng := initTestEngine(t)

	_, err := eng.Unpublish(&UnpublishOpts{Domain: ""})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "domain is required")
}
