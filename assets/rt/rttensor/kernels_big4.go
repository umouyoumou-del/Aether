package rttensor

import "strings"

// matmul_big4：大矩阵乘法的向量化版本（要求各维与批次步长都是 4 的倍数，A 不转置）。
// 每个工作组（16x16 个工作项）计算 128x128 的输出块；每个工作项计算 8x8 个元素——行 ty*4..ty*4+3 与
// 64+ty*4..，列 tx*4..tx*4+3 与 64+tx*4..，所以局部内存可以按 float4 读取，结果也按 float4 写回。
// 全局内存同样按 float4（int8 版本按 char4）读取。BTYPE 为 B 的元素类型：float，或者 char（量化的权重，
// 每行的缩放由调用者另外乘上）。
const matmulBig4Template = `
__kernel __attribute__((reqd_work_group_size(16, 16, 1)))
void NAME(__global const float* A, __global const BTYPE* B, __global float* C,
          int M, int N, int K, int tb, int sa, int sb) {
    int bz = get_global_id(2);
    A += bz * sa;
    B += bz * sb;
    C += bz * M * N;
    int tx = get_local_id(0), ty = get_local_id(1);
    int row0 = get_group_id(1) * 128, col0 = get_group_id(0) * 128;
    __local float As[16][128];
    __local float Bs[16][128];
    float4 acc[8][2];
    for (int i = 0; i < 8; i++) { acc[i][0] = (float4)(0.0f); acc[i][1] = (float4)(0.0f); }
    int lid = ty * 16 + tx;
    for (int k0 = 0; k0 < K; k0 += 16) {
        for (int l = 0; l < 2; l++) {
            int idx = lid + l * 256;
            // A 的块：128 行，每行 16 个数（4 个 float4）
            int r = idx >> 2, kq = (idx & 3) * 4;
            int gm = row0 + r, gk = k0 + kq;
            float4 v = (float4)(0.0f);
            if (gm < M && gk < K) v = vload4(0, A + gm * K + gk);
            As[kq][r] = v.x; As[kq + 1][r] = v.y; As[kq + 2][r] = v.z; As[kq + 3][r] = v.w;
            float4 w = (float4)(0.0f);
            if (tb) {
                // B 为 [N, K]：与 A 一样按行读取
                int gn = col0 + r;
                if (gn < N && gk < K) w = LOADB(B + gn * K + gk);
                Bs[kq][r] = w.x; Bs[kq + 1][r] = w.y; Bs[kq + 2][r] = w.z; Bs[kq + 3][r] = w.w;
            } else {
                // B 为 [K, N]：每行 128 个数（32 个 float4）
                int kk = idx >> 5, nq = (idx & 31) * 4;
                int gk2 = k0 + kk, gn = col0 + nq;
                if (gk2 < K && gn < N) w = LOADB(B + gk2 * N + gn);
                vstore4(w, 0, &Bs[kk][nq]);
            }
        }
        barrier(CLK_LOCAL_MEM_FENCE);
        #pragma unroll
        for (int kk = 0; kk < 16; kk++) {
            float4 a0 = vload4(0, &As[kk][ty * 4]), a1 = vload4(0, &As[kk][64 + ty * 4]);
            float4 b0 = vload4(0, &Bs[kk][tx * 4]), b1 = vload4(0, &Bs[kk][64 + tx * 4]);
            float a[8] = {a0.x, a0.y, a0.z, a0.w, a1.x, a1.y, a1.z, a1.w};
            #pragma unroll
            for (int i = 0; i < 8; i++) {
                acc[i][0] = fma((float4)(a[i]), b0, acc[i][0]);
                acc[i][1] = fma((float4)(a[i]), b1, acc[i][1]);
            }
        }
        barrier(CLK_LOCAL_MEM_FENCE);
    }
    for (int i = 0; i < 8; i++) {
        int gm = row0 + (i < 4 ? ty * 4 + i : 64 + ty * 4 + i - 4);
        if (gm >= M) continue;
        int gn0 = col0 + tx * 4, gn1 = col0 + 64 + tx * 4;
        if (gn0 < N) vstore4(acc[i][0], 0, C + gm * N + gn0);
        if (gn1 < N) vstore4(acc[i][1], 0, C + gm * N + gn1);
    }
}
`

func matmulBig4Kernel(name, btype string) string {
	r := strings.NewReplacer("NAME", name, "BTYPE", btype, "LOADB", "LOAD4_"+btype)
	return r.Replace(matmulBig4Template)
}

// 完整的内核源码：基本的内核（kernels.go）加上由模板生成的向量化矩阵乘法
var kernelProgram = kernelSource + `
#define LOAD4_float(p) vload4(0, p)
#define LOAD4_char(p) convert_float4(vload4(0, p))
` + matmulBig4Kernel("matmul_big4", "float") + matmulBig4Kernel("matmul_big4_q8", "char")
