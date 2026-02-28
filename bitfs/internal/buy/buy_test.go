package buy

import (
	"encoding/hex"
	"testing"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuyParams_Validate(t *testing.T) {
	tests := []struct {
		name    string
		params  BuyParams
		wantErr bool
	}{
		{"nil config", BuyParams{TxID: "abc"}, true},
		{"nil privkey in config", BuyParams{TxID: "abc", Config: &BuyerConfig{}}, true},
		{"empty txid", BuyParams{Config: &BuyerConfig{PrivKey: testPrivKey(t)}}, true},
		{"valid minimal", BuyParams{TxID: "abc", Config: &BuyerConfig{PrivKey: testPrivKey(t)}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.params.validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestBuyResult_Fields(t *testing.T) {
	// Verify BuyResult struct has the expected fields.
	r := &BuyResult{
		Capsule:      []byte{0x01, 0x02},
		HTLCTxID:     "deadbeef",
		CostSatoshis: 1000,
	}
	assert.Equal(t, []byte{0x01, 0x02}, r.Capsule)
	assert.Equal(t, "deadbeef", r.HTLCTxID)
	assert.Equal(t, uint64(1000), r.CostSatoshis)
}

func TestDefaultFeeRate(t *testing.T) {
	assert.Equal(t, uint64(1), defaultFeeRate)
}

func testPrivKey(t *testing.T) *ec.PrivateKey {
	t.Helper()
	keyBytes, err := hex.DecodeString("0000000000000000000000000000000000000000000000000000000000000001")
	require.NoError(t, err)
	pk, _ := ec.PrivateKeyFromBytes(keyBytes)
	require.NotNil(t, pk)
	return pk
}
