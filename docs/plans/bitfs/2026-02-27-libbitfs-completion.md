# libbitfs-go Feature Completion Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement all features defined in the design documents but missing from libbitfs-go — TLV protocol completion, content processing (compression/chunking/Rabin), economic system (revshare/ISO), and access control stubs.

**Architecture:** Four phases with zero new dependencies through Phase 3. Phase 1 extends the TLV parser with 11 new fields. Phase 2 adds content processing functions in storage/ and method42/. Phase 3 creates the revshare/ package from scratch. Phase 4 stubs ACL types and adds version log/share list structures.

**Tech Stack:** Go 1.25, github.com/bsv-blockchain/go-sdk v1.2.18, math/big (Rabin), compress/gzip + compress/lzw (compression), crypto/rand (key generation)

**Design doc:** `docs/plans/2026-02-27-libbitfs-completion-design.md`

**Test convention:** Table-driven tests, `require` for fatal checks, `assert` for comparisons, `_test.go` suffix, `testify` assertions.

---

## Phase 1: Protocol Layer Completion

### Task 1: Add new TLV tag constants and Node struct fields

**Files:**
- Modify: `libbitfs-go/metanet/node.go:127-146`
- Modify: `libbitfs-go/metanet/parser.go:10-49`

**Step 1: Add compression scheme constants and ISOConfig type to node.go**

Add after `AccessPaid` (line 90), before `ChildEntry`:

```go
// CompressionScheme constants for the Compression field.
const (
	CompressNone int32 = 0
	CompressLZW  int32 = 1
	CompressGZIP int32 = 2
	CompressZSTD int32 = 3
)

// ISOStatus represents the state of an Initial Share Offering.
type ISOStatus uint8

const (
	ISOStatusNone    ISOStatus = 0
	ISOStatusOpen    ISOStatus = 1
	ISOStatusPartial ISOStatus = 2
	ISOStatusClosed  ISOStatus = 3
)

// ISOConfig holds ISO (Initial Share Offering) parameters.
type ISOConfig struct {
	TotalShares   uint64
	PricePerShare uint64
	CreatorAddr   []byte    // 20 bytes P2PKH hash
	Status        ISOStatus
}
```

**Step 2: Add new fields to Node struct**

Add after `NetworkName string` (line 136), before the Anchor section:

```go
	// Extended fields (Protocol Layer Completion)
	VersionLog        []byte     // P_node pointing to version log node (33 bytes)
	ShareList         []byte     // P_node pointing to share list node (33 bytes)
	ChunkIndex        uint32     // Chunk index (0-based) for chunked content
	TotalChunks       uint32     // Total number of chunks (0 = not chunked)
	RecombinationHash []byte     // SHA256(chunk₀ ‖ chunk₁ ‖ ...) (32 bytes)
	RabinSignature    []byte     // Rabin signature (S, U) serialized
	RabinPubKey       []byte     // Rabin public key (modulus n)
	RegistryTxID      []byte     // Registry UTXO TxID (32 bytes)
	RegistryVout      uint32     // Registry UTXO output index
	ISO               *ISOConfig // ISO configuration (nil = no ISO)
	ACLRef            []byte     // ACL reference (group pubkey hash or ACL TxID)
```

**Step 3: Add new tag constants to parser.go**

Add after `tagEncPayload = 0x1B` (line 39), before the Anchor section:

```go
	// Extended fields (skip 0x1C-0x1D reserved, 0x20-0x26 used by Anchor)
	tagMetadata          = 0x1E // map<string,string> sub-TLV
	tagVersionLog        = 0x1F // bytes(33), P_node
	tagShareList         = 0x27 // bytes(33), P_node — skips 0x20-0x26 Anchor range
	tagChunkIndex        = 0x28 // uint32
	tagTotalChunks       = 0x29 // uint32
	tagRecombinationHash = 0x2A // bytes(32)
	tagRabinSignature    = 0x2B // bytes (variable)
	tagRabinPubKey       = 0x2C // bytes (variable)
	tagRegistryTxID      = 0x2D // bytes(32)
	tagRegistryVout      = 0x2E // uint32
	tagISOConfig         = 0x2F // bytes (sub-TLV, 37 bytes)
	tagACLRef            = 0x30 // bytes (variable)
```

**Step 4: Run existing tests to verify no regressions**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs-go && go test ./metanet/ -count=1`
Expected: All existing tests PASS (struct additions are backward compatible).

**Step 5: Commit**

```bash
git add libbitfs-go/metanet/node.go libbitfs-go/metanet/parser.go
git commit -m "feat(metanet): add extended TLV tag constants and Node fields"
```

---

### Task 2: Implement Metadata serialization/deserialization

**Files:**
- Modify: `libbitfs-go/metanet/parser.go:88-247` (SerializePayload) and `parser.go:320-470` (deserializePayload)
- Test: `libbitfs-go/metanet/parser_extended_test.go` (new)

**Step 1: Write failing tests**

Create `libbitfs-go/metanet/parser_extended_test.go`:

```go
package metanet

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSerializePayload_Metadata_RoundTrip(t *testing.T) {
	tests := []struct {
		name     string
		metadata map[string]string
	}{
		{"empty", map[string]string{}},
		{"single entry", map[string]string{"author": "alice"}},
		{"multiple entries", map[string]string{
			"author":  "alice",
			"license": "MIT",
			"version": "1.0",
		}},
		{"utf8 values", map[string]string{"名前": "太郎", "描述": "テスト"}},
		{"empty value", map[string]string{"tag": ""}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := &Node{
				Version:  1,
				Type:     NodeTypeFile,
				Metadata: tt.metadata,
			}

			payload, err := SerializePayload(node)
			require.NoError(t, err)

			decoded := &Node{Metadata: make(map[string]string)}
			err = deserializePayload(payload, decoded)
			require.NoError(t, err)

			assert.Equal(t, tt.metadata, decoded.Metadata)
		})
	}
}

func TestSerializePayload_Metadata_BackwardCompat(t *testing.T) {
	// Node with no metadata should still parse (no metadata tag emitted)
	node := &Node{
		Version:  1,
		Type:     NodeTypeFile,
		Metadata: map[string]string{},
	}

	payload, err := SerializePayload(node)
	require.NoError(t, err)

	// Verify tagMetadata not present in payload
	for i := 0; i < len(payload); i++ {
		if payload[i] == tagMetadata {
			t.Fatal("tagMetadata should not be emitted for empty metadata")
		}
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs-go && go test ./metanet/ -run TestSerializePayload_Metadata -v`
Expected: FAIL (metadata not serialized/deserialized)

**Step 3: Implement Metadata S/D in parser.go**

Add metadata serialization helpers (before `serializeChildEntry`):

```go
func serializeMetadata(m map[string]string) []byte {
	var buf []byte
	for k, v := range m {
		kb := []byte(k)
		vb := []byte(v)
		kLen := make([]byte, 2)
		binary.LittleEndian.PutUint16(kLen, uint16(len(kb)))
		buf = append(buf, kLen...)
		buf = append(buf, kb...)
		vLen := make([]byte, 2)
		binary.LittleEndian.PutUint16(vLen, uint16(len(vb)))
		buf = append(buf, vLen...)
		buf = append(buf, vb...)
	}
	return buf
}

func deserializeMetadata(data []byte) (map[string]string, error) {
	m := make(map[string]string)
	offset := 0
	for offset < len(data) {
		if offset+2 > len(data) {
			return nil, fmt.Errorf("metadata: truncated key length at offset %d", offset)
		}
		kLen := int(binary.LittleEndian.Uint16(data[offset : offset+2]))
		offset += 2
		if offset+kLen > len(data) {
			return nil, fmt.Errorf("metadata: truncated key at offset %d", offset)
		}
		key := string(data[offset : offset+kLen])
		offset += kLen
		if offset+2 > len(data) {
			return nil, fmt.Errorf("metadata: truncated value length at offset %d", offset)
		}
		vLen := int(binary.LittleEndian.Uint16(data[offset : offset+2]))
		offset += 2
		if offset+vLen > len(data) {
			return nil, fmt.Errorf("metadata: truncated value at offset %d", offset)
		}
		m[key] = string(data[offset : offset+vLen])
		offset += vLen
	}
	return m, nil
}
```

In `SerializePayload`, after the EncPayload section (line ~221), before Anchor fields:

```go
	// Metadata (extended)
	if len(node.Metadata) > 0 {
		metaBytes := serializeMetadata(node.Metadata)
		buf = appendBytesField(buf, tagMetadata, metaBytes)
	}
```

In `deserializePayload`, add case before `default:`:

```go
		case tagMetadata:
			meta, err := deserializeMetadata(value)
			if err != nil {
				return fmt.Errorf("invalid metadata: %w", err)
			}
			for k, v := range meta {
				node.Metadata[k] = v
			}
```

**Step 4: Run tests to verify they pass**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs-go && go test ./metanet/ -run TestSerializePayload_Metadata -v`
Expected: PASS

**Step 5: Run all metanet tests**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs-go && go test ./metanet/ -count=1 -race`
Expected: All PASS

**Step 6: Commit**

```bash
git add libbitfs-go/metanet/parser.go libbitfs-go/metanet/parser_extended_test.go
git commit -m "feat(metanet): implement Metadata map TLV serialization"
```

---

### Task 3: Implement remaining simple field S/D (VersionLog through ACLRef)

**Files:**
- Modify: `libbitfs-go/metanet/parser.go`
- Modify: `libbitfs-go/metanet/parser_extended_test.go`

**Step 1: Write failing tests for all new fields**

Append to `parser_extended_test.go`:

```go
func TestSerializePayload_ExtendedFields_RoundTrip(t *testing.T) {
	node := &Node{
		Version:           1,
		Type:              NodeTypeFile,
		Metadata:          make(map[string]string),
		VersionLog:        makePubKey(0xA1),
		ShareList:         makePubKey(0xA2),
		ChunkIndex:        3,
		TotalChunks:       10,
		RecombinationHash: bytes.Repeat([]byte{0xCC}, 32),
		RabinSignature:    bytes.Repeat([]byte{0xDD}, 64),
		RabinPubKey:       bytes.Repeat([]byte{0xEE}, 128),
		RegistryTxID:      makeTxID(0xF1),
		RegistryVout:      2,
		ACLRef:            bytes.Repeat([]byte{0xAA}, 32),
	}

	payload, err := SerializePayload(node)
	require.NoError(t, err)

	decoded := &Node{Metadata: make(map[string]string)}
	err = deserializePayload(payload, decoded)
	require.NoError(t, err)

	assert.Equal(t, node.VersionLog, decoded.VersionLog)
	assert.Equal(t, node.ShareList, decoded.ShareList)
	assert.Equal(t, node.ChunkIndex, decoded.ChunkIndex)
	assert.Equal(t, node.TotalChunks, decoded.TotalChunks)
	assert.Equal(t, node.RecombinationHash, decoded.RecombinationHash)
	assert.Equal(t, node.RabinSignature, decoded.RabinSignature)
	assert.Equal(t, node.RabinPubKey, decoded.RabinPubKey)
	assert.Equal(t, node.RegistryTxID, decoded.RegistryTxID)
	assert.Equal(t, node.RegistryVout, decoded.RegistryVout)
	assert.Equal(t, node.ACLRef, decoded.ACLRef)
}

func TestSerializePayload_ExtendedFields_ZeroValues(t *testing.T) {
	// Zero/nil values should not emit tags
	node := &Node{
		Version:  1,
		Type:     NodeTypeFile,
		Metadata: make(map[string]string),
		// All extended fields at zero/nil
	}

	payload, err := SerializePayload(node)
	require.NoError(t, err)

	// None of the new tags should be present
	newTags := []byte{
		tagVersionLog, tagShareList, tagChunkIndex, tagTotalChunks,
		tagRecombinationHash, tagRabinSignature, tagRabinPubKey,
		tagRegistryTxID, tagRegistryVout, tagISOConfig, tagACLRef,
	}
	for _, tag := range newTags {
		for i := 0; i < len(payload); i++ {
			assert.NotEqual(t, tag, payload[i],
				"tag 0x%02x should not be present for zero value", tag)
		}
	}
}
```

**Step 2: Run to verify failure**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs-go && go test ./metanet/ -run TestSerializePayload_Extended -v`
Expected: FAIL

**Step 3: Add S/D for all simple fields**

In `SerializePayload`, after metadata section, before Anchor fields:

```go
	// VersionLog
	if len(node.VersionLog) > 0 {
		buf = appendBytesField(buf, tagVersionLog, node.VersionLog)
	}

	// ShareList
	if len(node.ShareList) > 0 {
		buf = appendBytesField(buf, tagShareList, node.ShareList)
	}

	// ChunkIndex
	if node.ChunkIndex > 0 {
		buf = appendUint32Field(buf, tagChunkIndex, node.ChunkIndex)
	}

	// TotalChunks
	if node.TotalChunks > 0 {
		buf = appendUint32Field(buf, tagTotalChunks, node.TotalChunks)
	}

	// RecombinationHash
	if len(node.RecombinationHash) > 0 {
		buf = appendBytesField(buf, tagRecombinationHash, node.RecombinationHash)
	}

	// RabinSignature
	if len(node.RabinSignature) > 0 {
		buf = appendBytesField(buf, tagRabinSignature, node.RabinSignature)
	}

	// RabinPubKey
	if len(node.RabinPubKey) > 0 {
		buf = appendBytesField(buf, tagRabinPubKey, node.RabinPubKey)
	}

	// RegistryTxID
	if len(node.RegistryTxID) > 0 {
		buf = appendBytesField(buf, tagRegistryTxID, node.RegistryTxID)
	}

	// RegistryVout
	if node.RegistryVout > 0 {
		buf = appendUint32Field(buf, tagRegistryVout, node.RegistryVout)
	}

	// ISOConfig
	if node.ISO != nil {
		isoBytes := serializeISOConfig(node.ISO)
		buf = appendBytesField(buf, tagISOConfig, isoBytes)
	}

	// ACLRef
	if len(node.ACLRef) > 0 {
		buf = appendBytesField(buf, tagACLRef, node.ACLRef)
	}
```

In `deserializePayload`, add cases before `default:`:

```go
		case tagVersionLog:
			node.VersionLog = make([]byte, length)
			copy(node.VersionLog, value)
		case tagShareList:
			node.ShareList = make([]byte, length)
			copy(node.ShareList, value)
		case tagChunkIndex:
			if length == 4 {
				node.ChunkIndex = binary.LittleEndian.Uint32(value)
			}
		case tagTotalChunks:
			if length == 4 {
				node.TotalChunks = binary.LittleEndian.Uint32(value)
			}
		case tagRecombinationHash:
			node.RecombinationHash = make([]byte, length)
			copy(node.RecombinationHash, value)
		case tagRabinSignature:
			node.RabinSignature = make([]byte, length)
			copy(node.RabinSignature, value)
		case tagRabinPubKey:
			node.RabinPubKey = make([]byte, length)
			copy(node.RabinPubKey, value)
		case tagRegistryTxID:
			node.RegistryTxID = make([]byte, length)
			copy(node.RegistryTxID, value)
		case tagRegistryVout:
			if length == 4 {
				node.RegistryVout = binary.LittleEndian.Uint32(value)
			}
		case tagISOConfig:
			iso, err := deserializeISOConfig(value)
			if err != nil {
				return fmt.Errorf("invalid ISO config: %w", err)
			}
			node.ISO = iso
		case tagACLRef:
			node.ACLRef = make([]byte, length)
			copy(node.ACLRef, value)
```

**Step 4: Add ISOConfig S/D helpers**

```go
func serializeISOConfig(iso *ISOConfig) []byte {
	buf := make([]byte, 37) // 8+8+20+1
	binary.LittleEndian.PutUint64(buf[0:8], iso.TotalShares)
	binary.LittleEndian.PutUint64(buf[8:16], iso.PricePerShare)
	copy(buf[16:36], iso.CreatorAddr)
	buf[36] = byte(iso.Status)
	return buf
}

func deserializeISOConfig(data []byte) (*ISOConfig, error) {
	if len(data) != 37 {
		return nil, fmt.Errorf("ISO config must be 37 bytes, got %d", len(data))
	}
	iso := &ISOConfig{
		TotalShares:   binary.LittleEndian.Uint64(data[0:8]),
		PricePerShare: binary.LittleEndian.Uint64(data[8:16]),
		CreatorAddr:   make([]byte, 20),
		Status:        ISOStatus(data[36]),
	}
	copy(iso.CreatorAddr, data[16:36])
	return iso, nil
}
```

**Step 5: Run tests**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs-go && go test ./metanet/ -count=1 -race`
Expected: All PASS

**Step 6: Commit**

```bash
git add libbitfs-go/metanet/parser.go libbitfs-go/metanet/parser_extended_test.go
git commit -m "feat(metanet): implement S/D for all extended TLV fields"
```

---

### Task 4: ISOConfig round-trip tests and CLTV check function

**Files:**
- Modify: `libbitfs-go/metanet/parser_extended_test.go`
- Create: `libbitfs-go/metanet/cltv.go`
- Create: `libbitfs-go/metanet/cltv_test.go`

**Step 1: Write ISOConfig and CLTV tests**

Append to `parser_extended_test.go`:

```go
func TestSerializePayload_ISOConfig_RoundTrip(t *testing.T) {
	tests := []struct {
		name string
		iso  *ISOConfig
	}{
		{"open", &ISOConfig{
			TotalShares: 10000, PricePerShare: 100,
			CreatorAddr: bytes.Repeat([]byte{0xAB}, 20), Status: ISOStatusOpen,
		}},
		{"partial", &ISOConfig{
			TotalShares: 1000000, PricePerShare: 1,
			CreatorAddr: bytes.Repeat([]byte{0x01}, 20), Status: ISOStatusPartial,
		}},
		{"closed", &ISOConfig{
			TotalShares: 100, PricePerShare: 50000,
			CreatorAddr: bytes.Repeat([]byte{0xFF}, 20), Status: ISOStatusClosed,
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := &Node{
				Version:  1,
				Type:     NodeTypeFile,
				Metadata: make(map[string]string),
				ISO:      tt.iso,
			}

			payload, err := SerializePayload(node)
			require.NoError(t, err)

			decoded := &Node{Metadata: make(map[string]string)}
			err = deserializePayload(payload, decoded)
			require.NoError(t, err)

			require.NotNil(t, decoded.ISO)
			assert.Equal(t, tt.iso.TotalShares, decoded.ISO.TotalShares)
			assert.Equal(t, tt.iso.PricePerShare, decoded.ISO.PricePerShare)
			assert.Equal(t, tt.iso.CreatorAddr, decoded.ISO.CreatorAddr)
			assert.Equal(t, tt.iso.Status, decoded.ISO.Status)
		})
	}
}

func TestSerializePayload_ISOConfig_NilOmitted(t *testing.T) {
	node := &Node{Version: 1, Type: NodeTypeFile, Metadata: make(map[string]string)}
	payload, err := SerializePayload(node)
	require.NoError(t, err)

	decoded := &Node{Metadata: make(map[string]string)}
	err = deserializePayload(payload, decoded)
	require.NoError(t, err)
	assert.Nil(t, decoded.ISO)
}
```

Create `libbitfs-go/metanet/cltv.go`:

```go
package metanet

// CLTVResult indicates whether content access is allowed based on block height.
type CLTVResult int

const (
	// CLTVAllowed means no CLTV restriction or height has been reached.
	CLTVAllowed CLTVResult = 0
	// CLTVDenied means the required block height has not been reached.
	CLTVDenied CLTVResult = 1
)

// CheckCLTVAccess checks if content is accessible at the given block height.
// Returns CLTVAllowed if cltv_height is 0 (no restriction) or currentHeight >= cltv_height.
func CheckCLTVAccess(node *Node, currentHeight uint32) CLTVResult {
	if node.CltvHeight == 0 {
		return CLTVAllowed
	}
	if currentHeight >= node.CltvHeight {
		return CLTVAllowed
	}
	return CLTVDenied
}
```

Create `libbitfs-go/metanet/cltv_test.go`:

```go
package metanet

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCheckCLTVAccess(t *testing.T) {
	tests := []struct {
		name          string
		cltvHeight    uint32
		currentHeight uint32
		expected      CLTVResult
	}{
		{"no restriction", 0, 0, CLTVAllowed},
		{"no restriction with height", 0, 100000, CLTVAllowed},
		{"height reached exactly", 500000, 500000, CLTVAllowed},
		{"height exceeded", 500000, 500001, CLTVAllowed},
		{"height not reached", 500000, 499999, CLTVDenied},
		{"height 1 not reached at 0", 1, 0, CLTVDenied},
		{"max height", 4294967295, 4294967295, CLTVAllowed},
		{"max height not reached", 4294967295, 4294967294, CLTVDenied},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := &Node{CltvHeight: tt.cltvHeight}
			result := CheckCLTVAccess(node, tt.currentHeight)
			assert.Equal(t, tt.expected, result)
		})
	}
}
```

**Step 2: Run tests**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs-go && go test ./metanet/ -run "TestSerializePayload_ISOConfig|TestCheckCLTV" -v`
Expected: All PASS (ISOConfig S/D was implemented in Task 3, CLTV is new)

**Step 3: Run full suite**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs-go && go test ./metanet/ -count=1 -race`
Expected: All PASS

**Step 4: Commit**

```bash
git add libbitfs-go/metanet/cltv.go libbitfs-go/metanet/cltv_test.go libbitfs-go/metanet/parser_extended_test.go
git commit -m "feat(metanet): add ISOConfig tests and CLTV access check"
```

---

### Task 5: Update design document with tag resolution

**Files:**
- Modify: `design/bitfs/2-SystemDesign.zh.md:337`

**Step 1: Update the design document**

In `design/bitfs/2-SystemDesign.zh.md`, replace the comment at line 337:

```
  // === 以下字段编号与代码 parser.go tag 常量一致 ===
```

With:

```
  // === fields 19-27: tag 字节 = field 编号的十六进制 (field 19 = 0x13, ..., field 27 = 0x1B) ===
```

And replace the comment at line 364:

```
  // === 以下字段已设计但尚未实现 (编号预留) ===
```

With:

```
  // === 以下字段已实现。fields 30-31 保持自然 tag 映射 (0x1E, 0x1F);
  // fields 32+ tag 跳过 0x20-0x26 (Anchor 专用范围), 从 0x27 起连续分配。
  // 完整映射见 libbitfs-go/metanet/parser.go tag 常量。 ===
```

**Step 2: Commit**

```bash
git add design/bitfs/2-SystemDesign.zh.md
git commit -m "docs: update TLV tag numbering notes in system design"
```

---

## Phase 2: Content Processing

### Task 6: Content compression (LZW + GZIP)

**Files:**
- Create: `libbitfs-go/storage/compress.go`
- Create: `libbitfs-go/storage/compress_test.go`

**Step 1: Write failing tests**

Create `libbitfs-go/storage/compress_test.go`:

```go
package storage

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tongxiaofeng/libbitfs-go/metanet"
)

func TestCompress_RoundTrip(t *testing.T) {
	data := bytes.Repeat([]byte("Hello, BitFS! This is test data for compression. "), 100)

	tests := []struct {
		name   string
		scheme int32
	}{
		{"none", metanet.CompressNone},
		{"lzw", metanet.CompressLZW},
		{"gzip", metanet.CompressGZIP},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compressed, err := Compress(data, tt.scheme)
			require.NoError(t, err)

			decompressed, err := Decompress(compressed, tt.scheme)
			require.NoError(t, err)

			assert.Equal(t, data, decompressed)
		})
	}
}

func TestCompress_None_Identity(t *testing.T) {
	data := []byte("unchanged data")
	compressed, err := Compress(data, metanet.CompressNone)
	require.NoError(t, err)
	assert.Equal(t, data, compressed)
}

func TestCompress_Empty(t *testing.T) {
	for _, scheme := range []int32{metanet.CompressNone, metanet.CompressLZW, metanet.CompressGZIP} {
		compressed, err := Compress([]byte{}, scheme)
		require.NoError(t, err)

		decompressed, err := Decompress(compressed, scheme)
		require.NoError(t, err)
		assert.Empty(t, decompressed)
	}
}

func TestCompress_GZIP_SmallerThanOriginal(t *testing.T) {
	data := bytes.Repeat([]byte("AAAA"), 1000)
	compressed, err := Compress(data, metanet.CompressGZIP)
	require.NoError(t, err)
	assert.Less(t, len(compressed), len(data))
}

func TestCompress_UnsupportedScheme(t *testing.T) {
	_, err := Compress([]byte("data"), metanet.CompressZSTD)
	assert.ErrorIs(t, err, ErrUnsupportedCompression)
}

func TestDecompress_UnsupportedScheme(t *testing.T) {
	_, err := Decompress([]byte("data"), metanet.CompressZSTD)
	assert.ErrorIs(t, err, ErrUnsupportedCompression)
}
```

**Step 2: Run to verify failure**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs-go && go test ./storage/ -run TestCompress -v`
Expected: FAIL (Compress function not defined)

**Step 3: Implement compression**

Create `libbitfs-go/storage/compress.go`:

```go
package storage

import (
	"bytes"
	"compress/gzip"
	"compress/lzw"
	"io"

	"github.com/tongxiaofeng/libbitfs-go/metanet"
)

// Compress compresses data using the specified scheme.
func Compress(data []byte, scheme int32) ([]byte, error) {
	switch scheme {
	case metanet.CompressNone:
		return data, nil
	case metanet.CompressLZW:
		return compressLZW(data)
	case metanet.CompressGZIP:
		return compressGZIP(data)
	default:
		return nil, ErrUnsupportedCompression
	}
}

// Decompress decompresses data using the specified scheme.
func Decompress(data []byte, scheme int32) ([]byte, error) {
	switch scheme {
	case metanet.CompressNone:
		return data, nil
	case metanet.CompressLZW:
		return decompressLZW(data)
	case metanet.CompressGZIP:
		return decompressGZIP(data)
	default:
		return nil, ErrUnsupportedCompression
	}
}

func compressLZW(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	w := lzw.NewWriter(&buf, lzw.LSB, 8)
	if _, err := w.Write(data); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func decompressLZW(data []byte) ([]byte, error) {
	r := lzw.NewReader(bytes.NewReader(data), lzw.LSB, 8)
	defer r.Close()
	return io.ReadAll(r)
}

func compressGZIP(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(data); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func decompressGZIP(data []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}
```

Add to `libbitfs-go/storage/errors.go`:

```go
	// ErrUnsupportedCompression indicates an unsupported compression scheme.
	ErrUnsupportedCompression = errors.New("storage: unsupported compression scheme")
```

**Step 4: Run tests**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs-go && go test ./storage/ -count=1 -race`
Expected: All PASS

**Step 5: Commit**

```bash
git add libbitfs-go/storage/compress.go libbitfs-go/storage/compress_test.go libbitfs-go/storage/errors.go
git commit -m "feat(storage): add LZW and GZIP content compression"
```

---

### Task 7: Content chunking

**Files:**
- Create: `libbitfs-go/storage/chunk.go`
- Create: `libbitfs-go/storage/chunk_test.go`

**Step 1: Write failing tests**

Create `libbitfs-go/storage/chunk_test.go`:

```go
package storage

import (
	"bytes"
	"crypto/sha256"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSplitIntoChunks(t *testing.T) {
	tests := []struct {
		name       string
		dataSize   int
		chunkSize  int
		wantChunks int
	}{
		{"single chunk", 100, 1024, 1},
		{"exact multiple", 3000, 1000, 3},
		{"non-exact", 2500, 1000, 3},
		{"chunk size 1", 5, 1, 5},
		{"data equals chunk", 1000, 1000, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := bytes.Repeat([]byte{0xAB}, tt.dataSize)
			chunks := SplitIntoChunks(data, tt.chunkSize)
			assert.Len(t, chunks, tt.wantChunks)

			// Recombine and verify
			var combined []byte
			for _, chunk := range chunks {
				combined = append(combined, chunk...)
			}
			assert.Equal(t, data, combined)
		})
	}
}

func TestComputeRecombinationHash(t *testing.T) {
	chunks := [][]byte{
		bytes.Repeat([]byte{0x01}, 100),
		bytes.Repeat([]byte{0x02}, 100),
		bytes.Repeat([]byte{0x03}, 100),
	}

	hash := ComputeRecombinationHash(chunks)
	assert.Len(t, hash, 32)

	// Verify it's SHA256 of concatenation
	var combined []byte
	for _, c := range chunks {
		combined = append(combined, c...)
	}
	expected := sha256.Sum256(combined)
	assert.Equal(t, expected[:], hash)
}

func TestRecombineChunks_Valid(t *testing.T) {
	data := bytes.Repeat([]byte{0xAA}, 2500)
	chunks := SplitIntoChunks(data, 1000)
	hash := ComputeRecombinationHash(chunks)

	result, err := RecombineChunks(chunks, hash)
	require.NoError(t, err)
	assert.Equal(t, data, result)
}

func TestRecombineChunks_HashMismatch(t *testing.T) {
	chunks := [][]byte{{0x01}, {0x02}}
	badHash := bytes.Repeat([]byte{0xFF}, 32)

	_, err := RecombineChunks(chunks, badHash)
	assert.ErrorIs(t, err, ErrRecombinationHashMismatch)
}

func TestRecombineChunks_EmptyChunks(t *testing.T) {
	hash := ComputeRecombinationHash(nil)
	result, err := RecombineChunks(nil, hash)
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestSplitIntoChunks_EmptyData(t *testing.T) {
	chunks := SplitIntoChunks(nil, 1024)
	assert.Empty(t, chunks)
}
```

**Step 2: Run to verify failure**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs-go && go test ./storage/ -run "TestSplit|TestRecombine|TestCompute" -v`
Expected: FAIL

**Step 3: Implement chunking**

Create `libbitfs-go/storage/chunk.go`:

```go
package storage

import (
	"bytes"
	"crypto/sha256"
)

// DefaultChunkSize is the default chunk size for content splitting (1MB).
const DefaultChunkSize = 1 << 20

// SplitIntoChunks splits data into fixed-size chunks.
// The last chunk may be smaller than chunkSize.
func SplitIntoChunks(data []byte, chunkSize int) [][]byte {
	if len(data) == 0 {
		return nil
	}
	var chunks [][]byte
	for i := 0; i < len(data); i += chunkSize {
		end := i + chunkSize
		if end > len(data) {
			end = len(data)
		}
		chunk := make([]byte, end-i)
		copy(chunk, data[i:end])
		chunks = append(chunks, chunk)
	}
	return chunks
}

// ComputeRecombinationHash computes SHA256(chunk₀ ‖ chunk₁ ‖ ...).
func ComputeRecombinationHash(chunks [][]byte) []byte {
	h := sha256.New()
	for _, chunk := range chunks {
		h.Write(chunk)
	}
	sum := h.Sum(nil)
	return sum
}

// RecombineChunks concatenates chunks and verifies the recombination hash.
func RecombineChunks(chunks [][]byte, expectedHash []byte) ([]byte, error) {
	var buf bytes.Buffer
	h := sha256.New()
	for _, chunk := range chunks {
		buf.Write(chunk)
		h.Write(chunk)
	}
	actualHash := h.Sum(nil)
	if !bytes.Equal(actualHash, expectedHash) {
		return nil, ErrRecombinationHashMismatch
	}
	return buf.Bytes(), nil
}
```

Add to `libbitfs-go/storage/errors.go`:

```go
	// ErrRecombinationHashMismatch indicates chunk recombination hash verification failed.
	ErrRecombinationHashMismatch = errors.New("storage: recombination hash mismatch")
```

**Step 4: Run tests**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs-go && go test ./storage/ -count=1 -race`
Expected: All PASS

**Step 5: Commit**

```bash
git add libbitfs-go/storage/chunk.go libbitfs-go/storage/chunk_test.go libbitfs-go/storage/errors.go
git commit -m "feat(storage): add content chunking with recombination hash verification"
```

---

### Task 8: Rabin signatures

**Files:**
- Create: `libbitfs-go/method42/rabin.go`
- Create: `libbitfs-go/method42/rabin_test.go`

**Step 1: Write failing tests**

Create `libbitfs-go/method42/rabin_test.go`:

```go
package method42

import (
	"crypto/rand"
	"math/big"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateRabinKey(t *testing.T) {
	key, err := GenerateRabinKey(1024)
	require.NoError(t, err)
	require.NotNil(t, key)

	// p and q should be primes ≡ 3 mod 4
	assert.True(t, key.P.ProbablyPrime(20))
	assert.True(t, key.Q.ProbablyPrime(20))
	assert.Equal(t, int64(3), new(big.Int).Mod(key.P, big.NewInt(4)).Int64())
	assert.Equal(t, int64(3), new(big.Int).Mod(key.Q, big.NewInt(4)).Int64())

	// n = p * q
	expected := new(big.Int).Mul(key.P, key.Q)
	assert.Equal(t, 0, expected.Cmp(key.N))
}

func TestRabinSignVerify(t *testing.T) {
	key, err := GenerateRabinKey(1024)
	require.NoError(t, err)

	message := []byte("Hello, BitFS content authentication!")
	S, U, err := RabinSign(key, message)
	require.NoError(t, err)

	assert.True(t, RabinVerify(key.N, message, S, U))
}

func TestRabinVerify_TamperedMessage(t *testing.T) {
	key, err := GenerateRabinKey(1024)
	require.NoError(t, err)

	message := []byte("original message")
	S, U, err := RabinSign(key, message)
	require.NoError(t, err)

	tampered := []byte("tampered message")
	assert.False(t, RabinVerify(key.N, tampered, S, U))
}

func TestRabinSignature_Serialization(t *testing.T) {
	key, err := GenerateRabinKey(1024)
	require.NoError(t, err)

	message := []byte("serialization test")
	S, U, err := RabinSign(key, message)
	require.NoError(t, err)

	// Serialize
	data := SerializeRabinSignature(S, U)
	assert.NotEmpty(t, data)

	// Deserialize
	S2, U2, err := DeserializeRabinSignature(data)
	require.NoError(t, err)
	assert.Equal(t, 0, S.Cmp(S2))
	assert.Equal(t, U, U2)

	// Verify with deserialized values
	assert.True(t, RabinVerify(key.N, message, S2, U2))
}

func TestRabinPubKey_Serialization(t *testing.T) {
	key, err := GenerateRabinKey(1024)
	require.NoError(t, err)

	data := SerializeRabinPubKey(key.N)
	n2, err := DeserializeRabinPubKey(data)
	require.NoError(t, err)
	assert.Equal(t, 0, key.N.Cmp(n2))
}

func TestRabinSign_MultipleMessages(t *testing.T) {
	key, err := GenerateRabinKey(1024)
	require.NoError(t, err)

	messages := [][]byte{
		[]byte("message 1"),
		[]byte("message 2"),
		[]byte(""),
		make([]byte, 10000), // large message
	}

	for _, msg := range messages {
		S, U, err := RabinSign(key, msg)
		require.NoError(t, err)
		assert.True(t, RabinVerify(key.N, msg, S, U), "failed for message len=%d", len(msg))
	}
}

func BenchmarkRabinSign_1024(b *testing.B) {
	key, _ := GenerateRabinKey(1024)
	msg := make([]byte, 1024)
	rand.Read(msg)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		RabinSign(key, msg)
	}
}

func BenchmarkRabinVerify_1024(b *testing.B) {
	key, _ := GenerateRabinKey(1024)
	msg := make([]byte, 1024)
	rand.Read(msg)
	S, U, _ := RabinSign(key, msg)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		RabinVerify(key.N, msg, S, U)
	}
}
```

**Step 2: Run to verify failure**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs-go && go test ./method42/ -run TestRabin -v`
Expected: FAIL

**Step 3: Implement Rabin signatures**

Create `libbitfs-go/method42/rabin.go`:

```go
package method42

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/big"
)

// RabinKeyPair holds a Rabin signature key pair.
type RabinKeyPair struct {
	P *big.Int // private prime p ≡ 3 (mod 4)
	Q *big.Int // private prime q ≡ 3 (mod 4)
	N *big.Int // public modulus n = p * q
}

// GenerateRabinKey generates a Rabin key pair with primes of bitSize bits each.
func GenerateRabinKey(bitSize int) (*RabinKeyPair, error) {
	p, err := generateBlumPrime(bitSize)
	if err != nil {
		return nil, fmt.Errorf("generating p: %w", err)
	}
	q, err := generateBlumPrime(bitSize)
	if err != nil {
		return nil, fmt.Errorf("generating q: %w", err)
	}
	n := new(big.Int).Mul(p, q)
	return &RabinKeyPair{P: p, Q: q, N: n}, nil
}

// generateBlumPrime generates a random prime p ≡ 3 (mod 4).
func generateBlumPrime(bitSize int) (*big.Int, error) {
	three := big.NewInt(3)
	four := big.NewInt(4)
	for {
		p, err := rand.Prime(rand.Reader, bitSize)
		if err != nil {
			return nil, err
		}
		if new(big.Int).Mod(p, four).Cmp(three) == 0 {
			return p, nil
		}
	}
}

// RabinSign signs a message using the Rabin signature scheme.
// Returns (S, U) where S is the signature and U is the padding.
func RabinSign(key *RabinKeyPair, message []byte) (S *big.Int, U []byte, err error) {
	// Find padding U such that H(message || U) is a quadratic residue mod n
	for counter := uint32(0); ; counter++ {
		padding := make([]byte, 4)
		binary.BigEndian.PutUint32(padding, counter)

		h := rabinHash(message, padding, key.N)

		// Check if h is a quadratic residue mod p and mod q
		// For Blum primes (p ≡ 3 mod 4), h is QR mod p iff h^((p-1)/2) ≡ 1 mod p
		if !isQuadraticResidue(h, key.P) || !isQuadraticResidue(h, key.Q) {
			continue
		}

		// Compute square root using CRT
		// For p ≡ 3 mod 4: sqrt(h) mod p = h^((p+1)/4) mod p
		sp := modSqrtBlum(h, key.P)
		sq := modSqrtBlum(h, key.Q)

		// CRT reconstruction
		S = crt(sp, sq, key.P, key.Q, key.N)
		U = padding
		return S, U, nil
	}
}

// RabinVerify verifies a Rabin signature using only the public modulus n.
// Checks: S² mod n == SHA256(message || U)
func RabinVerify(n *big.Int, message []byte, S *big.Int, U []byte) bool {
	h := rabinHash(message, U, n)
	s2 := new(big.Int).Mul(S, S)
	s2.Mod(s2, n)
	return s2.Cmp(h) == 0
}

func rabinHash(message, padding []byte, n *big.Int) *big.Int {
	combined := make([]byte, 0, len(message)+len(padding))
	combined = append(combined, message...)
	combined = append(combined, padding...)
	hash := sha256.Sum256(combined)
	h := new(big.Int).SetBytes(hash[:])
	h.Mod(h, n)
	return h
}

func isQuadraticResidue(a, p *big.Int) bool {
	// Euler's criterion: a^((p-1)/2) ≡ 1 (mod p)
	exp := new(big.Int).Sub(p, big.NewInt(1))
	exp.Rsh(exp, 1) // (p-1)/2
	result := new(big.Int).Exp(a, exp, p)
	return result.Cmp(big.NewInt(1)) == 0
}

func modSqrtBlum(a, p *big.Int) *big.Int {
	// For p ≡ 3 (mod 4): sqrt(a) = a^((p+1)/4) mod p
	exp := new(big.Int).Add(p, big.NewInt(1))
	exp.Rsh(exp, 2) // (p+1)/4
	return new(big.Int).Exp(a, exp, p)
}

func crt(sp, sq, p, q, n *big.Int) *big.Int {
	// Chinese Remainder Theorem: x = sp*q*(q^{-1} mod p) + sq*p*(p^{-1} mod q) mod n
	qInv := new(big.Int).ModInverse(q, p)
	pInv := new(big.Int).ModInverse(p, q)

	t1 := new(big.Int).Mul(sp, q)
	t1.Mul(t1, qInv)

	t2 := new(big.Int).Mul(sq, p)
	t2.Mul(t2, pInv)

	result := new(big.Int).Add(t1, t2)
	result.Mod(result, n)
	return result
}

// --- Serialization ---

// SerializeRabinSignature encodes (S, U) for TLV storage.
// Format: S_len(4B BE) + S_bytes + U_len(4B BE) + U_bytes
func SerializeRabinSignature(S *big.Int, U []byte) []byte {
	sBytes := S.Bytes()
	buf := make([]byte, 4+len(sBytes)+4+len(U))
	binary.BigEndian.PutUint32(buf[0:4], uint32(len(sBytes)))
	copy(buf[4:4+len(sBytes)], sBytes)
	offset := 4 + len(sBytes)
	binary.BigEndian.PutUint32(buf[offset:offset+4], uint32(len(U)))
	copy(buf[offset+4:], U)
	return buf
}

// DeserializeRabinSignature decodes (S, U) from TLV.
func DeserializeRabinSignature(data []byte) (S *big.Int, U []byte, err error) {
	if len(data) < 8 {
		return nil, nil, fmt.Errorf("rabin signature data too short")
	}
	sLen := int(binary.BigEndian.Uint32(data[0:4]))
	if 4+sLen+4 > len(data) {
		return nil, nil, fmt.Errorf("rabin signature S truncated")
	}
	S = new(big.Int).SetBytes(data[4 : 4+sLen])
	offset := 4 + sLen
	uLen := int(binary.BigEndian.Uint32(data[offset : offset+4]))
	if offset+4+uLen > len(data) {
		return nil, nil, fmt.Errorf("rabin signature U truncated")
	}
	U = make([]byte, uLen)
	copy(U, data[offset+4:offset+4+uLen])
	return S, U, nil
}

// SerializeRabinPubKey encodes modulus n for TLV storage.
func SerializeRabinPubKey(n *big.Int) []byte {
	return n.Bytes()
}

// DeserializeRabinPubKey decodes modulus n from TLV.
func DeserializeRabinPubKey(data []byte) (*big.Int, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("rabin pubkey data empty")
	}
	return new(big.Int).SetBytes(data), nil
}
```

**Step 4: Run tests**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs-go && go test ./method42/ -run TestRabin -v -timeout 60s`
Expected: All PASS (key generation may take a few seconds for 1024-bit primes)

**Step 5: Run full method42 suite**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs-go && go test ./method42/ -count=1 -race`
Expected: All PASS

**Step 6: Commit**

```bash
git add libbitfs-go/method42/rabin.go libbitfs-go/method42/rabin_test.go
git commit -m "feat(method42): add Rabin signature scheme for content authentication"
```

---

## Phase 3: Economic System (revshare package)

### Task 9: Core types and errors

**Files:**
- Create: `libbitfs-go/revshare/types.go`
- Create: `libbitfs-go/revshare/errors.go`

**Step 1: Create types.go**

```go
package revshare

// RevShareEntry represents a shareholder's record in the registry.
type RevShareEntry struct {
	Address [20]byte // P2PKH address hash
	Share   uint64   // Number of shares held
}

// RegistryState represents the current state of a revenue share registry.
type RegistryState struct {
	NodeID      [32]byte        // SHA256(P_node || TxID) of the Metanet node
	TotalShares uint64          // Total shares issued
	Entries     []RevShareEntry // Current shareholders
	ModeFlags   uint8           // bit 0: ISO active, bit 1: locked
}

// IsISOActive returns true if the ISO pool is active.
func (s *RegistryState) IsISOActive() bool {
	return s.ModeFlags&0x01 != 0
}

// IsLocked returns true if share transfers are locked.
func (s *RegistryState) IsLocked() bool {
	return s.ModeFlags&0x02 != 0
}

// FindEntry returns the index and entry for the given address, or -1 if not found.
func (s *RegistryState) FindEntry(addr [20]byte) (int, *RevShareEntry) {
	for i := range s.Entries {
		if s.Entries[i].Address == addr {
			return i, &s.Entries[i]
		}
	}
	return -1, nil
}

// ShareData represents the data embedded in a Share UTXO.
type ShareData struct {
	NodeID [32]byte // Bound Metanet node
	Amount uint64   // Number of shares
}

// ISOPoolState represents the state of an ISO pool UTXO.
type ISOPoolState struct {
	NodeID          [32]byte // Bound Metanet node
	RemainingShares uint64   // Unsold shares
	PricePerShare   uint64   // Price in satoshis
	CreatorAddr     [20]byte // Creator's P2PKH address
}

// Distribution represents a single payout in revenue distribution.
type Distribution struct {
	Address [20]byte
	Amount  uint64
}
```

**Step 2: Create errors.go**

```go
package revshare

import "errors"

var (
	// ErrInvalidRegistryData indicates the registry UTXO data is malformed.
	ErrInvalidRegistryData = errors.New("revshare: invalid registry data")

	// ErrInvalidShareData indicates the share UTXO data is malformed.
	ErrInvalidShareData = errors.New("revshare: invalid share data")

	// ErrInvalidISOPoolData indicates the ISO pool UTXO data is malformed.
	ErrInvalidISOPoolData = errors.New("revshare: invalid ISO pool data")

	// ErrShareConservationViolation indicates shares were created or destroyed.
	ErrShareConservationViolation = errors.New("revshare: share conservation violated")

	// ErrInsufficientPayment indicates the payment is too small to distribute.
	ErrInsufficientPayment = errors.New("revshare: insufficient payment for distribution")

	// ErrNoEntries indicates the registry has no shareholders.
	ErrNoEntries = errors.New("revshare: no shareholder entries")

	// ErrZeroShares indicates a share amount of zero.
	ErrZeroShares = errors.New("revshare: zero share amount")

	// ErrZeroTotalShares indicates total shares is zero.
	ErrZeroTotalShares = errors.New("revshare: zero total shares")

	// ErrEntryNotFound indicates the address was not found in the registry.
	ErrEntryNotFound = errors.New("revshare: entry not found")

	// ErrRegistryLocked indicates share transfers are locked.
	ErrRegistryLocked = errors.New("revshare: registry is locked")
)
```

**Step 3: Verify compilation**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs-go && go build ./revshare/`
Expected: Success (no tests yet, just verify it compiles)

**Step 4: Commit**

```bash
git add libbitfs-go/revshare/types.go libbitfs-go/revshare/errors.go
git commit -m "feat(revshare): add core types and error definitions"
```

---

### Task 10: Registry, Share, and ISOPool serialization

**Files:**
- Create: `libbitfs-go/revshare/registry.go`
- Create: `libbitfs-go/revshare/share.go`
- Create: `libbitfs-go/revshare/pool.go`
- Create: `libbitfs-go/revshare/revshare_test.go`

**Step 1: Write failing tests**

Create `libbitfs-go/revshare/revshare_test.go`:

```go
package revshare

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeAddr(seed byte) [20]byte {
	var addr [20]byte
	for i := range addr {
		addr[i] = seed
	}
	return addr
}

func makeNodeID(seed byte) [32]byte {
	var id [32]byte
	for i := range id {
		id[i] = seed
	}
	return id
}

// --- Registry tests ---

func TestSerializeRegistry_RoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		state *RegistryState
	}{
		{"single entry", &RegistryState{
			NodeID: makeNodeID(0x01), TotalShares: 10000,
			Entries:   []RevShareEntry{{Address: makeAddr(0xAA), Share: 10000}},
			ModeFlags: 0,
		}},
		{"multiple entries", &RegistryState{
			NodeID: makeNodeID(0x02), TotalShares: 10000,
			Entries: []RevShareEntry{
				{Address: makeAddr(0xAA), Share: 3000},
				{Address: makeAddr(0xBB), Share: 2000},
				{Address: makeAddr(0xCC), Share: 5000},
			},
			ModeFlags: 0x01, // ISO active
		}},
		{"locked", &RegistryState{
			NodeID: makeNodeID(0x03), TotalShares: 100,
			Entries:   []RevShareEntry{{Address: makeAddr(0x01), Share: 100}},
			ModeFlags: 0x03, // ISO active + locked
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := SerializeRegistry(tt.state)
			require.NoError(t, err)

			decoded, err := DeserializeRegistry(data)
			require.NoError(t, err)

			assert.Equal(t, tt.state.NodeID, decoded.NodeID)
			assert.Equal(t, tt.state.TotalShares, decoded.TotalShares)
			assert.Equal(t, tt.state.ModeFlags, decoded.ModeFlags)
			require.Len(t, decoded.Entries, len(tt.state.Entries))
			for i := range tt.state.Entries {
				assert.Equal(t, tt.state.Entries[i].Address, decoded.Entries[i].Address)
				assert.Equal(t, tt.state.Entries[i].Share, decoded.Entries[i].Share)
			}
		})
	}
}

func TestSerializeRegistry_Size(t *testing.T) {
	state := &RegistryState{
		NodeID: makeNodeID(0x01), TotalShares: 10000,
		Entries: []RevShareEntry{
			{Address: makeAddr(0xAA), Share: 5000},
			{Address: makeAddr(0xBB), Share: 5000},
		},
		ModeFlags: 0,
	}
	data, err := SerializeRegistry(state)
	require.NoError(t, err)
	// Expected: 32 + 8 + 4 + 24*2 + 1 = 93
	assert.Len(t, data, 93)
}

func TestDeserializeRegistry_TooShort(t *testing.T) {
	_, err := DeserializeRegistry([]byte{0x01, 0x02})
	assert.ErrorIs(t, err, ErrInvalidRegistryData)
}

// --- Share tests ---

func TestSerializeShare_RoundTrip(t *testing.T) {
	share := &ShareData{NodeID: makeNodeID(0x01), Amount: 5000}
	data := SerializeShare(share)
	assert.Len(t, data, 40)

	decoded, err := DeserializeShare(data)
	require.NoError(t, err)
	assert.Equal(t, share.NodeID, decoded.NodeID)
	assert.Equal(t, share.Amount, decoded.Amount)
}

func TestDeserializeShare_WrongSize(t *testing.T) {
	_, err := DeserializeShare([]byte{0x01})
	assert.ErrorIs(t, err, ErrInvalidShareData)
}

// --- ISO Pool tests ---

func TestSerializeISOPool_RoundTrip(t *testing.T) {
	pool := &ISOPoolState{
		NodeID: makeNodeID(0x01), RemainingShares: 6000,
		PricePerShare: 100, CreatorAddr: makeAddr(0xAA),
	}
	data := SerializeISOPool(pool)
	assert.Len(t, data, 68)

	decoded, err := DeserializeISOPool(data)
	require.NoError(t, err)
	assert.Equal(t, pool.NodeID, decoded.NodeID)
	assert.Equal(t, pool.RemainingShares, decoded.RemainingShares)
	assert.Equal(t, pool.PricePerShare, decoded.PricePerShare)
	assert.Equal(t, pool.CreatorAddr, decoded.CreatorAddr)
}

func TestDeserializeISOPool_WrongSize(t *testing.T) {
	_, err := DeserializeISOPool([]byte{0x01})
	assert.ErrorIs(t, err, ErrInvalidISOPoolData)
}
```

**Step 2: Run to verify failure**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs-go && go test ./revshare/ -v`
Expected: FAIL

**Step 3: Implement Registry S/D**

Create `libbitfs-go/revshare/registry.go`:

```go
package revshare

import (
	"encoding/binary"
	"fmt"
)

// registryHeaderSize = node_id(32) + total_shares(8) + num_entries(4) = 44
// registryEntrySize = address(20) + share(8) = 28
// registryTrailerSize = mode_flags(1) = 1
const (
	registryHeaderSize  = 44
	registryEntrySize   = 28
	registryTrailerSize = 1
)

// SerializeRegistry serializes a RegistryState to binary format.
// Format: node_id(32B) + total_shares(8B BE) + num_entries(4B BE) +
//
//	entries[]: address(20B) + share(8B BE) × N + mode_flags(1B)
func SerializeRegistry(state *RegistryState) ([]byte, error) {
	size := registryHeaderSize + registryEntrySize*len(state.Entries) + registryTrailerSize
	buf := make([]byte, size)
	offset := 0

	copy(buf[offset:offset+32], state.NodeID[:])
	offset += 32

	binary.BigEndian.PutUint64(buf[offset:offset+8], state.TotalShares)
	offset += 8

	binary.BigEndian.PutUint32(buf[offset:offset+4], uint32(len(state.Entries)))
	offset += 4

	for _, entry := range state.Entries {
		copy(buf[offset:offset+20], entry.Address[:])
		offset += 20
		binary.BigEndian.PutUint64(buf[offset:offset+8], entry.Share)
		offset += 8
	}

	buf[offset] = state.ModeFlags
	return buf, nil
}

// DeserializeRegistry deserializes binary data into a RegistryState.
func DeserializeRegistry(data []byte) (*RegistryState, error) {
	if len(data) < registryHeaderSize+registryTrailerSize {
		return nil, fmt.Errorf("%w: too short (%d bytes)", ErrInvalidRegistryData, len(data))
	}
	offset := 0

	state := &RegistryState{}
	copy(state.NodeID[:], data[offset:offset+32])
	offset += 32

	state.TotalShares = binary.BigEndian.Uint64(data[offset : offset+8])
	offset += 8

	numEntries := int(binary.BigEndian.Uint32(data[offset : offset+4]))
	offset += 4

	expectedSize := registryHeaderSize + registryEntrySize*numEntries + registryTrailerSize
	if len(data) < expectedSize {
		return nil, fmt.Errorf("%w: expected %d bytes for %d entries, got %d",
			ErrInvalidRegistryData, expectedSize, numEntries, len(data))
	}

	state.Entries = make([]RevShareEntry, numEntries)
	for i := 0; i < numEntries; i++ {
		copy(state.Entries[i].Address[:], data[offset:offset+20])
		offset += 20
		state.Entries[i].Share = binary.BigEndian.Uint64(data[offset : offset+8])
		offset += 8
	}

	state.ModeFlags = data[offset]
	return state, nil
}
```

**Step 4: Implement Share S/D**

Create `libbitfs-go/revshare/share.go`:

```go
package revshare

import (
	"encoding/binary"
	"fmt"
)

const shareDataSize = 40 // node_id(32) + share_amount(8)

// SerializeShare encodes ShareData to binary format.
func SerializeShare(data *ShareData) []byte {
	buf := make([]byte, shareDataSize)
	copy(buf[0:32], data.NodeID[:])
	binary.BigEndian.PutUint64(buf[32:40], data.Amount)
	return buf
}

// DeserializeShare decodes binary data into ShareData.
func DeserializeShare(data []byte) (*ShareData, error) {
	if len(data) != shareDataSize {
		return nil, fmt.Errorf("%w: expected %d bytes, got %d", ErrInvalidShareData, shareDataSize, len(data))
	}
	share := &ShareData{}
	copy(share.NodeID[:], data[0:32])
	share.Amount = binary.BigEndian.Uint64(data[32:40])
	return share, nil
}
```

**Step 5: Implement ISOPool S/D**

Create `libbitfs-go/revshare/pool.go`:

```go
package revshare

import (
	"encoding/binary"
	"fmt"
)

const isoPoolSize = 68 // node_id(32) + remaining(8) + price(8) + creator(20)

// SerializeISOPool encodes ISOPoolState to binary format.
func SerializeISOPool(state *ISOPoolState) []byte {
	buf := make([]byte, isoPoolSize)
	copy(buf[0:32], state.NodeID[:])
	binary.BigEndian.PutUint64(buf[32:40], state.RemainingShares)
	binary.BigEndian.PutUint64(buf[40:48], state.PricePerShare)
	copy(buf[48:68], state.CreatorAddr[:])
	return buf
}

// DeserializeISOPool decodes binary data into ISOPoolState.
func DeserializeISOPool(data []byte) (*ISOPoolState, error) {
	if len(data) != isoPoolSize {
		return nil, fmt.Errorf("%w: expected %d bytes, got %d", ErrInvalidISOPoolData, isoPoolSize, len(data))
	}
	state := &ISOPoolState{}
	copy(state.NodeID[:], data[0:32])
	state.RemainingShares = binary.BigEndian.Uint64(data[32:40])
	state.PricePerShare = binary.BigEndian.Uint64(data[40:48])
	copy(state.CreatorAddr[:], data[48:68])
	return state, nil
}
```

**Step 6: Run tests**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs-go && go test ./revshare/ -count=1 -race`
Expected: All PASS

**Step 7: Commit**

```bash
git add libbitfs-go/revshare/registry.go libbitfs-go/revshare/share.go libbitfs-go/revshare/pool.go libbitfs-go/revshare/revshare_test.go
git commit -m "feat(revshare): implement Registry, Share, and ISOPool serialization"
```

---

### Task 11: Revenue distribution algorithm

**Files:**
- Create: `libbitfs-go/revshare/distribute.go`
- Modify: `libbitfs-go/revshare/revshare_test.go`

**Step 1: Write failing tests**

Append to `revshare_test.go`:

```go
func TestDistributeRevenue(t *testing.T) {
	tests := []struct {
		name         string
		totalPayment uint64
		entries      []RevShareEntry
		totalShares  uint64
		wantAmounts  []uint64
	}{
		{
			"exact division",
			10000,
			[]RevShareEntry{
				{Address: makeAddr(0xAA), Share: 3000},
				{Address: makeAddr(0xBB), Share: 2000},
				{Address: makeAddr(0xCC), Share: 5000},
			},
			10000,
			[]uint64{3000, 2000, 5000},
		},
		{
			"remainder goes to last",
			10,
			[]RevShareEntry{
				{Address: makeAddr(0xAA), Share: 3333},
				{Address: makeAddr(0xBB), Share: 3333},
				{Address: makeAddr(0xCC), Share: 3334},
			},
			10000,
			[]uint64{3, 3, 4}, // 3+3+4=10, last gets remainder
		},
		{
			"single shareholder",
			5000,
			[]RevShareEntry{
				{Address: makeAddr(0xAA), Share: 10000},
			},
			10000,
			[]uint64{5000},
		},
		{
			"two shareholders equal",
			100,
			[]RevShareEntry{
				{Address: makeAddr(0xAA), Share: 5000},
				{Address: makeAddr(0xBB), Share: 5000},
			},
			10000,
			[]uint64{50, 50},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dists, err := DistributeRevenue(tt.totalPayment, tt.entries, tt.totalShares)
			require.NoError(t, err)
			require.Len(t, dists, len(tt.entries))

			var total uint64
			for i, d := range dists {
				assert.Equal(t, tt.entries[i].Address, d.Address)
				assert.Equal(t, tt.wantAmounts[i], d.Amount, "entry %d", i)
				total += d.Amount
			}
			assert.Equal(t, tt.totalPayment, total, "total payout must equal totalPayment")
		})
	}
}

func TestDistributeRevenue_Errors(t *testing.T) {
	entries := []RevShareEntry{{Address: makeAddr(0xAA), Share: 5000}}

	_, err := DistributeRevenue(0, entries, 10000)
	assert.ErrorIs(t, err, ErrInsufficientPayment)

	_, err = DistributeRevenue(100, nil, 10000)
	assert.ErrorIs(t, err, ErrNoEntries)

	_, err = DistributeRevenue(100, entries, 0)
	assert.ErrorIs(t, err, ErrZeroTotalShares)
}
```

**Step 2: Run to verify failure**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs-go && go test ./revshare/ -run TestDistribute -v`
Expected: FAIL

**Step 3: Implement distribution**

Create `libbitfs-go/revshare/distribute.go`:

```go
package revshare

// DistributeRevenue calculates per-shareholder payouts.
// The last entry gets the remainder to avoid integer division precision loss.
func DistributeRevenue(totalPayment uint64, entries []RevShareEntry, totalShares uint64) ([]Distribution, error) {
	if totalPayment == 0 {
		return nil, ErrInsufficientPayment
	}
	if len(entries) == 0 {
		return nil, ErrNoEntries
	}
	if totalShares == 0 {
		return nil, ErrZeroTotalShares
	}

	distributions := make([]Distribution, len(entries))
	var distributed uint64

	for i, entry := range entries {
		distributions[i].Address = entry.Address
		if i == len(entries)-1 {
			// Last shareholder gets remainder
			distributions[i].Amount = totalPayment - distributed
		} else {
			amount := totalPayment * entry.Share / totalShares
			distributions[i].Amount = amount
			distributed += amount
		}
	}

	return distributions, nil
}
```

**Step 4: Run tests**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs-go && go test ./revshare/ -count=1 -race`
Expected: All PASS

**Step 5: Commit**

```bash
git add libbitfs-go/revshare/distribute.go libbitfs-go/revshare/revshare_test.go
git commit -m "feat(revshare): implement revenue distribution algorithm"
```

---

### Task 12: Share conservation validation

**Files:**
- Create: `libbitfs-go/revshare/validate.go`
- Modify: `libbitfs-go/revshare/revshare_test.go`

**Step 1: Write failing tests**

Append to `revshare_test.go`:

```go
func TestValidateShareConservation(t *testing.T) {
	nodeID := makeNodeID(0x01)

	// Valid transfer (1:1)
	inputs := []ShareData{{NodeID: nodeID, Amount: 3000}}
	outputs := []ShareData{{NodeID: nodeID, Amount: 3000}}
	assert.NoError(t, ValidateShareConservation(inputs, outputs))

	// Valid split (1:2)
	outputs = []ShareData{
		{NodeID: nodeID, Amount: 2000},
		{NodeID: nodeID, Amount: 1000},
	}
	assert.NoError(t, ValidateShareConservation(inputs, outputs))

	// Valid merge (2:1)
	inputs = []ShareData{
		{NodeID: nodeID, Amount: 2000},
		{NodeID: nodeID, Amount: 1000},
	}
	outputs = []ShareData{{NodeID: nodeID, Amount: 3000}}
	assert.NoError(t, ValidateShareConservation(inputs, outputs))

	// Invalid: shares created
	inputs = []ShareData{{NodeID: nodeID, Amount: 1000}}
	outputs = []ShareData{{NodeID: nodeID, Amount: 2000}}
	assert.ErrorIs(t, ValidateShareConservation(inputs, outputs), ErrShareConservationViolation)

	// Invalid: shares destroyed
	inputs = []ShareData{{NodeID: nodeID, Amount: 2000}}
	outputs = []ShareData{{NodeID: nodeID, Amount: 1000}}
	assert.ErrorIs(t, ValidateShareConservation(inputs, outputs), ErrShareConservationViolation)
}

func TestValidateDistribution(t *testing.T) {
	entries := []RevShareEntry{
		{Address: makeAddr(0xAA), Share: 3000},
		{Address: makeAddr(0xBB), Share: 7000},
	}

	// Valid distribution
	dists := []Distribution{
		{Address: makeAddr(0xAA), Amount: 3000},
		{Address: makeAddr(0xBB), Amount: 7000},
	}
	assert.NoError(t, ValidateDistribution(dists, entries, 10000, 10000))

	// Wrong amount
	dists[0].Amount = 5000
	assert.Error(t, ValidateDistribution(dists, entries, 10000, 10000))
}

func TestRegistryState_FindEntry(t *testing.T) {
	state := &RegistryState{
		Entries: []RevShareEntry{
			{Address: makeAddr(0xAA), Share: 3000},
			{Address: makeAddr(0xBB), Share: 7000},
		},
	}

	idx, entry := state.FindEntry(makeAddr(0xBB))
	assert.Equal(t, 1, idx)
	assert.Equal(t, uint64(7000), entry.Share)

	idx, entry = state.FindEntry(makeAddr(0xCC))
	assert.Equal(t, -1, idx)
	assert.Nil(t, entry)
}
```

**Step 2: Implement validation**

Create `libbitfs-go/revshare/validate.go`:

```go
package revshare

import "fmt"

// ValidateShareConservation checks that total input shares equal total output shares.
func ValidateShareConservation(inputs []ShareData, outputs []ShareData) error {
	var inputTotal, outputTotal uint64
	for _, in := range inputs {
		inputTotal += in.Amount
	}
	for _, out := range outputs {
		outputTotal += out.Amount
	}
	if inputTotal != outputTotal {
		return fmt.Errorf("%w: input=%d output=%d", ErrShareConservationViolation, inputTotal, outputTotal)
	}
	return nil
}

// ValidateDistribution checks that distribution amounts match registry proportions.
func ValidateDistribution(distributions []Distribution, entries []RevShareEntry, totalPayment, totalShares uint64) error {
	if len(distributions) != len(entries) {
		return fmt.Errorf("distribution count %d != entry count %d", len(distributions), len(entries))
	}

	expected, err := DistributeRevenue(totalPayment, entries, totalShares)
	if err != nil {
		return err
	}

	for i := range distributions {
		if distributions[i].Address != expected[i].Address {
			return fmt.Errorf("entry %d: address mismatch", i)
		}
		if distributions[i].Amount != expected[i].Amount {
			return fmt.Errorf("entry %d: amount %d != expected %d", i, distributions[i].Amount, expected[i].Amount)
		}
	}
	return nil
}
```

**Step 3: Run tests**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs-go && go test ./revshare/ -count=1 -race`
Expected: All PASS

**Step 4: Commit**

```bash
git add libbitfs-go/revshare/validate.go libbitfs-go/revshare/revshare_test.go
git commit -m "feat(revshare): add share conservation and distribution validation"
```

---

### Task 13: Full test suite verification

**Files:** None (verification only)

**Step 1: Run complete libbitfs-go test suite**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs-go && go test ./... -count=1 -race -timeout 120s`
Expected: All PASS across all 11 packages

**Step 2: Run linter**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs-go && golangci-lint run ./...`
Expected: 0 issues

**Step 3: Run bitfs test suite for compatibility**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test ./... -count=1 -race -timeout 120s`
Expected: All PASS (no breaking changes)

---

## Phase 4: Access Control Stubs

> **Note:** Phase 4 requires design document updates before implementation. The tasks below create stub types and placeholder functions. Full ACL/group signature implementation is deferred to a separate plan after BLS12-381 design review.

### Task 14: Update design docs for ACL/VersionLog/ShareList

**Files:**
- Modify: `design/bitfs/2-SystemDesign.zh.md`
- Modify: `design/bitfs/3-DetailedDesign.zh.md`

**Step 1: Add Version Log design to 2-SystemDesign.zh.md**

After the version control section (§12), add a subsection:

```markdown
### 版本日志节点 (Version Log)

`version_log` 字段 (field 31, tag 0x1F) 指向一个独立的 Metanet 版本记录节点。

**版本节点结构**:
- 独立 Metanet 节点, P_node 存储在父文件的 version_log 字段
- payload 包含 `prev_version_txid` (32 bytes), 指向前一版本记录节点
- 形成单向链表: 最新 → 前一版本 → ... → 第一版本 (prev_version_txid = 0)

**遍历**: 从 version_log P_node 获取最新版本 → 沿 prev_version_txid 回溯
**创建**: 每次 SelfUpdate 且 version_log 非空时, 自动创建新版本记录节点
**限制**: 不剪枝 (区块链数据不可删除), 客户端可选择只获取最近 N 个版本
```

**Step 2: Add Share List design to 2-SystemDesign.zh.md**

```markdown
### 共享列表节点 (Share List)

`share_list` 字段 (field 32, tag 0x27) 指向一个独立的 Metanet 共享列表节点。

**节点结构**: payload 包含 repeated P2PKH 地址 (20 bytes each)
**查询**: Daemon 检查请求者地址是否在列表中
**更新**: Owner 通过 SelfUpdate 修改列表节点
**与 ACL 的关系**: Share List 是 ACL 的简化版 (无签名验证, 仅地址列表)
```

**Step 3: Update ACL section in 2-SystemDesign.zh.md**

Add implementation notes to the existing ACL section (§22):

```markdown
**实现说明 (Phase 4)**:
- BLS12-381 库待选: github.com/kilic/bls12-381 (纯 Go, 无 CGO) 或 gnark-crypto
- 凭证结构: BLS signature over (member_pubkey, attributes, expiry)
- 凭证颁发: Owner 生成群密钥 → 签发凭证 → 通过 Method 42 ECDH 分发
- 凭证撤销: SelfUpdate 更新 ACL 引用 → 新 Registry 不含被撤销成员
- ACLRef 指向 Metanet 节点, 该节点 payload 包含 group public key + 成员列表
```

**Step 4: Commit**

```bash
git add design/bitfs/2-SystemDesign.zh.md design/bitfs/3-DetailedDesign.zh.md
git commit -m "docs: add Version Log, Share List, and ACL implementation notes"
```

---

### Task 15: Final verification and lint

**Files:** None (verification only)

**Step 1: Run complete test suite with race detector**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs-go && go test ./... -count=1 -race -timeout 120s`
Expected: All packages PASS

**Step 2: Run linter**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs-go && golangci-lint run ./...`
Expected: 0 issues

**Step 3: Run bitfs tests**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test ./... -count=1 -timeout 120s`
Expected: All PASS

**Step 4: Verify line counts**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs-go && find . -name '*.go' ! -name '*_test.go' -newer metanet/node.go | xargs wc -l`
Expected: ~1,200-1,500 new lines of production code

---

## Summary

| Task | Phase | What | Files |
|------|-------|------|-------|
| 1 | P1 | Node fields + tag constants | node.go, parser.go |
| 2 | P1 | Metadata S/D | parser.go, parser_extended_test.go |
| 3 | P1 | All 11 extended fields S/D | parser.go, parser_extended_test.go |
| 4 | P1 | ISOConfig tests + CLTV check | cltv.go, cltv_test.go |
| 5 | P1 | Design doc tag resolution | 2-SystemDesign.zh.md |
| 6 | P2 | Compression (LZW + GZIP) | storage/compress.go |
| 7 | P2 | Content chunking | storage/chunk.go |
| 8 | P2 | Rabin signatures | method42/rabin.go |
| 9 | P3 | revshare types + errors | revshare/types.go, errors.go |
| 10 | P3 | Registry/Share/Pool S/D | revshare/*.go |
| 11 | P3 | Distribution algorithm | revshare/distribute.go |
| 12 | P3 | Validation functions | revshare/validate.go |
| 13 | — | Full suite verification | (no files) |
| 14 | P4 | Design doc updates | design/bitfs/*.md |
| 15 | — | Final verification + lint | (no files) |
