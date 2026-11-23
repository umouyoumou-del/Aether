package rttensor

import (
	"image"
	"image/color"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"

	"aether/assets/rt"
)

// ===== gather、topk、sort =====

// lastDimView 把第 dim 维移到最后（需要时复制），返回结果与“是否移动过”。
func (x *Tensor) lastDimView(dim int, what string) (*Tensor, int) {
	d := normDim(dim, len(x.shape), what)
	if d == len(x.shape)-1 {
		return x, d
	}
	return x.Transpose(d, -1), d
}

// Gather 实现 x.gather(dim, index)：out[..., j, ...] = x[..., index[..., j, ...], ...]，
// index 的形状除第 dim 维外与 x 相同。对 x 可以求导。
func (x *Tensor) Gather(dim int, index *Tensor) *Tensor {
	if len(index.shape) != len(x.shape) {
		raiseShape("gather：index 的维数（%d）应与输入（%d）相同", len(index.shape), len(x.shape))
	}
	xt, d := x.lastDimView(dim, "gather ")
	it := index
	if d != len(x.shape)-1 {
		it = index.Transpose(d, -1)
	}
	cols, k := xt.shape[len(xt.shape)-1], it.shape[len(it.shape)-1]
	rows := xt.n / max(cols, 1)
	if it.n/max(k, 1) != rows || !sameShape(xt.shape[:len(xt.shape)-1], it.shape[:len(it.shape)-1]) {
		raiseShape("gather：index 的形状 %s 与输入的形状 %s 不一致", shapeStr(index.shape), shapeStr(x.shape))
	}
	// 换算为展平后的下标，再用 embedding 取出（反向时把梯度加回对应的位置）
	h := it.host()
	flat := make([]float32, len(h))
	for i, v := range h {
		c := int(v)
		if c < 0 || c >= cols || float32(c) != v {
			rt.Raise("IndexError", "gather：下标 %v 超出范围（长度为 %d）", v, cols)
		}
		flat[i] = float32((i/k)*cols + c)
	}
	out := Embedding(xt.reshape([]int{xt.n, 1}), fromHost(xt.dev(), []int{len(flat)}, flat)).reshape(it.shape)
	if d != len(x.shape)-1 {
		out = out.Transpose(d, -1)
	}
	return out
}

// sortedIndex 返回每一行（最后一维）排序后的下标。
func sortedIndex(x *Tensor, descending bool) ([]int, int, int) {
	if len(x.shape) == 0 {
		raiseShape("不能对零维张量排序")
	}
	cols := x.shape[len(x.shape)-1]
	rows := x.n / max(cols, 1)
	h := x.host()
	idx := make([]int, x.n)
	keys := make([]float32, cols)
	for r := 0; r < rows; r++ {
		row := idx[r*cols : (r+1)*cols]
		for j := range row {
			row[j] = j
			keys[j] = h[r*cols+j]
			if descending {
				keys[j] = -keys[j]
			}
		}
		sortByKey(row, keys)
	}
	return idx, rows, cols
}

// Sort 沿最后一维排序，返回 (排序后的值, 原来的下标)；值可以求导。
func (x *Tensor) Sort(descending bool) rt.T2[*Tensor, *Tensor] {
	idx, _, _ := sortedIndex(x, descending)
	d := make([]float32, len(idx))
	for i, v := range idx {
		d[i] = float32(v)
	}
	it := fromHost(x.dev(), x.shape, d)
	return rt.T2[*Tensor, *Tensor]{V0: x.Gather(-1, it), V1: it}
}

// Argsort 返回沿最后一维排序后的下标。
func (x *Tensor) Argsort(descending bool) *Tensor { return x.Sort(descending).V1 }

// Topk 返回最后一维上最大（largest 为假时最小）的 k 个值及其下标（按值排序）。
func (x *Tensor) Topk(k int, largest bool) rt.T2[*Tensor, *Tensor] {
	idx, rows, cols := sortedIndex(x, largest)
	if k < 0 || k > cols {
		rt.Raise("ValueError", "topk：k=%d 超出范围（最后一维的长度为 %d）", k, cols)
	}
	d := make([]float32, rows*k)
	for r := 0; r < rows; r++ {
		for j := 0; j < k; j++ {
			d[r*k+j] = float32(idx[r*cols+j])
		}
	}
	shape := append(append([]int(nil), x.shape[:len(x.shape)-1]...), k)
	it := fromHost(x.dev(), shape, d)
	return rt.T2[*Tensor, *Tensor]{V0: x.Gather(-1, it), V1: it}
}

// ===== 图像 =====

// LoadImage 读取 PNG、JPEG 或 GIF 图像，返回 [通道, 高, 宽] 的张量，数值在 [0, 1] 中
// （gray 为真时是 1 个通道的灰度图，否则是 RGB 3 个通道）。
func LoadImage(path string, gray bool) *Tensor {
	f, err := os.Open(path)
	if err != nil {
		rt.Raise("FileNotFoundError", "无法打开图像 %s：%v", path, err)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		rt.Raise("ValueError", "无法解码图像 %s：%v（支持 PNG、JPEG、GIF）", path, err)
	}
	b := img.Bounds()
	h, w := b.Dy(), b.Dx()
	c := 3
	if gray {
		c = 1
	}
	d := make([]float32, c*h*w)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, bl, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			if gray {
				// ITU-R BT.601 的亮度公式
				d[y*w+x] = (0.299*float32(r) + 0.587*float32(g) + 0.114*float32(bl)) / 65535
				continue
			}
			d[y*w+x] = float32(r) / 65535
			d[h*w+y*w+x] = float32(g) / 65535
			d[2*h*w+y*w+x] = float32(bl) / 65535
		}
	}
	return fromHost(defaultDev, []int{c, h, w}, d)
}

// SaveImage 把 [通道, 高, 宽]（通道为 1 或 3）或 [高, 宽] 的张量保存为 PNG 或 JPEG（按扩展名），
// 数值按 [0, 1] 截断。
func SaveImage(t *Tensor, path string) {
	var c, h, w int
	switch len(t.shape) {
	case 2:
		c, h, w = 1, t.shape[0], t.shape[1]
	case 3:
		c, h, w = t.shape[0], t.shape[1], t.shape[2]
	default:
		raiseShape("save_image 需要 [通道, 高, 宽] 或 [高, 宽] 的张量，得到 %s", shapeStr(t.shape))
	}
	if c != 1 && c != 3 {
		raiseShape("save_image：通道数应为 1 或 3，得到 %d", c)
	}
	d := t.host()
	px := func(v float32) uint8 {
		if v != v {
			return 0
		}
		return uint8(math.Round(float64(min(max(v, 0), 1)) * 255))
	}
	var img image.Image
	if c == 1 {
		g := image.NewGray(image.Rect(0, 0, w, h))
		for i := 0; i < h*w; i++ {
			g.Pix[i] = px(d[i])
		}
		img = g
	} else {
		rgba := image.NewRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				rgba.SetRGBA(x, y, color.RGBA{px(d[y*w+x]), px(d[h*w+y*w+x]), px(d[2*h*w+y*w+x]), 255})
			}
		}
		img = rgba
	}
	f, err := os.Create(path)
	if err != nil {
		rt.Raise("IOError", "无法创建文件 %s：%v", path, err)
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		err = jpeg.Encode(f, img, &jpeg.Options{Quality: 95})
	default:
		err = png.Encode(f, img)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		rt.Raise("IOError", "保存图像 %s 失败：%v", path, err)
	}
}
