package rttensor

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"math"
	"os"
	"sort"

	"aether/assets/rt"
)

// safetensors 格式（Hugging Face 等发布模型时使用）：8 字节的头部长度（小端），JSON 头部
// （每个张量的类型、形状、在数据区中的位置），然后是所有张量的原始数据。

type stEntry struct {
	DType   string   `json:"dtype"`
	Shape   []int    `json:"shape"`
	Offsets [2]int64 `json:"data_offsets"`
}

func readSafetensorsHeader(f *os.File, path string) (map[string]stEntry, int64) {
	var n uint64
	if err := binary.Read(f, binary.LittleEndian, &n); err != nil {
		rt.Raise("ValueError", "%s 不是有效的 safetensors 文件：%v", path, err)
	}
	if n == 0 || n > 100<<20 {
		rt.Raise("ValueError", "%s 不是有效的 safetensors 文件（头部长度 %d）", path, n)
	}
	head := make([]byte, n)
	if _, err := io.ReadFull(f, head); err != nil {
		rt.Raise("ValueError", "%s 的头部不完整：%v", path, err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(head, &raw); err != nil {
		rt.Raise("ValueError", "%s 的头部不是有效的 JSON：%v", path, err)
	}
	out := map[string]stEntry{}
	for name, msg := range raw {
		if name == "__metadata__" {
			continue
		}
		var e stEntry
		if err := json.Unmarshal(msg, &e); err != nil {
			rt.Raise("ValueError", "%s：张量 %s 的描述无效：%v", path, name, err)
		}
		out[name] = e
	}
	return out, 8 + int64(n)
}

// SafetensorsShapes 返回文件中每个张量的形状（不读取数据），按名字排序。
func SafetensorsShapes(path string) *rt.Dict[string, *rt.List[int]] {
	f, err := os.Open(path)
	if err != nil {
		rt.Raise("FileNotFoundError", "无法打开 %s：%v", path, err)
	}
	defer f.Close()
	entries, _ := readSafetensorsHeader(f, path)
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	d := rt.NewDict[string, *rt.List[int]](len(names))
	for _, name := range names {
		d.Set(name, &rt.List[int]{S: append([]int(nil), entries[name].Shape...)})
	}
	return d
}

// LoadSafetensors 读取 safetensors 文件中的张量，转换为 float32，放在默认设备上。
// names 给出时只读取这些张量（例如跳过与词嵌入共享的 lm_head）。支持 BF16、F16、F32。
func LoadSafetensors(path string, names rt.Opt[*rt.List[string]]) *rt.Dict[string, *Tensor] {
	f, err := os.Open(path)
	if err != nil {
		rt.Raise("FileNotFoundError", "无法打开 %s：%v", path, err)
	}
	defer f.Close()
	entries, base := readSafetensorsHeader(f, path)
	var want []string
	if names.Ok {
		for _, n := range names.V.S {
			if _, ok := entries[n]; !ok {
				rt.Raise("KeyError", "%s 中没有张量 %s", path, n)
			}
			want = append(want, n)
		}
	} else {
		for n := range entries {
			want = append(want, n)
		}
		sort.Strings(want)
	}
	out := rt.NewDict[string, *Tensor](len(want))
	for _, name := range want {
		e := entries[name]
		count := numel(e.Shape)
		size := e.Offsets[1] - e.Offsets[0]
		var elem int64
		switch e.DType {
		case "BF16", "F16":
			elem = 2
		case "F32":
			elem = 4
		default:
			rt.Raise("ValueError", "%s：张量 %s 的类型 %s 不受支持（支持 BF16、F16、F32）", path, name, e.DType)
		}
		if size != int64(count)*elem || size < 0 {
			rt.Raise("ValueError", "%s：张量 %s 的数据长度与形状不一致", path, name)
		}
		buf := make([]byte, size)
		if _, err := f.ReadAt(buf, base+e.Offsets[0]); err != nil {
			rt.Raise("IOError", "读取 %s 中的 %s 失败：%v", path, name, err)
		}
		data := cpuBuffer(count)
		switch e.DType {
		case "BF16":
			for i := range data {
				data[i] = math.Float32frombits(uint32(binary.LittleEndian.Uint16(buf[2*i:])) << 16)
			}
		case "F16":
			for i := range data {
				data[i] = halfToFloat(binary.LittleEndian.Uint16(buf[2*i:]))
			}
		case "F32":
			for i := range data {
				data[i] = math.Float32frombits(binary.LittleEndian.Uint32(buf[4*i:]))
			}
		}
		var t *Tensor
		if defaultDev == cpuDev {
			t = &Tensor{shape: append([]int(nil), e.Shape...), n: count, st: &storage{dev: cpuDev, cpu: data}}
		} else {
			t = fromHost(defaultDev, e.Shape, data)
		}
		out.Set(name, t)
	}
	return out
}

// halfToFloat 把 IEEE 754 半精度数转换为 float32。
func halfToFloat(h uint16) float32 {
	sign := uint32(h>>15) << 31
	exp := uint32(h>>10) & 0x1f
	mant := uint32(h) & 0x3ff
	switch exp {
	case 0:
		// 零与非规格化数：mant × 2^-24
		v := float32(mant) * (1.0 / 16777216.0)
		if sign != 0 {
			v = -v
		}
		return v
	case 0x1f:
		return math.Float32frombits(sign | 0xff<<23 | mant<<13)
	}
	return math.Float32frombits(sign | (exp+112)<<23 | mant<<13)
}
