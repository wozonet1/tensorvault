package core

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"tensorvault/pkg/types"

	"github.com/fxamacker/cbor/v2"
)

// 定义符合 DAG-CBOR 规范的编码选项
var encOptions = cbor.EncOptions{
	// 1. 强制 Map Key 排序 (Canonical)
	// 保证相同的对象生成唯一的 Hash
	Sort: cbor.SortCanonical,

	//2.浮点数必须使用64位表示
	ShortestFloat: cbor.ShortestFloatNone,
	// 3. 时间格式化为 Unix 整数
	// 禁止自动生成 Tag 0/1 (RFC 3339 字符串)，那是 DAG-CBOR 不推荐的
	Time:    cbor.TimeUnix,
	TimeTag: cbor.EncTagNone,

	// 4. 禁止不定长编码 (Indefinite Length)
	// IPLD 要求数组和 Map 必须在头部声明长度
	IndefLength: cbor.IndefLengthForbidden,

	// 5. (可选) 大整数处理
	// 即使我们目前只用 int64，显式设置这个也是好习惯
	// 确保如果未来用了 big.Int，也会用最短编码
	BigIntConvert: cbor.BigIntConvertShortest,
}

// 全局复用的编码模式
var em, _ = encOptions.EncMode()

// 定义符合 DAG-CBOR 规范的解码选项
var decOptions = cbor.DecOptions{
	// --- 安全性配置 (防 DoS 攻击) ---
	// 限制容器元素数量和嵌套深度，防止恶意构造的巨大头部耗尽内存或栈
	MaxArrayElements: 10000,
	MaxMapPairs:      10000,
	MaxNestedLevels:  100,

	// --- 规范性配置 (DAG-CBOR Strictness) ---
	// 禁止不定长编码 (Indefinite Length)
	IndefLength: cbor.IndefLengthForbidden,

	// 强制检查 Map Key 重复 (DAG-CBOR 不允许重复 Key)
	DupMapKey: cbor.DupMapKeyEnforcedAPF,

	// 禁止自动解析 Bignum Tag (Tag 2/3) -> 必须手动处理或拒绝
	BignumTag: cbor.BignumTagForbidden,

	// 忽略时间 Tag (Tag 0/1)，强制解析为数字或字符串，由 Struct 类型决定
	TimeTag: cbor.DecTagIgnored,
}

// 导出 dm 供包内部使用 (如 link.go)
var dm, _ = decOptions.DecMode()

// CalculateHash 计算对象的 Hash (CID) 和序列化数据
func CalculateHash(v any) (types.Hash, []byte, error) {
	data, err := em.Marshal(v)
	if err != nil {
		return "", nil, fmt.Errorf("failed to marshal object: %w", err)
	}

	// 计算 SHA-256
	hashBytes := sha256.Sum256(data)
	hashStr := hex.EncodeToString(hashBytes[:])

	return types.Hash(hashStr), data, nil
}

// CalculateBlobHash 计算原始数据块的 Hash
func CalculateBlobHash(data []byte) types.Hash {
	hashBytes := sha256.Sum256(data)
	return types.Hash(hex.EncodeToString(hashBytes[:]))
}

// DecodeObject 通用的解码函数 (供外部使用)
func DecodeObject(data []byte, v any) error {
	return dm.Unmarshal(data, v) // 使用 dm 解码
}
