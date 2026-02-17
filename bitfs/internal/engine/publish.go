package engine

import (
	"encoding/hex"
	"fmt"
)

// PublishOpts holds options for the Publish operation.
type PublishOpts struct {
	VaultIndex uint32
	Domain     string
}

// Publish generates DNS TXT record instructions for binding a domain to a vault.
// No transaction is needed — this is purely informational.
func (e *Engine) Publish(opts *PublishOpts) (*Result, error) {
	rootKP, err := e.Wallet.DeriveVaultRootKey(opts.VaultIndex)
	if err != nil {
		return nil, fmt.Errorf("engine: derive vault root key: %w", err)
	}

	rootPubHex := hex.EncodeToString(rootKP.PublicKey.Compressed())

	msg := fmt.Sprintf(`To publish vault to %s, add the following DNS TXT record:

  _bitfs.%s  TXT  "bitfs=%s"

Then visitors can access your content at:
  bitfs://%s/
  https://%s/ (via daemon)`, opts.Domain, opts.Domain, rootPubHex, opts.Domain, opts.Domain)

	return &Result{
		Message: msg,
		NodePub: rootPubHex,
	}, nil
}
