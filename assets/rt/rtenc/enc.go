// Package rtenc 实现 Aether 的 base64 与 hashlib 模块（只在用到时链接）。
package rtenc

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"hash"
	"strings"

	"aether/assets/rt"
)

// ===== base64 =====

func decode(enc *base64.Encoding, s string) []byte {
	s = strings.TrimSpace(s)
	b, err := enc.DecodeString(s)
	if err != nil {
		// 也接受省略了末尾 = 的写法
		if b2, err2 := enc.WithPadding(base64.NoPadding).DecodeString(strings.TrimRight(s, "=")); err2 == nil {
			return b2
		}
		if pos, ok := err.(base64.CorruptInputError); ok {
			rt.Raise("ValueError", "无效的 Base64 数据（第 %d 个字符附近）", int64(pos)+1)
		}
		rt.Raise("ValueError", "无效的 Base64 数据：%v", err)
	}
	return b
}

func B64Encode(s string) string    { return base64.StdEncoding.EncodeToString([]byte(s)) }
func B64Decode(s string) string    { return string(decode(base64.StdEncoding, s)) }
func B64URLEncode(s string) string { return base64.URLEncoding.EncodeToString([]byte(s)) }
func B64URLDecode(s string) string { return string(decode(base64.URLEncoding, s)) }

func B64EncodeBytes(data *rt.List[uint8]) string {
	return base64.StdEncoding.EncodeToString(data.S)
}

func B64DecodeBytes(s string) *rt.List[uint8] {
	return &rt.List[uint8]{S: decode(base64.StdEncoding, s)}
}

func B16Encode(s string) string { return strings.ToUpper(hex.EncodeToString([]byte(s))) }

func B16Decode(s string) string {
	b, err := hex.DecodeString(strings.TrimSpace(s))
	if err != nil {
		rt.Raise("ValueError", "无效的十六进制数据：%s", rt.QuoteStr(s))
	}
	return string(b)
}

// ===== hashlib =====

// Hash 是一个正在计算的摘要（对应 Python 的 hashlib 对象）。
type Hash struct {
	h    hash.Hash
	name string
}

func newHash(name string, h hash.Hash, data string) *Hash {
	x := &Hash{h: h, name: name}
	x.h.Write([]byte(data))
	return x
}

func NewMD5(data string) *Hash    { return newHash("md5", md5.New(), data) }
func NewSHA1(data string) *Hash   { return newHash("sha1", sha1.New(), data) }
func NewSHA256(data string) *Hash { return newHash("sha256", sha256.New(), data) }
func NewSHA512(data string) *Hash { return newHash("sha512", sha512.New(), data) }

// NewHash 按名字创建摘要：md5、sha1、sha224、sha256、sha384、sha512。
func NewHash(name, data string) *Hash {
	switch strings.ToLower(strings.ReplaceAll(name, "-", "")) {
	case "md5":
		return NewMD5(data)
	case "sha1":
		return NewSHA1(data)
	case "sha224":
		return newHash("sha224", sha256.New224(), data)
	case "sha256":
		return NewSHA256(data)
	case "sha384":
		return newHash("sha384", sha512.New384(), data)
	case "sha512":
		return NewSHA512(data)
	}
	rt.Raise("ValueError", "不支持的摘要算法：%s", name)
	return nil
}

func (x *Hash) Update(data string)               { x.h.Write([]byte(data)) }
func (x *Hash) UpdateBytes(data *rt.List[uint8]) { x.h.Write(data.S) }
func (x *Hash) HexDigest() string                { return hex.EncodeToString(x.h.Sum(nil)) }
func (x *Hash) Digest() *rt.List[uint8]          { return &rt.List[uint8]{S: x.h.Sum(nil)} }
func (x *Hash) Name() string                     { return x.name }
func (x *Hash) Size() int                        { return x.h.Size() }
func (x *Hash) String() string                   { return "<" + x.name + " 摘要>" }
