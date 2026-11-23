package rttensor

import (
	"runtime"
	"sync"
)

// CPU 上的矩阵乘法（有 AVX2 + FMA 时使用）：按 BLIS 的做法把 A、B 的小块“打包”成连续的窄条，
// 再由 6×16 的汇编内核计算每个小块（gemm_amd64.s）。打包后的数据在缓存中顺序访问，
// 转置的输入也在打包时处理，内核只需要一种形式。

const (
	gemmMR = 6   // 内核一次计算的行数
	gemmNR = 16  // 内核一次计算的列数
	gemmKC = 256 // k 方向上一次处理的长度（打包的 B 窄条 16×256 个数，放得进 L1 缓存）
)

type gemmArgs struct {
	m, n, k int
	a, b, c []float32 // a 为 [m, k]（ta 时为 [k, m]），b 为 [k, n]（tb 时为 [n, k]），c 为 [m, n]
	ta, tb  bool
}

func ceilDiv(a, b int) int { return (a + b - 1) / b }

var gemmBufs = sync.Pool{New: func() any { return new([]float32) }}

func getBuf(n int) *[]float32 {
	p := gemmBufs.Get().(*[]float32)
	if cap(*p) < n {
		*p = make([]float32, n)
	}
	*p = (*p)[:n]
	return p
}

// gemm 计算 c = op(a) @ op(b)（覆盖 c）；parallel 为真时把输出分块交给多个协程。
// k 方向按 gemmKC 分段；每一段先把 B 的这一段整体打包一次（所有任务共用），
// 再由各个任务打包自己负责的 A 的行并计算。
func gemm(g *gemmArgs, parallel bool) {
	m, n, k := g.m, g.n, g.k
	tm, tn := ceilDiv(m, gemmMR), ceilDiv(n, gemmNR)
	if tm == 0 || tn == 0 {
		return
	}
	if k == 0 {
		clear(g.c[:m*n])
		return
	}
	workers := runtime.GOMAXPROCS(0)
	if !parallel {
		workers = 1
	}
	// 每个任务负责最多 12×64 个小块（72 行 × 1024 列）；任务太少时把块切小
	mTiles, nTiles := min(tm, 12), min(tn, 64)
	if workers > 1 {
		for ceilDiv(tm, mTiles)*ceilDiv(tn, nTiles) < 2*workers {
			if mTiles > 1 && (mTiles*gemmMR*4 >= nTiles*gemmNR || nTiles <= 4) {
				mTiles = ceilDiv(mTiles, 2)
			} else if nTiles > 4 {
				nTiles = ceilDiv(nTiles, 2)
			} else {
				break
			}
		}
	}
	colBlocks := ceilDiv(tn, nTiles)
	tasks := ceilDiv(tm, mTiles) * colBlocks
	kc := min(gemmKC, k)
	bpBuf := getBuf(tn * gemmNR * kc)
	defer gemmBufs.Put(bpBuf)
	bp := *bpBuf
	for pc := 0; pc < k; pc += gemmKC {
		kb := min(gemmKC, k-pc)
		packJobs := func(lo, hi int) { packB(bp, g, lo*gemmNR, min(hi*gemmNR, n), hi-lo, pc, kb, lo) }
		if workers > 1 && tn*kb >= 1<<12 {
			parallelFor(tn, max(1, (1<<12)/kb), packJobs)
		} else {
			packJobs(0, tn)
		}
		first := pc == 0
		run := func(lo, hi int) {
			for t := lo; t < hi; t++ {
				i0, jt0 := (t/colBlocks)*mTiles*gemmMR, (t%colBlocks)*nTiles
				gemmBlock(g, bp, i0, min(i0+mTiles*gemmMR, m), jt0, min(jt0+nTiles, tn), pc, kb, first)
			}
		}
		if workers > 1 && tasks > 1 {
			parallelFor(tasks, 1, run)
		} else {
			run(0, tasks)
		}
	}
}

// gemmBlock 把 op(A)[i0:i1, pc:pc+kb] @ op(B)[pc:pc+kb, 列小块 jt0..jt1] 加到 c 上（first 时先清零）。
// bp 是打包好的 B（整个 n 方向）。
func gemmBlock(g *gemmArgs, bp []float32, i0, i1, jt0, jt1, pc, kb int, first bool) {
	n := g.n
	j0, j1 := jt0*gemmNR, min(jt1*gemmNR, n)
	if first {
		for i := i0; i < i1; i++ {
			clear(g.c[i*n+j0 : i*n+j1])
		}
	}
	mtiles := ceilDiv(i1-i0, gemmMR)
	apBuf := getBuf(mtiles * gemmMR * kb)
	defer gemmBufs.Put(apBuf)
	ap := *apBuf
	packA(ap, g, i0, i1, mtiles, pc, kb)
	var tmp [gemmMR * gemmNR]float32
	for jt := jt0; jt < jt1; jt++ {
		j := jt * gemmNR
		bt := &bp[jt*kb*gemmNR]
		for it := 0; it < mtiles; it++ {
			i := i0 + it*gemmMR
			at := &ap[it*kb*gemmMR]
			if i+gemmMR <= i1 && j+gemmNR <= n {
				sgemmKernel6x16(kb, at, bt, &g.c[i*n+j], n)
				continue
			}
			// 边缘上不完整的小块：先算到临时数组中，再加上有效的部分
			tmp = [gemmMR * gemmNR]float32{}
			sgemmKernel6x16(kb, at, bt, &tmp[0], gemmNR)
			for ii := 0; ii < min(gemmMR, i1-i); ii++ {
				row := g.c[(i+ii)*n+j:]
				for jj := 0; jj < min(gemmNR, n-j); jj++ {
					row[jj] += tmp[ii*gemmNR+jj]
				}
			}
		}
	}
}

// packA：ap[t*kb*MR + p*MR + ii] = A(i0 + t*MR + ii, pc + p)，超出 [i0, i1) 的行补 0。
func packA(ap []float32, g *gemmArgs, i0, i1, mtiles, pc, kb int) {
	for t := 0; t < mtiles; t++ {
		dst := ap[t*kb*gemmMR : (t+1)*kb*gemmMR]
		base := i0 + t*gemmMR
		if g.ta { // A(i, p) = a[p*m + i]
			for p := 0; p < kb; p++ {
				src := g.a[(pc+p)*g.m:]
				d := dst[p*gemmMR : p*gemmMR+gemmMR]
				for ii := range d {
					if base+ii < i1 {
						d[ii] = src[base+ii]
					} else {
						d[ii] = 0
					}
				}
			}
			continue
		}
		for ii := 0; ii < gemmMR; ii++ {
			i := base + ii
			if i >= i1 {
				for p := 0; p < kb; p++ {
					dst[p*gemmMR+ii] = 0
				}
				continue
			}
			for p, v := range g.a[i*g.k+pc : i*g.k+pc+kb] {
				dst[p*gemmMR+ii] = v
			}
		}
	}
}

// packB：bp[(t0+t)*kb*NR + p*NR + jj] = B(pc + p, j0 + t*NR + jj)，超出 [j0, j1) 的列补 0。
func packB(bp []float32, g *gemmArgs, j0, j1, ntiles, pc, kb, t0 int) {
	for t := 0; t < ntiles; t++ {
		dst := bp[(t0+t)*kb*gemmNR : (t0+t+1)*kb*gemmNR]
		j := j0 + t*gemmNR
		w := min(gemmNR, j1-j)
		if g.tb { // B(p, j) = b[j*k + p]
			for jj := 0; jj < gemmNR; jj++ {
				if jj >= w {
					for p := 0; p < kb; p++ {
						dst[p*gemmNR+jj] = 0
					}
					continue
				}
				off := (j+jj)*g.k + pc
				for p, v := range g.b[off : off+kb] {
					dst[p*gemmNR+jj] = v
				}
			}
			continue
		}
		for p := 0; p < kb; p++ {
			off := (pc+p)*g.n + j
			d := dst[p*gemmNR : p*gemmNR+gemmNR]
			copy(d, g.b[off:off+w])
			clear(d[w:])
		}
	}
}

// matmulSIMD 是 cpuBackend.matmul 在有 SIMD 内核时的实现。
func matmulSIMD(as, bs, cs []float32, batch, m, k, n int, ta, tb bool, sa, sb int) {
	one := func(bi int, parallel bool) {
		gemm(&gemmArgs{m: m, n: n, k: k, a: as[bi*sa:], b: bs[bi*sb:], c: cs[bi*m*n : (bi+1)*m*n], ta: ta, tb: tb}, parallel)
	}
	work := m * n * k
	switch {
	case batch == 1:
		one(0, work >= 1<<15)
	case batch >= runtime.GOMAXPROCS(0) || work < 1<<16:
		// 批次多或每个矩阵很小：按批次并行
		parallelFor(batch, max(1, (1<<16)/max(work, 1)), func(lo, hi int) {
			for bi := lo; bi < hi; bi++ {
				one(bi, false)
			}
		})
	default:
		for bi := 0; bi < batch; bi++ {
			one(bi, true)
		}
	}
}
