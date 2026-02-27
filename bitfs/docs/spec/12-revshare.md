# 模块规范：libbitfs-go/revshare

## 目的

BitFS 的收入分成系统。允许文件所有者将付费内容的检索收入分配给多个股东。支持初始份额发行 (ISO)、份额转让、以及收入自动分配。

## 公共 API

### 类型

```go
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
func (s *RegistryState) IsISOActive() bool

// IsLocked returns true if share transfers are locked.
func (s *RegistryState) IsLocked() bool

// FindEntry returns the index and entry for the given address, or -1 if not found.
func (s *RegistryState) FindEntry(addr [20]byte) (int, *RevShareEntry)

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

### 函数

```go
// --- Registry Serialization ---

// SerializeRegistry serializes a RegistryState to binary format.
func SerializeRegistry(state *RegistryState) ([]byte, error)

// DeserializeRegistry deserializes binary data into a RegistryState.
func DeserializeRegistry(data []byte) (*RegistryState, error)

// --- Share Serialization ---

// SerializeShare encodes ShareData to binary format.
func SerializeShare(data *ShareData) []byte

// DeserializeShare decodes binary data into ShareData.
func DeserializeShare(data []byte) (*ShareData, error)

// --- ISO Pool Serialization ---

// SerializeISOPool encodes ISOPoolState to binary format.
func SerializeISOPool(state *ISOPoolState) []byte

// DeserializeISOPool decodes binary data into ISOPoolState.
func DeserializeISOPool(data []byte) (*ISOPoolState, error)

// --- Revenue Distribution ---

// DistributeRevenue calculates per-shareholder payouts.
// The last entry gets the remainder to avoid integer division precision loss.
func DistributeRevenue(totalPayment uint64, entries []RevShareEntry, totalShares uint64) ([]Distribution, error)

// --- Validation ---

// ValidateShareConservation checks that total input shares equal total output shares.
func ValidateShareConservation(inputs []ShareData, outputs []ShareData) error

// ValidateDistribution checks that distribution amounts match registry proportions.
func ValidateDistribution(distributions []Distribution, entries []RevShareEntry, totalPayment, totalShares uint64) error
```

## 依赖

- `encoding/binary` -- 大端序序列化
- `fmt` -- 错误包装

## 数据结构

### Registry UTXO 二进制格式

固定布局，所有多字节字段使用 **大端序** (Big-Endian)：

```
Header (44 bytes):
  NodeID       [32]byte  // SHA256(P_node || TxID)
  TotalShares  uint64    // 总份额数
  NumEntries   uint32    // 股东数量

Entries (28 bytes × NumEntries):
  Address      [20]byte  // P2PKH 地址哈希
  Share        uint64    // 持有份额数

Trailer (1 byte):
  ModeFlags    uint8     // bit 0: ISO active, bit 1: locked
```

最小大小: 44 + 0×28 + 1 = 45 bytes (无股东)
典型大小: 44 + N×28 + 1 bytes

### Share UTXO 二进制格式

固定 40 bytes:
```
NodeID  [32]byte  // 绑定的 Metanet 节点
Amount  uint64    // 份额数量
```

### ISO Pool UTXO 二进制格式

固定 68 bytes:
```
NodeID          [32]byte  // 绑定的 Metanet 节点
RemainingShares uint64    // 未售份额
PricePerShare   uint64    // 每份价格 (satoshis)
CreatorAddr     [20]byte  // 创建者 P2PKH 地址
```

### 收入分配算法

```
DistributeRevenue(totalPayment, entries, totalShares):
  distributed = 0
  for i = 0 to len(entries)-2:
    amount[i] = totalPayment × entries[i].Share / totalShares
    distributed += amount[i]
  amount[last] = totalPayment - distributed  // 余数归末位
  return amounts
```

保证: sum(amounts) == totalPayment (无精度损失)。

### ModeFlags 位定义

| Bit | 名称 | 说明 |
|-----|------|------|
| 0 | ISO_ACTIVE | ISO 池处于活跃状态 |
| 1 | LOCKED | 份额转让被锁定 |

### ISO 生命周期

```
None → Open (创建 ISO 池, 设定总份额和价格)
Open → Partial (首笔购买后)
Partial → Closed (份额售罄 或 创建者手动关闭)
Closed → (终态, 不可回滚)
```

## 错误处理

| 错误 | 条件 |
|------|------|
| `ErrInvalidRegistryData` | Registry UTXO 数据格式错误或截断 |
| `ErrInvalidShareData` | Share UTXO 数据不是 40 字节 |
| `ErrInvalidISOPoolData` | ISO Pool UTXO 数据不是 68 字节 |
| `ErrShareConservationViolation` | 输入份额总和 ≠ 输出份额总和 |
| `ErrInsufficientPayment` | totalPayment == 0 |
| `ErrNoEntries` | 注册表无股东 |
| `ErrZeroShares` | 份额数量为零 |
| `ErrZeroTotalShares` | 总份额为零 |
| `ErrEntryNotFound` | 地址不在注册表中 |
| `ErrRegistryLocked` | 注册表已锁定，拒绝份额转让 |

## 安全考量

1. **份额守恒**: ValidateShareConservation 确保份额不被凭空创造或销毁。输入总和必须等于输出总和。
2. **余数归末位**: 整数除法余数给最后一个股东，避免精度损失。最大误差: len(entries)-1 satoshis。
3. **锁定机制**: LOCKED 标志防止 ISO 进行中的份额转让，确保 ISO 池的份额计数一致。
4. **大端序**: 选择大端序 (网络字节序) 以便跨平台序列化一致性。
