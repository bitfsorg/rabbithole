# AMP (Agent Messaging Protocol) — Design Document

> Date: 2026-03-04
> Status: Approved
> Positioning: Independent protocol spec; BitFS daemon as first implementation

## 1. Overview

AMP is a peer-to-peer encrypted messaging protocol for AI Agent collaboration,
built on BSV blockchain and Paymail identity infrastructure.

**Core principles:**
- Off-chain first, on-chain as offline fallback / archival
- End-to-end encrypted (Method 42 ECDH + AES-256-GCM)
- TLV binary wire format (machine-optimized, no human-readable headers)
- Independent of BitFS — only depends on BSV + Paymail + ECDH

**Dependencies:**
- BSV blockchain (transaction layer)
- Paymail (identity discovery: alias → pubkey + daemon URL)
- ECDH / Method 42 (encryption: shared secret from key pair)

## 2. Protocol Stack

```
┌─────────────────────────────────────────┐
│            Application Layer            │
│  (Task delegation, result exchange...)  │
├─────────────────────────────────────────┤
│           AMP Message Layer             │
│  TLV envelope: content-type + payload   │
│  + optional attachment refs             │
├──────────────────┬──────────────────────┤
│  Online Channel  │  Offline Channel     │
│  HTTP POST to    │  BSV OP_RETURN TX    │
│  /_amp/deliver   │  encrypted w/ ECDH   │
├──────────────────┴──────────────────────┤
│           Encryption Layer              │
│  ECDH(sender_priv, receiver_pub)        │
│  → HKDF-SHA256 → AES-256-GCM           │
│  (= Method 42, key_hash = msg_nonce)    │
├─────────────────────────────────────────┤
│           Identity Layer                │
│  Paymail: agent@domain → P_node pubkey  │
│  + daemon URL via SRV/capability        │
└─────────────────────────────────────────┘
```

Message format is identical across both channels. Only the transport differs.

## 3. Message Format (TLV)

### 3.1 TLV Tag Definitions

| Tag    | Length | Name           | Description                                    |
|--------|--------|----------------|------------------------------------------------|
| `0x01` | 1      | Version        | Protocol version, currently `0x01`             |
| `0x02` | 2      | ContentType    | Message body type (see §3.2)                   |
| `0x03` | var    | Payload        | Message body (encrypted ciphertext)            |
| `0x04` | 12     | Nonce          | AES-256-GCM nonce                              |
| `0x05` | 16     | AuthTag        | AES-256-GCM authentication tag                 |
| `0x06` | 32     | ConversationID | Thread/session ID (SHA256, optional)           |
| `0x07` | 32     | ReplyTo        | Referenced message ID (optional)               |
| `0x08` | var    | AttachmentRef  | BitFS file reference TxID:Vout (optional, repeatable) |

### 3.2 ContentType Enumeration

| Value    | Type               | Purpose                        |
|----------|--------------------|--------------------------------|
| `0x0001` | `text/plain`       | Plain text message             |
| `0x0002` | `application/json` | JSON structured data           |
| `0x0003` | `application/cbor` | CBOR binary structured data    |
| `0x0010` | `amp/task-request`  | Task delegation request        |
| `0x0011` | `amp/task-result`   | Task result response           |
| `0x0012` | `amp/task-status`   | Task progress update           |
| `0x0013` | `amp/ack`           | Message acknowledgement        |
| `0xFF00`+| custom             | Application-defined types      |

### 3.3 Message ID

Message ID is not in the TLV — it comes from the transport layer:
- **Online channel:** daemon-generated UUID, returned in HTTP response
- **Offline channel:** `TxID:Vout` is the message ID

### 3.4 Encryption

Two-layer TLV structure:

```
# Inner TLV (plaintext message)
inner = TLV(version, content_type, payload, [conversation_id], [reply_to], [attachment_refs])

# Encryption
key   = HKDF-SHA256(ECDH(sender_priv, receiver_pub).x, msg_nonce)
ct    = AES-256-GCM(key, nonce, inner)

# Outer TLV (encrypted envelope — wire format)
wire  = TLV(version, nonce, auth_tag, encrypted_blob)
```

Receiver decrypts outer TLV → recovers inner TLV with full message structure.

## 4. Online Channel (HTTP Direct)

### 4.1 Paymail Capability Discovery

AMP extends `.well-known/bsvalias` with new capabilities:

```json
{
  "bsvalias": "1.0",
  "capabilities": {
    "amp-deliver": "https://agent.example.com/_amp/deliver/{alias}",
    "amp-capabilities": "https://agent.example.com/_amp/capabilities/{alias}"
  }
}
```

Sender discovers: (1) receiver's pubkey (PKI endpoint), (2) delivery URL (amp-deliver capability).

### 4.2 Daemon Endpoints (External)

| Method | Path                          | Description                       |
|--------|-------------------------------|-----------------------------------|
| `POST` | `/_amp/deliver/{alias}`       | Deliver encrypted message         |
| `GET`  | `/_amp/capabilities/{alias}`  | Query supported AMP features      |

### 4.3 Delivery Flow

```
Agent A                              Agent B daemon
  │                                       │
  │ 1. Paymail discover B's pubkey + URL  │
  │                                       │
  │ 2. ECDH(A_priv, B_pub) → shared key  │
  │    Encrypt TLV message                │
  │                                       │
  │ 3. POST /_amp/deliver/analyst         │
  │    Body: encrypted TLV                │
  │    Header: X-AMP-Sender: A's pubkey   │
  │──────────────────────────────────────>│
  │                                       │ 4. ECDH(B_priv, A_pub) → same key
  │                                       │    Decrypt, validate, store in inbox
  │                                       │
  │ 5. 200 OK                             │
  │    {msg_id: "uuid", received: true}   │
  │<──────────────────────────────────────│
```

### 4.4 HTTP Request Format

```
POST /_amp/deliver/analyst HTTP/1.1
Host: bob.bitfs.org
Content-Type: application/octet-stream
X-AMP-Version: 1
X-AMP-Sender: 03a1b2c3d4...  (sender compressed pubkey, 33 bytes hex)

[encrypted TLV bytes]
```

### 4.5 HTTP Responses

| Status | Body                                          | Meaning              |
|--------|-----------------------------------------------|----------------------|
| `200`  | `{"msg_id":"uuid","received":true}`           | Delivered            |
| `404`  | `{"error":"unknown_alias"}`                   | Alias not found      |
| `400`  | `{"error":"decryption_failed"}`               | Pubkey mismatch      |
| `429`  | `{"error":"rate_limited","retry_after":30}`   | Rate limited         |

### 4.6 Online Detection

No explicit heartbeat. Simple try-then-fallback:
- **POST succeeds** → online, message delivered
- **Connection refused / timeout** → offline, fall back to on-chain channel

## 5. Offline Channel (On-Chain OP_RETURN)

### 5.1 Transaction Format

```
Input:
  [0] Sender UTXO (signed with sender's key)

Output:
  [0] OP_RETURN <AMP_FLAG> <receiver_P_node> <encrypted_TLV>
  [1] P2PKH → change address
```

**AMP_FLAG:** `0x616d70` (ASCII "amp", 3 bytes) — protocol identifier for on-chain messages.

### 5.2 Field Layout

| Field            | Size  | Description                              |
|------------------|-------|------------------------------------------|
| AMP_FLAG         | 3B    | `0x616d70` protocol identifier           |
| receiver_P_node  | 33B   | Receiver compressed pubkey (scan filter) |
| encrypted_TLV    | var   | Same encrypted TLV as online channel     |

### 5.3 Receiver Scanning

```
1. Monitor mempool + new blocks
2. Filter: OP_RETURN first pushdata == 0x616d70 (AMP_FLAG)
3. Filter: second pushdata == own P_node
4. Extract encrypted_TLV
5. Recover sender pubkey from input[0] scriptSig
6. ECDH(self_priv, sender_pub) → decrypt
7. Store in inbox, message ID = TxID:0
```

Sender pubkey recovery: BSV P2PKH unlock script `<sig> <pubkey>` — no extra identity field needed.

### 5.4 Cost Estimate

```
OP_RETURN data: 3 (flag) + 33 (pubkey) + ~200 (encrypted TLV) ≈ 236 bytes
Total tx size:  ~400 bytes
Fee:            ~400 sats (1 sat/byte) ≈ $0.0001
```

### 5.5 Channel Comparison

|                  | Online             | Offline                         |
|------------------|--------------------|---------------------------------|
| Transport        | HTTP POST          | BSV TX OP_RETURN                |
| Latency          | Milliseconds       | Seconds (mempool) to minutes    |
| Cost             | Free               | ~400 sats/message               |
| Persistence      | Daemon local store | On-chain permanent              |
| Message ID       | UUID               | TxID:Vout                       |
| Sender identity  | X-AMP-Sender header| input scriptSig pubkey          |

## 6. Inbox & Local API

### 6.1 Storage Layout

```
~/.bitfs/amp/
├── inbox/
│   ├── {msg_id}.amp        # Decrypted TLV message + metadata
│   └── ...
├── outbox/
│   ├── {msg_id}.amp        # Sent message copies
│   └── ...
└── conversations/
    └── {conversation_id}/  # Per-conversation index
```

### 6.2 Message Metadata

```go
type Message struct {
    ID             string    // UUID (online) or TxID:Vout (onchain)
    SenderPubKey   []byte    // 33-byte compressed pubkey
    ReceivedAt     time.Time // Local receive time
    Channel        string    // "online" or "onchain"
    ContentType    uint16    // TLV content type
    Payload        []byte    // Decrypted message body
    ConversationID []byte    // Optional, 32 bytes
    ReplyTo        string    // Optional, referenced message ID
    Attachments    []string  // Optional, BitFS TxID:Vout list
    Read           bool      // Read status
}
```

### 6.3 Local API Endpoints (localhost only)

| Method   | Path                          | Description                          |
|----------|-------------------------------|--------------------------------------|
| `GET`    | `/_amp/inbox`                 | List inbox (supports ?unread=true)   |
| `GET`    | `/_amp/inbox/{msg_id}`        | Read single message                  |
| `POST`   | `/_amp/send`                  | Send message (daemon picks channel)  |
| `GET`    | `/_amp/conversations`         | List conversations                   |
| `GET`    | `/_amp/conversations/{id}`    | Get conversation messages            |
| `DELETE` | `/_amp/inbox/{msg_id}`        | Delete message                       |

### 6.4 Send API

```json
POST /_amp/send
{
  "to": "analyst@bob.bitfs.org",
  "content_type": "amp/task-request",
  "payload": {"task": "analyze", "data_ref": "txid:0"},
  "conversation_id": "optional-hex",
  "attachments": ["txid1:0", "txid2:1"],
  "prefer": "auto"
}
```

`prefer` values:
- `"auto"` (default): try online first, fall back to on-chain
- `"online"`: online only, fail if unreachable
- `"onchain"`: force on-chain delivery

## 7. Application Layer Message Types

### 7.1 Task Request (`0x0010`)

```json
{
  "task_id": "uuid",
  "action": "analyze_dataset",
  "params": {"format": "csv", "columns": ["price", "volume"]},
  "deadline": 1709568000,
  "data_refs": ["txid:0"]
}
```

### 7.2 Task Result (`0x0011`)

```json
{
  "task_id": "uuid",
  "status": "completed",
  "result": {"summary": "...", "confidence": 0.95},
  "output_refs": ["txid:1"]
}
```

### 7.3 Task Status (`0x0012`)

```json
{
  "task_id": "uuid",
  "status": "in_progress",
  "progress": 0.7,
  "eta": 1709567000
}
```

### 7.4 Ack (`0x0013`)

```json
{
  "ref_msg_id": "uuid-or-txid:0"
}
```

Applications may define custom types in the `0xFF00`+ range.

## 8. Security

### 8.1 End-to-End Encryption

All messages (online and offline) use ECDH + AES-256-GCM. No intermediary
(CDN, ISP, miners) can read message content.

### 8.2 Authentication

- **Online:** `X-AMP-Sender` pubkey. Optional ECDH challenge for strict verification.
- **Offline:** BSV transaction signature is identity proof (input scriptSig contains pubkey).

### 8.3 Anti-Replay

- **Online:** daemon tracks processed msg_ids, rejects duplicates.
- **Offline:** TxID is naturally unique; UTXO can only be spent once.

### 8.4 Anti-Spam

- **Online:** IP rate limiting (reuse daemon's existing rate limiter).
- **Offline:** each on-chain message costs real sats — natural spam resistance.
- **Optional:** receiver configures pubkey whitelist (accept messages from known keys only).

### 8.5 Key Management

Each vault's P_node serves as AMP identity key. No new key infrastructure needed —
fully reuses the HD wallet system.

## 9. Limitations (V1)

| Limitation        | Notes                                                    |
|-------------------|----------------------------------------------------------|
| Point-to-point    | V1 supports 1:1 only, no group/broadcast                |
| Message size      | Online: 1MB HTTP body. Offline: ~100KB OP_RETURN         |
| No read receipts  | Sender doesn't know if read (simulate via ack messages)  |
| No message recall | On-chain messages are permanent                          |
| No routing        | No multi-hop relay, direct connection or on-chain only   |

## 10. Future Extensions (V2+)

- **Group messaging:** multi-party ECDH or group key distribution
- **Paid messages:** combine with HTLC, receiver pays to unlock content (consulting)
- **Metanet relay:** CDN nodes as message relays (NAT traversal)
- **Agent marketplace:** agents publish capability catalogs, others invoke and pay
- **Subscriptions/Webhooks:** event subscriptions (file change notifications, etc.)

## 11. Analogies

| AMP                  | Traditional Email     | Difference                              |
|----------------------|-----------------------|-----------------------------------------|
| Paymail alias        | Email address         | Bound to crypto pubkey, not just address|
| `/_amp/deliver`      | SMTP                  | Direct delivery, no relay MTA           |
| On-chain OP_RETURN   | Mail server storage   | Permanent, tamper-proof, trustless      |
| `/_amp/inbox`        | POP3/IMAP             | Local query, daemon is your mail server |
| Method 42 ECDH       | PGP/S-MIME            | Built-in encryption, not optional plugin|
| TLV binary           | MIME                   | Compact, machine-optimized              |
