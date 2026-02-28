package buy

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/bitfsorg/bitfs/internal/client"
)

func TestExitCodeFromError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"NotFound", client.ErrNotFound, 2},
		{"Timeout", client.ErrTimeout, 4},
		{"Network", client.ErrNetwork, 4},
		{"Server", client.ErrServer, 4},
		{"PaymentRequired", client.ErrPaymentRequired, 5},
		{"GenericError", errors.New("something broke"), 1},
		{"WrappedNotFound", fmt.Errorf("context: %w", client.ErrNotFound), 2},
		{"WrappedTimeout", fmt.Errorf("context: %w", client.ErrTimeout), 4},
		{"WrappedNetwork", fmt.Errorf("context: %w", client.ErrNetwork), 4},
		{"WrappedServer", fmt.Errorf("context: %w", client.ErrServer), 4},
		{"WrappedPayment", fmt.Errorf("context: %w", client.ErrPaymentRequired), 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExitCodeFromError(tt.err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestErrorMessage(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"NotFound", client.ErrNotFound, "not found"},
		{"Timeout", client.ErrTimeout, "request timeout"},
		{"Network", client.ErrNetwork, "network error"},
		{"Server", client.ErrServer, "server error"},
		{"PaymentRequired", client.ErrPaymentRequired, client.ErrPaymentRequired.Error()},
		{"GenericError", errors.New("disk full"), "disk full"},
		{"WrappedNotFound", fmt.Errorf("wrap: %w", client.ErrNotFound), "not found"},
		{"WrappedTimeout", fmt.Errorf("wrap: %w", client.ErrTimeout), "request timeout"},
		{"WrappedNetwork", fmt.Errorf("wrap: %w", client.ErrNetwork), "network error"},
		{"WrappedServer", fmt.Errorf("wrap: %w", client.ErrServer), "server error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ErrorMessage(tt.err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestHandleError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		toolName string
		wantCode int
		wantOut  string
	}{
		{
			name:     "NotFound_ShortForm",
			err:      client.ErrNotFound,
			toolName: "bcat",
			wantCode: 2,
			wantOut:  "bcat: not found\n",
		},
		{
			name:     "Timeout_ShortForm",
			err:      client.ErrTimeout,
			toolName: "bget",
			wantCode: 4,
			wantOut:  "bget: request timeout\n",
		},
		{
			name:     "Network_LongForm",
			err:      fmt.Errorf("%w: connection refused", client.ErrNetwork),
			toolName: "bls",
			wantCode: 4,
			wantOut:  "bls: network error: client: network error: connection refused\n",
		},
		{
			name:     "Server_LongForm",
			err:      fmt.Errorf("%w: HTTP 503", client.ErrServer),
			toolName: "btree",
			wantCode: 4,
			wantOut:  "btree: server error: client: server error: HTTP 503\n",
		},
		{
			name:     "PaymentRequired_ShortForm",
			err:      client.ErrPaymentRequired,
			toolName: "bget",
			wantCode: 5,
			wantOut:  "bget: client: payment required\n",
		},
		{
			name:     "GenericError_ShortForm",
			err:      errors.New("unknown failure"),
			toolName: "bstat",
			wantCode: 1,
			wantOut:  "bstat: unknown failure\n",
		},
		{
			name:     "WrappedTimeout_ShortForm",
			err:      fmt.Errorf("wrap: %w", client.ErrTimeout),
			toolName: "bcat",
			wantCode: 4,
			wantOut:  "bcat: request timeout\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			code := HandleError(tt.err, tt.toolName, &buf)
			assert.Equal(t, tt.wantCode, code)
			assert.Equal(t, tt.wantOut, buf.String())
		})
	}
}
