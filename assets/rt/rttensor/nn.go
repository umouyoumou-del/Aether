package rttensor

import (
	"fmt"
	"math"
	"reflect"
	"strings"
	"sync"

	"aether/assets/rt"
)

// 模块（tensor.nn.Module 的子类）是普通的 Aether 对象：参数就是类型为 Tensor 的字段。
// 下面的函数通过生成代码提供的 Fieldsꞏ（对象的公开字段）、Itemsꞏ（列表、元组）、
// Pairsꞏ（字典）遍历对象，找出其中的张量与子模块。

type fielder interface{ Fieldsꞏ() ([]string, []any) }
type itemser interface{ Itemsꞏ() []any }
type pairser interface{ Pairsꞏ() ([]any, []any) }
type typeNamer interface{ TypeNameꞏ() string }

// unwrapOpt 取出可空类型（rt.Opt）中的值。
func unwrapOpt(v any) (any, bool) {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Struct || !strings.HasPrefix(rv.Type().Name(), "Opt[") {
		return v, true
	}
	if !rv.FieldByName("Ok").Bool() {
		return nil, false
	}
	return rv.FieldByName("V").Interface(), true
}

func isPointer(v any) bool { return reflect.ValueOf(v).Kind() == reflect.Pointer }

// walker 按字段的声明顺序深度优先遍历对象图（每个对象只访问一次）。
type walker struct {
	seen    map[any]bool
	tensor  func(name string, t *Tensor)
	object  func(name string, o any)
	leaveFn func(name string, o any)
}

func (w *walker) walk(v any, name string) {
	v, ok := unwrapOpt(v)
	if !ok || v == nil {
		return
	}
	if rv := reflect.ValueOf(v); rv.Kind() == reflect.Pointer && rv.IsNil() {
		return
	}
	if isPointer(v) {
		if w.seen[v] {
			return
		}
		w.seen[v] = true
	}
	join := func(child string) string {
		if name == "" {
			return child
		}
		return name + "." + child
	}
	switch x := v.(type) {
	case *Tensor:
		if w.tensor != nil {
			w.tensor(name, x)
		}
	case fielder:
		if w.object != nil {
			w.object(name, x)
		}
		names, vals := x.Fieldsꞏ()
		for i, val := range vals {
			w.walk(val, join(names[i]))
		}
		if w.leaveFn != nil {
			w.leaveFn(name, x)
		}
	case itemser:
		for i, val := range x.Itemsꞏ() {
			w.walk(val, join(fmt.Sprint(i)))
		}
	case pairser:
		keys, vals := x.Pairsꞏ()
		for i, val := range vals {
			w.walk(val, join(rt.Str(keys[i])))
		}
	}
}

func newWalker() *walker { return &walker{seen: map[any]bool{}} }

// NamedTensors 返回对象中所有张量（参数与缓冲区）及其名字，例如 "fc1.weight"。
func NamedTensors(root any) *rt.List[rt.T2[string, *Tensor]] {
	out := &rt.List[rt.T2[string, *Tensor]]{}
	w := newWalker()
	w.tensor = func(name string, t *Tensor) { out.S = append(out.S, rt.T2[string, *Tensor]{V0: name, V1: t}) }
	w.walk(root, "")
	return out
}

// NamedParameters 返回需要梯度的张量（可训练的参数）及其名字。
func NamedParameters(root any) *rt.List[rt.T2[string, *Tensor]] {
	all := NamedTensors(root)
	out := &rt.List[rt.T2[string, *Tensor]]{}
	for _, p := range all.S {
		if p.V1.requiresGrad && p.V1.fn == nil {
			out.S = append(out.S, p)
		}
	}
	return out
}

// Parameters 返回对象中所有可训练的参数。
func Parameters(root any) *rt.List[*Tensor] {
	out := &rt.List[*Tensor]{}
	for _, p := range NamedParameters(root).S {
		out.S = append(out.S, p.V1)
	}
	return out
}

// Objects 返回从 root 出发可以到达的所有对象（包括 root 自己），用于查找子模块。
func Objects(root any) *rt.List[any] {
	out := &rt.List[any]{}
	w := newWalker()
	w.object = func(_ string, o any) { out.S = append(out.S, o) }
	w.walk(root, "")
	return out
}

// StateDict 返回名字到张量的字典（与模块共享数据，不参与求导）。
func StateDict(root any) *rt.Dict[string, *Tensor] {
	d := rt.NewDict[string, *Tensor](0)
	for _, p := range NamedTensors(root).S {
		d.Set(p.V0, p.V1.Detach())
	}
	return d
}

// LoadStateDict 把字典中的数据复制到模块的张量中（张量对象不变，所以优化器仍然有效）。
// strict 为真时，名字必须完全对应。
func LoadStateDict(root any, d *rt.Dict[string, *Tensor], strict bool) {
	named := NamedTensors(root)
	have := map[string]bool{}
	var missing []string
	for _, p := range named.S {
		have[p.V0] = true
		src, ok := d.Lookup(p.V0)
		if !ok {
			missing = append(missing, p.V0)
			continue
		}
		if !sameShape(src.shape, p.V1.shape) {
			raiseShape("load_state_dict：%s 的形状是 %s，但保存的数据是 %s", p.V0, shapeStr(p.V1.shape), shapeStr(src.shape))
		}
		p.V1.copyData(src)
	}
	if !strict {
		return
	}
	var unexpected []string
	for _, k := range d.Keys().S {
		if !have[k] {
			unexpected = append(unexpected, k)
		}
	}
	if len(missing) > 0 || len(unexpected) > 0 {
		msg := "load_state_dict：名字不一致"
		if len(missing) > 0 {
			msg += "；模型中有但数据中没有：" + strings.Join(missing, ", ")
		}
		if len(unexpected) > 0 {
			msg += "；数据中有但模型中没有：" + strings.Join(unexpected, ", ")
		}
		rt.Raise("KeyError", "%s", msg)
	}
}

// copyData 把 src 的数据复制到 t 中（原地修改，不记录求导；可以跨设备）。
func (t *Tensor) copyData(src *Tensor) {
	if src.dev() != t.dev() {
		src = &Tensor{shape: src.shape, n: src.n, st: t.dev().upload(src.host())}
	}
	t.dev().copyTo(t.st, src.st, t.n)
}

// CopyFrom 实现 x.copy_(src)：把 src 的数据复制到 x 中（形状必须相同）。
func (t *Tensor) CopyFrom(src *Tensor) *Tensor {
	if !sameShape(t.shape, src.shape) {
		raiseShape("copy_：形状 %s 与 %s 不同", shapeStr(t.shape), shapeStr(src.shape))
	}
	if t.requiresGrad && t.fn == nil && gradEnabled() {
		rt.Raise("RuntimeError", "不能在记录计算图时原地修改需要梯度的叶子张量；请放在 with tensor.no_grad(): 中")
	}
	t.copyData(src)
	return t
}

// ModuleTo 把对象中的所有张量（包括梯度）原地移到指定设备。
func ModuleTo(root any, device string) {
	dev := deviceByName(device)
	for _, p := range NamedTensors(root).S {
		p.V1.moveTo(dev)
	}
}

// ZeroGrads 清除对象中所有参数的梯度。
func ZeroGrads(root any) {
	for _, p := range NamedTensors(root).S {
		p.V1.grad = nil
	}
}

// NumParameters 返回可训练参数的元素总数。
func NumParameters(root any) int {
	n := 0
	for _, p := range Parameters(root).S {
		n += p.n
	}
	return n
}

// ModuleRepr 显示模块的结构：每个子模块一行，叶子模块显示其中的数值字段与张量的形状。
func ModuleRepr(root any) string {
	var sb strings.Builder
	seen := map[any]bool{}
	var rec func(o fielder, indent int)
	rec = func(o fielder, indent int) {
		name := "?"
		if tn, ok := o.(typeNamer); ok {
			name = tn.TypeNameꞏ()
		}
		names, vals := o.Fieldsꞏ()
		var attrs []string
		type child struct {
			name string
			obj  fielder
		}
		var children []child
		var addChild func(n string, v any)
		addChild = func(n string, v any) {
			v, ok := unwrapOpt(v)
			if !ok || v == nil {
				return
			}
			switch x := v.(type) {
			case *Tensor:
				attrs = append(attrs, fmt.Sprintf("%s=%s", n, shapeStr(x.shape)))
			case fielder:
				if !seen[x] {
					seen[x] = true
					children = append(children, child{n, x})
				}
			case itemser:
				for i, it := range x.Itemsꞏ() {
					addChild(fmt.Sprintf("%s.%d", n, i), it)
				}
			case pairser:
				keys, vs := x.Pairsꞏ()
				for i, it := range vs {
					addChild(n+"."+rt.Str(keys[i]), it)
				}
			case bool:
				if n != "training" {
					attrs = append(attrs, fmt.Sprintf("%s=%v", n, x))
				}
			case int, int64, float64, string:
				attrs = append(attrs, fmt.Sprintf("%s=%s", n, rt.Repr(x)))
			}
		}
		for i, v := range vals {
			addChild(names[i], v)
		}
		sb.WriteString(name + "(" + strings.Join(attrs, ", "))
		if len(children) > 0 {
			if len(attrs) > 0 {
				sb.WriteString(",")
			}
			sb.WriteString("\n")
			for _, c := range children {
				sb.WriteString(strings.Repeat("  ", indent+1) + "(" + c.name + "): ")
				rec(c.obj, indent+1)
				sb.WriteString("\n")
			}
			sb.WriteString(strings.Repeat("  ", indent))
		}
		sb.WriteString(")")
	}
	if f, ok := root.(fielder); ok {
		seen[root] = true
		rec(f, 0)
	}
	return sb.String()
}

// ClipGradNorm 把所有参数的梯度按总的 L2 范数缩放到不超过 maxNorm，返回缩放前的范数。
func ClipGradNorm(params *rt.List[*Tensor], maxNorm float64) float64 {
	var total float64
	for _, p := range params.S {
		if p.grad != nil {
			var s float64
			withNoGrad(func() { s = p.grad.Mul(p.grad).sumAll().Item() })
			total += s
		}
	}
	norm := math.Sqrt(total)
	if norm > maxNorm && norm > 0 {
		scale := maxNorm / (norm + 1e-6)
		for _, p := range params.S {
			if p.grad != nil {
				// 不原地修改：多个参数的梯度可能共享同一块数据
				withNoGrad(func() { p.grad = p.grad.MulScalar(scale) })
			}
		}
	}
	return norm
}

// ===== 常用的层（组合已有的可求导运算） =====

// LinearFn 实现 x @ weightᵀ + bias：x 的形状为 [..., in]，weight 为 [out, in]。
func LinearFn(x, weight *Tensor, bias rt.Opt[*Tensor]) *Tensor {
	if len(weight.shape) != 2 {
		raiseShape("linear 的权重应为 [输出维数, 输入维数]，得到 %s", shapeStr(weight.shape))
	}
	if len(x.shape) == 0 || x.shape[len(x.shape)-1] != weight.shape[1] {
		raiseShape("linear：输入的形状 %s 的最后一维应为 %d", shapeStr(x.shape), weight.shape[1])
	}
	in, outF := weight.shape[1], weight.shape[0]
	rows := x.n / max(in, 1)
	x2 := x.reshape([]int{rows, in})
	out := mm(x2, weight, false, true, 1, rows, in, outF, 0, 0)
	out = record(out, "linear", []*Tensor{x2, weight}, func(g *Tensor) []*Tensor {
		var gx, gw *Tensor
		if x2.requiresGrad {
			gx = mm(g, weight, false, false, 1, rows, outF, in, 0, 0)
		}
		if weight.requiresGrad {
			gw = mm(g, x2, true, false, 1, outF, rows, in, 0, 0)
		}
		return []*Tensor{gx, gw}
	})
	if bias.Ok {
		out = out.Add(bias.V)
	}
	shape := append(append([]int(nil), x.shape[:len(x.shape)-1]...), outF)
	return out.reshape(shape)
}

// BatchNorm 对 [N, C] 或 [N, C, ...] 的输入按通道做批归一化。训练时使用这一批的统计量，
// 并原地更新 runMean、runVar（滑动平均）；推理时使用 runMean、runVar。
func BatchNorm(x, weight, bias, runMean, runVar *Tensor, training bool, momentum, eps float64) *Tensor {
	if len(x.shape) < 2 {
		raiseShape("batch_norm 的输入至少要有两维 [N, C, ...]，得到 %s", shapeStr(x.shape))
	}
	n, c := x.shape[0], x.shape[1]
	l := x.n / max(n*c, 1)
	x3 := x.reshape([]int{n, c, l})
	var mu, v *Tensor
	if training {
		cnt := float64(n * l)
		if cnt <= 1 {
			rt.Raise("ValueError", "batch_norm 训练时每个通道至少需要 2 个数值，得到形状 %s", shapeStr(x.shape))
		}
		mu = x3.sumDim(2, true).sumDim(0, true).MulScalar(1 / cnt)
		xc := x3.Sub(mu)
		v = xc.Mul(xc).sumDim(2, true).sumDim(0, true).MulScalar(1 / cnt)
		withNoGrad(func() {
			m := momentum
			runMean.copyData(runMean.MulScalar(1 - m).Add(mu.Detach().view([]int{c}).MulScalar(m)))
			runVar.copyData(runVar.MulScalar(1 - m).Add(v.Detach().view([]int{c}).MulScalar(m * cnt / (cnt - 1))))
		})
	} else {
		mu = runMean.view([]int{1, c, 1})
		v = runVar.view([]int{1, c, 1})
	}
	y := x3.Sub(mu).Div(v.AddScalar(eps).Sqrt())
	y = y.Mul(weight.reshape([]int{1, c, 1})).Add(bias.reshape([]int{1, c, 1}))
	return y.reshape(x.shape)
}

// OneHot 把类别下标转换为 one-hot 编码：结果的形状是 idx 的形状加上 [classes]。
func OneHot(idx *Tensor, classes int) *Tensor {
	h := idx.host()
	d := make([]float32, len(h)*classes)
	for i, v := range h {
		k := int(v)
		if k < 0 || k >= classes || float32(k) != v {
			rt.Raise("IndexError", "one_hot：类别 %v 不在 0 到 %d 之间", v, classes-1)
		}
		d[i*classes+k] = 1
	}
	return fromHost(idx.dev(), append(append([]int(nil), idx.shape...), classes), d)
}

// Batches 把 0 到 n-1 分成若干批（shuffle 为真时先随机打乱），返回每一批的下标张量。
func Batches(n, batchSize int, shuffle bool) *rt.List[*Tensor] {
	if batchSize <= 0 {
		rt.Raise("ValueError", "batch_size 必须是正数")
	}
	order := make([]float32, n)
	for i := range order {
		order[i] = float32(i)
	}
	if shuffle {
		perm := Randperm(n)
		order = perm.host()
	}
	out := &rt.List[*Tensor]{}
	for lo := 0; lo < n; lo += batchSize {
		hi := min(lo+batchSize, n)
		out.S = append(out.S, fromHost(defaultDev, []int{hi - lo}, order[lo:hi]))
	}
	return out
}

// CausalMask 返回 [n, n] 的注意力掩码：第 i 行中 j ≤ i 的位置为 0，j > i 的位置为 -1e9
// （加到注意力分数上之后，softmax 使每个位置只关注它自己和之前的位置）。
func CausalMask(n int) *Tensor {
	key := maskKey{defaultDev, n}
	maskMu.Lock()
	defer maskMu.Unlock()
	if m, ok := maskCache[key]; ok {
		return m
	}
	d := make([]float32, n*n)
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			d[i*n+j] = -1e9
		}
	}
	m := fromHost(defaultDev, []int{n, n}, d)
	if len(maskCache) > 64 {
		clear(maskCache)
	}
	maskCache[key] = m
	return m
}

type maskKey struct {
	dev backend
	n   int
}

// 因果掩码只读，按设备与长度缓存（每次前向计算都会用到）
var (
	maskMu    sync.Mutex
	maskCache = map[maskKey]*Tensor{}
)

// triMask 返回形状为 [rows, cols] 的 0/1 矩阵：lower 为真时保留 j - i ≤ diag 的位置，否则保留 j - i ≥ diag。
func triMask(dev backend, rows, cols, diag int, lower bool) *Tensor {
	d := make([]float32, rows*cols)
	for i := 0; i < rows; i++ {
		for j := 0; j < cols; j++ {
			if (lower && j-i <= diag) || (!lower && j-i >= diag) {
				d[i*cols+j] = 1
			}
		}
	}
	return fromHost(dev, []int{rows, cols}, d)
}

// Tril 保留最后两维的下三角部分（j - i ≤ diagonal），其余置 0。
func (x *Tensor) Tril(diagonal int) *Tensor {
	if len(x.shape) < 2 {
		raiseShape("tril 需要至少二维的张量，得到 %s", shapeStr(x.shape))
	}
	r, c := x.shape[len(x.shape)-2], x.shape[len(x.shape)-1]
	return x.Mul(triMask(x.dev(), r, c, diagonal, true))
}

// Triu 保留最后两维的上三角部分（j - i ≥ diagonal），其余置 0。
func (x *Tensor) Triu(diagonal int) *Tensor {
	if len(x.shape) < 2 {
		raiseShape("triu 需要至少二维的张量，得到 %s", shapeStr(x.shape))
	}
	r, c := x.shape[len(x.shape)-2], x.shape[len(x.shape)-1]
	return x.Mul(triMask(x.dev(), r, c, diagonal, false))
}

// Multinomial 按概率抽样：probs 为 [C] 或 [N, C]（每行是非负的权重，不必归一化），
// 每行有放回地抽取 numSamples 个类别下标。使用 manual_seed 控制的随机数。
func Multinomial(probs *Tensor, numSamples int) *Tensor {
	if len(probs.shape) != 1 && len(probs.shape) != 2 {
		raiseShape("multinomial 需要一维或二维的张量，得到 %s", shapeStr(probs.shape))
	}
	if numSamples < 1 {
		rt.Raise("ValueError", "multinomial 的 num_samples 必须是正数")
	}
	cols := probs.shape[len(probs.shape)-1]
	rows := probs.n / max(cols, 1)
	p := probs.host()
	u := newTensor(cpuDev, []int{rows * numSamples})
	cpuDev.random(u.st, u.n, nextSeed(), false)
	out := make([]float32, rows*numSamples)
	for r := 0; r < rows; r++ {
		row := p[r*cols : (r+1)*cols]
		var total float64
		for _, v := range row {
			if v < 0 || math.IsNaN(float64(v)) {
				rt.Raise("ValueError", "multinomial 的概率不能是负数或 nan")
			}
			total += float64(v)
		}
		if total <= 0 {
			rt.Raise("ValueError", "multinomial：第 %d 行的概率之和为 0", r)
		}
		for s := 0; s < numSamples; s++ {
			target := float64(u.st.cpu[r*numSamples+s]) * total
			k, acc := cols-1, 0.0
			for j, v := range row {
				acc += float64(v)
				if target < acc {
					k = j
					break
				}
			}
			out[r*numSamples+s] = float32(k)
		}
	}
	shape := []int{rows, numSamples}
	if len(probs.shape) == 1 {
		shape = []int{numSamples}
	}
	return fromHost(probs.dev(), shape, out)
}
