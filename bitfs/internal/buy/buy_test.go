package buy

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"
	"github.com/bsv-blockchain/go-sdk/script"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/bitfsorg/bitfs/internal/client"
	"github.com/bitfsorg/libbitfs-go/network"
	"github.com/bitfsorg/libbitfs-go/x402"
)

// testAddr returns a base58 P2PKH address from a 20-byte PKH (testnet).
func testAddr(pkh []byte) string {
	addr, _ := script.NewAddressFromPublicKeyHash(pkh, false)
	return addr.AddressString
}

// testPubKeyAddr returns a base58 P2PKH address from a public key (testnet).
func testPubKeyAddr(pub *ec.PublicKey) string {
	addr, _ := script.NewAddressFromPublicKey(pub, false)
	return addr.AddressString
}

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

// --- Buy() error path tests ---

func TestBuy_ValidationError(t *testing.T) {
	_, err := Buy(&BuyParams{TxID: "abc", Config: &BuyerConfig{}})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "wallet key is required")
}

func TestBuy_GetBuyInfoError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := Buy(&BuyParams{
		Client: client.New(srv.URL),
		TxID:   "deadbeef",
		Config: &BuyerConfig{PrivKey: testPrivKey(t)},
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "get buy info")
}

func TestBuy_InvalidCapsuleHashHex(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(client.BuyInfo{
			CapsuleHash:  "not-valid-hex",
			Price:        100,
			PaymentAddr:  strings.Repeat("aa", 20),
			SellerPubKey: strings.Repeat("bb", 33),
		})
	}))
	defer srv.Close()

	_, err := Buy(&BuyParams{
		Client: client.New(srv.URL),
		TxID:   "deadbeef",
		Config: &BuyerConfig{PrivKey: testPrivKey(t)},
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid capsule hash hex")
}

func TestBuy_InvalidPaymentAddr(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(client.BuyInfo{
			CapsuleHash:  strings.Repeat("aa", 32),
			Price:        100,
			PaymentAddr:  "not-a-valid-address!",
			SellerPubKey: strings.Repeat("bb", 33),
		})
	}))
	defer srv.Close()

	_, err := Buy(&BuyParams{
		Client: client.New(srv.URL),
		TxID:   "deadbeef",
		Config: &BuyerConfig{PrivKey: testPrivKey(t)},
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid payment address")
}

func TestBuy_InvalidSellerPubKeyHex(t *testing.T) {
	pkhBytes, _ := hex.DecodeString(strings.Repeat("aa", 20))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(client.BuyInfo{
			CapsuleHash:  strings.Repeat("aa", 32),
			Price:        100,
			PaymentAddr:  testAddr(pkhBytes),
			SellerPubKey: "not-hex!",
		})
	}))
	defer srv.Close()

	_, err := Buy(&BuyParams{
		Client: client.New(srv.URL),
		TxID:   "deadbeef",
		Config: &BuyerConfig{PrivKey: testPrivKey(t)},
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid seller pubkey hex")
}

func TestBuy_ResolveUTXOsError(t *testing.T) {
	pkhBytes, _ := hex.DecodeString(strings.Repeat("aa", 20))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(client.BuyInfo{
			CapsuleHash:  strings.Repeat("aa", 32),
			Price:        100,
			PaymentAddr:  testAddr(pkhBytes),
			SellerPubKey: strings.Repeat("bb", 33),
		})
	}))
	defer srv.Close()

	// No manual UTXOs and no blockchain → resolveUTXOs error
	_, err := Buy(&BuyParams{
		Client: client.New(srv.URL),
		TxID:   "deadbeef",
		Config: &BuyerConfig{PrivKey: testPrivKey(t)},
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no UTXOs available")
}

func TestBuy_SubmitHTLCError(t *testing.T) {
	pk := testPrivKey(t)
	buyerPKH := pk.PubKey().Hash()
	p2pkh := BuildP2PKHScript(buyerPKH)

	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.Method == "GET" {
			// GetBuyInfo — return valid info with the buyer's own pubkey as seller
			// so BuildHTLCFundingTx can construct a valid tx.
			json.NewEncoder(w).Encode(client.BuyInfo{
				CapsuleHash:  strings.Repeat("aa", 32),
				Price:        100,
				PaymentAddr:  testPubKeyAddr(pk.PubKey()),
				SellerPubKey: hex.EncodeToString(pk.PubKey().Compressed()),
			})
		} else {
			// SubmitHTLC — return server error
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	_, err := Buy(&BuyParams{
		Client: client.New(srv.URL),
		TxID:   "deadbeef",
		Config: &BuyerConfig{
			PrivKey: pk,
			ManualUTXOs: []*x402.HTLCUTXO{
				{TxID: make([]byte, 32), Vout: 0, Amount: 100000, ScriptPubKey: p2pkh},
			},
		},
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "submit HTLC")
}

func TestBuy_SuccessFlow(t *testing.T) {
	pk := testPrivKey(t)
	buyerPKH := pk.PubKey().Hash()
	p2pkh := BuildP2PKHScript(buyerPKH)

	capsuleHex := strings.Repeat("cc", 16)
	nonceHex := strings.Repeat("dd", 16)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			json.NewEncoder(w).Encode(client.BuyInfo{
				CapsuleHash:  strings.Repeat("aa", 32),
				Price:        100,
				PaymentAddr:  testPubKeyAddr(pk.PubKey()),
				SellerPubKey: hex.EncodeToString(pk.PubKey().Compressed()),
			})
		} else {
			json.NewEncoder(w).Encode(client.CapsuleResponse{
				Capsule:      capsuleHex,
				CapsuleNonce: nonceHex,
			})
		}
	}))
	defer srv.Close()

	result, err := Buy(&BuyParams{
		Client: client.New(srv.URL),
		TxID:   "deadbeef",
		Config: &BuyerConfig{
			PrivKey: pk,
			ManualUTXOs: []*x402.HTLCUTXO{
				{TxID: make([]byte, 32), Vout: 0, Amount: 100000, ScriptPubKey: p2pkh},
			},
		},
	})
	require.NoError(t, err)
	assert.NotEmpty(t, result.Capsule)
	assert.NotEmpty(t, result.CapsuleNonce)
	assert.NotEmpty(t, result.HTLCTxID)
	assert.Equal(t, uint64(100000), result.CostSatoshis)
}

func TestBuy_SuccessWithoutNonce(t *testing.T) {
	pk := testPrivKey(t)
	buyerPKH := pk.PubKey().Hash()
	p2pkh := BuildP2PKHScript(buyerPKH)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			json.NewEncoder(w).Encode(client.BuyInfo{
				CapsuleHash:  strings.Repeat("aa", 32),
				Price:        100,
				PaymentAddr:  testPubKeyAddr(pk.PubKey()),
				SellerPubKey: hex.EncodeToString(pk.PubKey().Compressed()),
			})
		} else {
			json.NewEncoder(w).Encode(client.CapsuleResponse{
				Capsule: strings.Repeat("cc", 16),
			})
		}
	}))
	defer srv.Close()

	result, err := Buy(&BuyParams{
		Client: client.New(srv.URL),
		TxID:   "deadbeef",
		Config: &BuyerConfig{
			PrivKey: pk,
			ManualUTXOs: []*x402.HTLCUTXO{
				{TxID: make([]byte, 32), Vout: 0, Amount: 100000, ScriptPubKey: p2pkh},
			},
		},
	})
	require.NoError(t, err)
	assert.Nil(t, result.CapsuleNonce)
}

func TestBuy_InvalidCapsuleHexInResponse(t *testing.T) {
	pk := testPrivKey(t)
	buyerPKH := pk.PubKey().Hash()
	p2pkh := BuildP2PKHScript(buyerPKH)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			json.NewEncoder(w).Encode(client.BuyInfo{
				CapsuleHash:  strings.Repeat("aa", 32),
				Price:        100,
				PaymentAddr:  testPubKeyAddr(pk.PubKey()),
				SellerPubKey: hex.EncodeToString(pk.PubKey().Compressed()),
			})
		} else {
			json.NewEncoder(w).Encode(client.CapsuleResponse{
				Capsule: "not-hex!",
			})
		}
	}))
	defer srv.Close()

	_, err := Buy(&BuyParams{
		Client: client.New(srv.URL),
		TxID:   "deadbeef",
		Config: &BuyerConfig{
			PrivKey: pk,
			ManualUTXOs: []*x402.HTLCUTXO{
				{TxID: make([]byte, 32), Vout: 0, Amount: 100000, ScriptPubKey: p2pkh},
			},
		},
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid capsule hex in response")
}

func TestBuy_InvalidNonceHexInResponse(t *testing.T) {
	pk := testPrivKey(t)
	buyerPKH := pk.PubKey().Hash()
	p2pkh := BuildP2PKHScript(buyerPKH)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			json.NewEncoder(w).Encode(client.BuyInfo{
				CapsuleHash:  strings.Repeat("aa", 32),
				Price:        100,
				PaymentAddr:  testPubKeyAddr(pk.PubKey()),
				SellerPubKey: hex.EncodeToString(pk.PubKey().Compressed()),
			})
		} else {
			json.NewEncoder(w).Encode(client.CapsuleResponse{
				Capsule:      strings.Repeat("cc", 16),
				CapsuleNonce: "not-hex!",
			})
		}
	}))
	defer srv.Close()

	_, err := Buy(&BuyParams{
		Client: client.New(srv.URL),
		TxID:   "deadbeef",
		Config: &BuyerConfig{
			PrivKey: pk,
			ManualUTXOs: []*x402.HTLCUTXO{
				{TxID: make([]byte, 32), Vout: 0, Amount: 100000, ScriptPubKey: p2pkh},
			},
		},
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid capsule nonce hex in response")
}

// --- resolveUTXOs tests ---

func TestResolveUTXOs_ManualSufficient(t *testing.T) {
	pk := testPrivKey(t)
	utxos, err := resolveUTXOs(&BuyParams{
		Config: &BuyerConfig{
			PrivKey: pk,
			ManualUTXOs: []*x402.HTLCUTXO{
				{TxID: make([]byte, 32), Vout: 0, Amount: 100000},
			},
		},
	}, 100)
	require.NoError(t, err)
	assert.Len(t, utxos, 1)
}

func TestResolveUTXOs_ManualInsufficient(t *testing.T) {
	pk := testPrivKey(t)
	_, err := resolveUTXOs(&BuyParams{
		Config: &BuyerConfig{
			PrivKey: pk,
			ManualUTXOs: []*x402.HTLCUTXO{
				{TxID: make([]byte, 32), Vout: 0, Amount: 10},
			},
		},
	}, 100000)
	assert.ErrorIs(t, err, ErrInsufficientBalance)
}

func TestResolveUTXOs_NoBlockchainNoManual(t *testing.T) {
	pk := testPrivKey(t)
	_, err := resolveUTXOs(&BuyParams{
		Config: &BuyerConfig{PrivKey: pk},
	}, 100)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no UTXOs available")
}

func TestResolveUTXOs_BlockchainAutoSelect(t *testing.T) {
	pk := testPrivKey(t)
	mock := &network.MockBlockchainService{
		ListUnspentFn: func(_ context.Context, _ string) ([]*network.UTXO, error) {
			return []*network.UTXO{
				{
					TxID:         strings.Repeat("aa", 32),
					Vout:         0,
					Amount:       100000,
					ScriptPubKey: "76a914" + strings.Repeat("00", 20) + "88ac",
				},
			}, nil
		},
	}
	utxos, err := resolveUTXOs(&BuyParams{
		Config:     &BuyerConfig{PrivKey: pk},
		Blockchain: mock,
	}, 100)
	require.NoError(t, err)
	assert.NotEmpty(t, utxos)
}
