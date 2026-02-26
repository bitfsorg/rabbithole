package engine

import "fmt"

// UnpublishOpts holds options for the Unpublish operation.
type UnpublishOpts struct {
	Domain string
}

// Unpublish removes a domain binding from local state.
func (e *Engine) Unpublish(opts *UnpublishOpts) (*Result, error) {
	if opts.Domain == "" {
		return nil, fmt.Errorf("engine: domain is required")
	}

	removed := e.State.RemovePublishBinding(opts.Domain)
	if !removed {
		return nil, fmt.Errorf("engine: no publish binding for %q", opts.Domain)
	}

	msg := fmt.Sprintf("Removed publish binding for %s.\n\nRemember to also remove the DNS TXT record:\n  _bitfs.%s  TXT  (delete this record)",
		opts.Domain, opts.Domain)

	return &Result{
		Message: msg,
	}, nil
}
