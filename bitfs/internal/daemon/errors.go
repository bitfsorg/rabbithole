package daemon

import "errors"

var (
	// ErrNilConfig indicates a nil configuration was provided.
	ErrNilConfig = errors.New("daemon: nil config")

	// ErrNilWallet indicates a nil wallet was provided.
	ErrNilWallet = errors.New("daemon: nil wallet")

	// ErrNilStore indicates a nil store was provided.
	ErrNilStore = errors.New("daemon: nil store")

	// ErrAlreadyRunning indicates the daemon is already started.
	ErrAlreadyRunning = errors.New("daemon: already running")

	// ErrNotRunning indicates the daemon is not started.
	ErrNotRunning = errors.New("daemon: not running")

	// ErrHandshakeFailed indicates the Method 42 handshake failed.
	ErrHandshakeFailed = errors.New("daemon: handshake failed")

	// ErrSessionExpired indicates the session has expired.
	ErrSessionExpired = errors.New("daemon: session expired")

	// ErrSessionNotFound indicates the session was not found.
	ErrSessionNotFound = errors.New("daemon: session not found")

	// ErrRateLimited indicates the request was rate limited.
	ErrRateLimited = errors.New("daemon: rate limited")

	// ErrContentNotFound indicates the requested content was not found.
	ErrContentNotFound = errors.New("daemon: content not found")

	// ErrInvalidRequest indicates the request is malformed.
	ErrInvalidRequest = errors.New("daemon: invalid request")

	// ErrInternalError indicates an internal server error.
	ErrInternalError = errors.New("daemon: internal error")
)
