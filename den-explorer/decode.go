package main

import (
	"encoding/hex"
	"fmt"

	"github.com/bsv-blockchain/go-sdk/transaction"
	"github.com/bitfsorg/libbitfs-go/metanet"
	libtx "github.com/bitfsorg/libbitfs-go/tx"
)

// DecodedMetanet holds parsed Metanet data from a transaction.
type DecodedMetanet struct {
	IsMetanet   bool
	PNode       string // hex-encoded compressed pubkey
	ParentTxID  string // hex-encoded (empty for root)
	IsRoot      bool
	Node        *metanet.Node
	RawPayload  string // hex-encoded TLV payload
	Error       string // parsing error if any
}

// TLVField represents a single decoded TLV field for display.
type TLVField struct {
	Tag      byte
	TagName  string
	Length   int
	ValueHex string
	ValueStr string // human-readable interpretation
}

// tlvTagNames maps TLV tags to human-readable names.
var tlvTagNames = map[byte]string{
	0x01: "Version",
	0x02: "Type",
	0x03: "Operation",
	0x04: "MimeType",
	0x05: "FileSize",
	0x06: "KeyHash",
	0x07: "Access",
	0x08: "PricePerKB",
	0x09: "LinkTarget",
	0x0A: "LinkType",
	0x0B: "Timestamp",
	0x0C: "Parent",
	0x0D: "Index",
	0x0E: "ChildEntry",
	0x0F: "NextChildIndex",
	0x10: "Domain",
	0x11: "Keywords",
	0x12: "Description",
	0x13: "Encrypted",
	0x14: "OnChain",
	0x15: "ContentTxID",
	0x16: "Compression",
	0x17: "CltvHeight",
	0x18: "RevenueShare",
	0x19: "NetworkName",
	0x1A: "MerkleRoot",
}

// DecodeMetanetTx attempts to parse a raw transaction and extract Metanet data.
// rawTxBytes is the serialized transaction.
func DecodeMetanetTx(rawTxBytes []byte) *DecodedMetanet {
	result := &DecodedMetanet{}

	sdkTx, err := transaction.NewTransactionFromBytes(rawTxBytes)
	if err != nil {
		result.Error = fmt.Sprintf("parse tx: %v", err)
		return result
	}

	// Find OP_RETURN output (typically output 0)
	pushes := extractOPReturnPushes(sdkTx)
	if pushes == nil {
		return result // not a Metanet tx
	}

	// Check MetaFlag
	if len(pushes) < 4 {
		return result
	}
	if string(pushes[0]) != libtx.MetaFlag {
		return result
	}

	result.IsMetanet = true
	result.PNode = hex.EncodeToString(pushes[1])
	result.ParentTxID = hex.EncodeToString(pushes[2])
	result.IsRoot = len(pushes[2]) == 0
	result.RawPayload = hex.EncodeToString(pushes[3])

	// Parse into full Node
	node, err := metanet.ParseNode(pushes)
	if err != nil {
		result.Error = fmt.Sprintf("parse metanet: %v", err)
		return result
	}
	result.Node = node
	return result
}

// DecodeTLVFields parses raw TLV payload bytes into individual fields for display.
func DecodeTLVFields(payloadHex string) []TLVField {
	data, err := hex.DecodeString(payloadHex)
	if err != nil {
		return nil
	}

	var fields []TLVField
	pos := 0
	for pos+3 <= len(data) {
		tag := data[pos]
		if pos+3 > len(data) {
			break
		}
		length := int(data[pos+1]) | int(data[pos+2])<<8
		pos += 3

		if pos+length > len(data) {
			break
		}
		value := data[pos : pos+length]
		pos += length

		name := tlvTagNames[tag]
		if name == "" {
			name = fmt.Sprintf("Unknown(0x%02X)", tag)
		}

		field := TLVField{
			Tag:      tag,
			TagName:  name,
			Length:   length,
			ValueHex: hex.EncodeToString(value),
			ValueStr: interpretTLVValue(tag, value),
		}
		fields = append(fields, field)
	}
	return fields
}

// interpretTLVValue returns a human-readable string for common TLV fields.
func interpretTLVValue(tag byte, value []byte) string {
	switch tag {
	case 0x01: // Version
		if len(value) == 4 {
			return fmt.Sprintf("%d", uint32(value[0])|uint32(value[1])<<8|uint32(value[2])<<16|uint32(value[3])<<24)
		}
	case 0x02: // Type
		if len(value) == 4 {
			v := int32(value[0]) | int32(value[1])<<8 | int32(value[2])<<16 | int32(value[3])<<24
			return metanet.NodeType(v).String()
		}
	case 0x03: // Op
		if len(value) == 4 {
			v := int32(value[0]) | int32(value[1])<<8 | int32(value[2])<<16 | int32(value[3])<<24
			return metanet.OpType(v).String()
		}
	case 0x04: // MimeType
		return string(value)
	case 0x05: // FileSize
		if len(value) == 8 {
			v := uint64(0)
			for i := 0; i < 8; i++ {
				v |= uint64(value[i]) << (i * 8)
			}
			return fmt.Sprintf("%d bytes", v)
		}
	case 0x07: // Access
		if len(value) == 4 {
			v := int32(value[0]) | int32(value[1])<<8 | int32(value[2])<<16 | int32(value[3])<<24
			switch metanet.AccessLevel(v) {
			case metanet.AccessPrivate:
				return "PRIVATE"
			case metanet.AccessFree:
				return "FREE"
			case metanet.AccessPaid:
				return "PAID"
			}
		}
	case 0x10: // Domain
		return string(value)
	case 0x11: // Keywords
		return string(value)
	case 0x12: // Description
		return string(value)
	case 0x19: // NetworkName
		return string(value)
	}
	return ""
}

// extractOPReturnPushes finds and extracts OP_RETURN data pushes from a transaction.
// It parses the raw script bytes directly because the go-sdk Chunks() method
// bundles all data after OP_RETURN into a single chunk.
func extractOPReturnPushes(sdkTx *transaction.Transaction) [][]byte {
	for _, out := range sdkTx.Outputs {
		raw := []byte(*out.LockingScript)
		if len(raw) < 2 {
			continue
		}

		// Find OP_RETURN start position.
		var pos int
		if raw[0] == 0x00 && raw[1] == 0x6a {
			pos = 2 // OP_FALSE OP_RETURN
		} else if raw[0] == 0x6a {
			pos = 1 // OP_RETURN
		} else {
			continue
		}

		// Parse push data elements from the remaining bytes.
		var pushes [][]byte
		for pos < len(raw) {
			opcode := raw[pos]
			pos++

			var dataLen int
			switch {
			case opcode == 0x00:
				pushes = append(pushes, []byte{})
				continue
			case opcode >= 0x01 && opcode <= 0x4b:
				dataLen = int(opcode)
			case opcode == 0x4c: // OP_PUSHDATA1
				if pos >= len(raw) {
					return pushes
				}
				dataLen = int(raw[pos])
				pos++
			case opcode == 0x4d: // OP_PUSHDATA2
				if pos+2 > len(raw) {
					return pushes
				}
				dataLen = int(raw[pos]) | int(raw[pos+1])<<8
				pos += 2
			case opcode == 0x4e: // OP_PUSHDATA4
				if pos+4 > len(raw) {
					return pushes
				}
				dataLen = int(raw[pos]) | int(raw[pos+1])<<8 | int(raw[pos+2])<<16 | int(raw[pos+3])<<24
				pos += 4
			default:
				return pushes // non-push opcode, stop
			}

			if pos+dataLen > len(raw) {
				return pushes
			}
			data := make([]byte, dataLen)
			copy(data, raw[pos:pos+dataLen])
			pushes = append(pushes, data)
			pos += dataLen
		}
		return pushes
	}
	return nil
}
