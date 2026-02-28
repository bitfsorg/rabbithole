package buy

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/bitfsorg/libbitfs-go/network"
)

func TestSelectUTXOs_SingleLargeUTXO(t *testing.T) {
	utxos := []*network.UTXO{
		{TxID: "aa" + strings.Repeat("00", 31), Vout: 0, Amount: 100000, ScriptPubKey: "76a914" + strings.Repeat("00", 20) + "88ac"},
	}
	mock := &network.MockBlockchainService{
		ListUnspentFn: func(_ context.Context, _ string) ([]*network.UTXO, error) {
			return utxos, nil
		},
	}

	selected, err := SelectUTXOs(context.Background(), mock, "1Address", 1000, 1)
	require.NoError(t, err)
	assert.Len(t, selected, 1)
	assert.Equal(t, uint64(100000), selected[0].Amount)
}

func TestSelectUTXOs_MultipleUTXOs(t *testing.T) {
	utxos := []*network.UTXO{
		{TxID: "aa" + strings.Repeat("00", 31), Vout: 0, Amount: 500, ScriptPubKey: "76a914" + strings.Repeat("00", 20) + "88ac"},
		{TxID: "bb" + strings.Repeat("00", 31), Vout: 0, Amount: 800, ScriptPubKey: "76a914" + strings.Repeat("00", 20) + "88ac"},
		{TxID: "cc" + strings.Repeat("00", 31), Vout: 0, Amount: 300, ScriptPubKey: "76a914" + strings.Repeat("00", 20) + "88ac"},
	}
	mock := &network.MockBlockchainService{
		ListUnspentFn: func(_ context.Context, _ string) ([]*network.UTXO, error) {
			return utxos, nil
		},
	}

	selected, err := SelectUTXOs(context.Background(), mock, "1Address", 1000, 1)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(selected), 2)
}

func TestSelectUTXOs_InsufficientBalance(t *testing.T) {
	utxos := []*network.UTXO{
		{TxID: "aa" + strings.Repeat("00", 31), Vout: 0, Amount: 100, ScriptPubKey: "76a914" + strings.Repeat("00", 20) + "88ac"},
	}
	mock := &network.MockBlockchainService{
		ListUnspentFn: func(_ context.Context, _ string) ([]*network.UTXO, error) {
			return utxos, nil
		},
	}

	_, err := SelectUTXOs(context.Background(), mock, "1Address", 10000, 1)
	assert.ErrorIs(t, err, ErrInsufficientBalance)
}

func TestSelectUTXOs_NoUTXOs(t *testing.T) {
	mock := &network.MockBlockchainService{
		ListUnspentFn: func(_ context.Context, _ string) ([]*network.UTXO, error) {
			return nil, nil
		},
	}

	_, err := SelectUTXOs(context.Background(), mock, "1Address", 1000, 1)
	assert.ErrorIs(t, err, ErrInsufficientBalance)
}

func TestEstimateFee(t *testing.T) {
	fee := EstimateFee(1, 2, 1)
	// 1 input * 148 + 2 outputs * 34 + 10 overhead = 226
	assert.Equal(t, uint64(1*(148+2*34+10)), fee)
}

func TestSelectUTXOs_SkipsInvalidTxID(t *testing.T) {
	utxos := []*network.UTXO{
		{TxID: "not-valid-hex", Vout: 0, Amount: 50000, ScriptPubKey: "76a914" + strings.Repeat("00", 20) + "88ac"},
		{TxID: "bb" + strings.Repeat("00", 31), Vout: 0, Amount: 50000, ScriptPubKey: "76a914" + strings.Repeat("00", 20) + "88ac"},
	}
	mock := &network.MockBlockchainService{
		ListUnspentFn: func(_ context.Context, _ string) ([]*network.UTXO, error) {
			return utxos, nil
		},
	}

	selected, err := SelectUTXOs(context.Background(), mock, "1Address", 1000, 1)
	require.NoError(t, err)
	assert.Len(t, selected, 1)
	// Should have selected the valid one (bb...).
	assert.Equal(t, uint64(50000), selected[0].Amount)
}

func TestSelectUTXOs_GreedySelectsLargestFirst(t *testing.T) {
	utxos := []*network.UTXO{
		{TxID: "aa" + strings.Repeat("00", 31), Vout: 0, Amount: 100, ScriptPubKey: "76a914" + strings.Repeat("00", 20) + "88ac"},
		{TxID: "bb" + strings.Repeat("00", 31), Vout: 0, Amount: 5000, ScriptPubKey: "76a914" + strings.Repeat("00", 20) + "88ac"},
		{TxID: "cc" + strings.Repeat("00", 31), Vout: 0, Amount: 200, ScriptPubKey: "76a914" + strings.Repeat("00", 20) + "88ac"},
	}
	mock := &network.MockBlockchainService{
		ListUnspentFn: func(_ context.Context, _ string) ([]*network.UTXO, error) {
			return utxos, nil
		},
	}

	selected, err := SelectUTXOs(context.Background(), mock, "1Address", 1000, 1)
	require.NoError(t, err)
	// Should select only the 5000-sat UTXO (largest first, sufficient alone).
	assert.Len(t, selected, 1)
	assert.Equal(t, uint64(5000), selected[0].Amount)
}
