package rttensor

// kernelSource 是 GPU 后端的 OpenCL C 源码，在第一次使用 GPU 时由显卡驱动编译。
// 运算的编号与 backend.go 中的常量一致。
const kernelSource = `
#define U_NEG 0
#define U_EXP 1
#define U_LOG 2
#define U_SQRT 3
#define U_ABS 4
#define U_RELU 5
#define U_SIGMOID 6
#define U_TANH 7
#define U_GELU 8
#define U_SIN 9
#define U_COS 10
#define U_ADDS 11
#define U_MULS 12
#define U_RSUBS 13
#define U_RDIVS 14
#define U_POWS 15
#define U_CLAMP 16
#define U_LEAKY 17
#define U_SQUARE 18
#define U_RECIP 19
#define U_SIGN 20
#define U_GTS 21
#define U_GELUGRAD 22
#define U_SIGDERIV 23
#define U_TANHDERIV 24
#define U_LEAKYGRAD 25
#define U_POWSGRAD 26
#define U_LTS 27
#define U_SILU 28
#define U_SILUGRAD 29

float gelu_f(float x) {
    return 0.5f * x * (1.0f + tanh(0.7978845608028654f * (x + 0.044715f * x * x * x)));
}

float gelu_grad_f(float x) {
    float u = 0.7978845608028654f * (x + 0.044715f * x * x * x);
    float t = tanh(u);
    float du = 0.7978845608028654f * (1.0f + 3.0f * 0.044715f * x * x);
    return 0.5f * (1.0f + t) + 0.5f * x * (1.0f - t * t) * du;
}

float unary_f(int op, float x, float a, float b) {
    switch (op) {
    case U_NEG: return -x;
    case U_EXP: return exp(x);
    case U_LOG: return log(x);
    case U_SQRT: return sqrt(x);
    case U_ABS: return fabs(x);
    case U_RELU: return fmax(x, 0.0f);
    case U_SIGMOID: return 1.0f / (1.0f + exp(-x));
    case U_TANH: return tanh(x);
    case U_GELU: return gelu_f(x);
    case U_SIN: return sin(x);
    case U_COS: return cos(x);
    case U_ADDS: return x + a;
    case U_MULS: return x * a;
    case U_RSUBS: return a - x;
    case U_RDIVS: return a / x;
    case U_POWS: return pow(x, a);
    case U_CLAMP: return fmin(fmax(x, a), b);
    case U_LEAKY: return x > 0.0f ? x : a * x;
    case U_SQUARE: return x * x;
    case U_RECIP: return 1.0f / x;
    case U_SIGN: return x > 0.0f ? 1.0f : (x < 0.0f ? -1.0f : 0.0f);
    case U_GTS: return x > a ? 1.0f : 0.0f;
    case U_GELUGRAD: return gelu_grad_f(x);
    case U_SIGDERIV: return x * (1.0f - x);
    case U_TANHDERIV: return 1.0f - x * x;
    case U_LEAKYGRAD: return x > 0.0f ? 1.0f : a;
    case U_POWSGRAD: return a * pow(x, a - 1.0f);
    case U_LTS: return x < a ? 1.0f : 0.0f;
    case U_SILU: return x / (1.0f + exp(-x));
    case U_SILUGRAD: { float s = 1.0f / (1.0f + exp(-x)); return s * (1.0f + x * (1.0f - s)); }
    }
    return 0.0f;
}

__kernel void unary_op(int op, __global const float* x, __global float* y, int n, float a, float b) {
    int i = get_global_id(0);
    if (i < n) y[i] = unary_f(op, x[i], a, b);
}

float binary_f(int op, float x, float y) {
    switch (op) {
    case 0: return x + y;
    case 1: return x - y;
    case 2: return x * y;
    case 3: return x / y;
    case 4: return pow(x, y);
    case 5: return fmax(x, y);
    case 6: return fmin(x, y);
    case 7: return x > y ? 1.0f : 0.0f;
    case 8: return x < y ? 1.0f : 0.0f;
    case 9: return x == y ? 1.0f : 0.0f;
    case 10: return x >= y ? 1.0f : 0.0f;
    case 11: return x <= y ? 1.0f : 0.0f;
    }
    return 0.0f;
}

__kernel void binary_same(int op, __global const float* x, __global const float* y, __global float* out, int n) {
    int i = get_global_id(0);
    if (i < n) out[i] = binary_f(op, x[i], y[i]);
}

// 广播：由线性下标求出两个输入中的位置。各维的长度与步长直接取向量的分量（展开写），
// 不放进私有数组——GPU 上按变量下标访问的私有数组会放到很慢的显存中
#define BCAST_DIM(c) if (c < nd) { int k = rem % shape.s##c; rem /= shape.s##c; ox += k * sx.s##c; oy += k * sy.s##c; }
__kernel void binary_bcast(int op, __global const float* x, __global const float* y, __global float* out, int n,
                           int nd, int8 shape, int8 sx, int8 sy) {
    int i = get_global_id(0);
    if (i >= n) return;
    int rem = i, ox = 0, oy = 0;
    BCAST_DIM(7) BCAST_DIM(6) BCAST_DIM(5) BCAST_DIM(4) BCAST_DIM(3) BCAST_DIM(2) BCAST_DIM(1) BCAST_DIM(0)
    out[i] = binary_f(op, x[ox], y[oy]);
}

__kernel void fill(__global float* x, int n, float v) {
    int i = get_global_id(0);
    if (i < n) x[i] = v;
}

__kernel void copy_buf(__global const float* src, __global float* dst, int n) {
    int i = get_global_id(0);
    if (i < n) dst[i] = src[i];
}

// 沿中间一维归约：每个工作项负责一个 (outer, inner) 位置
__kernel void reduce_dim(int op, __global const float* x, __global float* out, __global float* idx,
                         int outer, int dim, int inner, int want_idx) {
    int t = get_global_id(0);
    if (t >= outer * inner) return;
    int o = t / inner, i = t % inner;
    int base = o * dim * inner + i;
    if (op == 0) {
        float acc = 0.0f, comp = 0.0f;
        for (int d = 0; d < dim; d++) {
            // Kahan 求和，减少大量元素相加时的误差
            float y = x[base + d * inner] - comp;
            float s = acc + y;
            comp = (s - acc) - y;
            acc = s;
        }
        out[t] = acc;
    } else {
        float best = x[base];
        int bi = 0;
        for (int d = 1; d < dim; d++) {
            float v = x[base + d * inner];
            if ((op == 1 && v > best) || (op == 2 && v < best)) { best = v; bi = d; }
        }
        out[t] = best;
        if (want_idx) idx[t] = (float)bi;
    }
}

// 沿很长的一维求和的第一步：把 dim 分成长度为 chunk 的段，part[o, c, i] = 第 c 段的和
__kernel void reduce_chunks(__global const float* x, __global float* part, int outer, int dim, int inner, int chunk, int chunks) {
    int t = get_global_id(0);
    if (t >= outer * chunks * inner) return;
    int i = t % inner, c = (t / inner) % chunks, o = t / (inner * chunks);
    int d0 = c * chunk, d1 = min(d0 + chunk, dim);
    __global const float* p = x + (o * dim + d0) * inner + i;
    float s = 0.0f;
    for (int d = d0; d < d1; d++, p += inner) s += *p;
    part[t] = s;
}

// 对每一行（最后一维）求和：每个工作组处理一行，组内的工作项读取相邻的元素
__kernel void reduce_rows(__global const float* x, __global float* out, int rows, int cols, __local float* tmp) {
    int r = get_group_id(0), lid = get_local_id(0), lsz = get_local_size(0);
    if (r >= rows) return;
    __global const float* row = x + r * cols;
    float s = 0.0f;
    for (int j = lid; j < cols; j += lsz) s += row[j];
    tmp[lid] = s;
    barrier(CLK_LOCAL_MEM_FENCE);
    for (int k = lsz / 2; k > 0; k >>= 1) {
        if (lid < k) tmp[lid] += tmp[lid + k];
        barrier(CLK_LOCAL_MEM_FENCE);
    }
    if (lid == 0) out[r] = tmp[0];
}

// 对一大段连续数据求和的第一步：每个工作组得到一个部分和
__kernel void reduce_partial(__global const float* x, __global float* part, int n, __local float* tmp) {
    int lid = get_local_id(0), gsz = get_global_size(0);
    float acc = 0.0f;
    for (int i = get_global_id(0); i < n; i += gsz) acc += x[i];
    tmp[lid] = acc;
    barrier(CLK_LOCAL_MEM_FENCE);
    for (int s = get_local_size(0) / 2; s > 0; s >>= 1) {
        if (lid < s) tmp[lid] += tmp[lid + s];
        barrier(CLK_LOCAL_MEM_FENCE);
    }
    if (lid == 0) part[get_group_id(0)] = tmp[0];
}

// 矩阵乘法：每个工作组（16x16 个工作项）计算 64x64 的输出块，每个工作项计算 4x4 个元素
#define TS 64
#define TK 16
__kernel __attribute__((reqd_work_group_size(16, 16, 1)))
void matmul(__global const float* A, __global const float* B, __global float* C,
            int M, int N, int K, int ta, int tb, int sa, int sb) {
    int bz = get_global_id(2);
    A += bz * sa;
    B += bz * sb;
    C += bz * M * N;
    int tx = get_local_id(0), ty = get_local_id(1);
    int row0 = get_group_id(1) * TS, col0 = get_group_id(0) * TS;
    __local float As[TK][TS + 1];
    __local float Bs[TK][TS + 1];
    float acc[4][4];
    for (int i = 0; i < 4; i++) for (int j = 0; j < 4; j++) acc[i][j] = 0.0f;
    int lid = ty * 16 + tx;
    for (int k0 = 0; k0 < K; k0 += TK) {
        for (int l = 0; l < 4; l++) {
            int idx = lid + l * 256;
            int m = idx / TK, kk = idx % TK;
            int gm = row0 + m, gk = k0 + kk;
            float v = 0.0f;
            if (gm < M && gk < K) v = ta ? A[gk * M + gm] : A[gm * K + gk];
            As[kk][m] = v;
            int kk2 = idx / TS, nn = idx % TS;
            int gk2 = k0 + kk2, gn = col0 + nn;
            float w = 0.0f;
            if (gk2 < K && gn < N) w = tb ? B[gn * K + gk2] : B[gk2 * N + gn];
            Bs[kk2][nn] = w;
        }
        barrier(CLK_LOCAL_MEM_FENCE);
        for (int kk = 0; kk < TK; kk++) {
            float a0 = As[kk][ty], a1 = As[kk][ty + 16], a2 = As[kk][ty + 32], a3 = As[kk][ty + 48];
            float b0 = Bs[kk][tx], b1 = Bs[kk][tx + 16], b2 = Bs[kk][tx + 32], b3 = Bs[kk][tx + 48];
            acc[0][0] += a0 * b0; acc[0][1] += a0 * b1; acc[0][2] += a0 * b2; acc[0][3] += a0 * b3;
            acc[1][0] += a1 * b0; acc[1][1] += a1 * b1; acc[1][2] += a1 * b2; acc[1][3] += a1 * b3;
            acc[2][0] += a2 * b0; acc[2][1] += a2 * b1; acc[2][2] += a2 * b2; acc[2][3] += a2 * b3;
            acc[3][0] += a3 * b0; acc[3][1] += a3 * b1; acc[3][2] += a3 * b2; acc[3][3] += a3 * b3;
        }
        barrier(CLK_LOCAL_MEM_FENCE);
    }
    for (int i = 0; i < 4; i++) {
        int gm = row0 + ty + i * 16;
        if (gm >= M) continue;
        for (int j = 0; j < 4; j++) {
            int gn = col0 + tx + j * 16;
            if (gn < N) C[gm * N + gn] = acc[i][j];
        }
    }
}

// int8 权重的矩阵乘法：C[M, N] = A[M, K] @ B，B 按字节存放的 int8（tb 时为 [N, K]，否则为 [K, N]），
// 读进局部内存时转换为 float（每行的缩放由调用者另外乘上）。分块方式与 matmul 相同
__kernel __attribute__((reqd_work_group_size(16, 16, 1)))
void matmul_q8(__global const float* A, __global const char* B, __global float* C, int M, int N, int K, int tb) {
    int tx = get_local_id(0), ty = get_local_id(1);
    int row0 = get_group_id(1) * 64, col0 = get_group_id(0) * 64;
    __local float As[16][65];
    __local float Bs[16][65];
    float acc[4][4];
    for (int i = 0; i < 4; i++) for (int j = 0; j < 4; j++) acc[i][j] = 0.0f;
    int lid = ty * 16 + tx;
    for (int k0 = 0; k0 < K; k0 += 16) {
        for (int l = 0; l < 4; l++) {
            int idx = lid + l * 256;
            int m = idx / 16, ka = idx % 16;
            int gm = row0 + m, gk = k0 + ka;
            As[ka][m] = (gm < M && gk < K) ? A[gm * K + gk] : 0.0f;
            if (tb) {
                int n = idx / 16, kb = idx % 16;
                int gn = col0 + n, gk2 = k0 + kb;
                Bs[kb][n] = (gn < N && gk2 < K) ? (float)B[gn * K + gk2] : 0.0f;
            } else {
                int kb = idx / 64, n = idx % 64;
                int gn = col0 + n, gk2 = k0 + kb;
                Bs[kb][n] = (gk2 < K && gn < N) ? (float)B[gk2 * N + gn] : 0.0f;
            }
        }
        barrier(CLK_LOCAL_MEM_FENCE);
        for (int kk = 0; kk < 16; kk++) {
            float a0 = As[kk][ty], a1 = As[kk][ty + 16], a2 = As[kk][ty + 32], a3 = As[kk][ty + 48];
            float b0 = Bs[kk][tx], b1 = Bs[kk][tx + 16], b2 = Bs[kk][tx + 32], b3 = Bs[kk][tx + 48];
            acc[0][0] += a0 * b0; acc[0][1] += a0 * b1; acc[0][2] += a0 * b2; acc[0][3] += a0 * b3;
            acc[1][0] += a1 * b0; acc[1][1] += a1 * b1; acc[1][2] += a1 * b2; acc[1][3] += a1 * b3;
            acc[2][0] += a2 * b0; acc[2][1] += a2 * b1; acc[2][2] += a2 * b2; acc[2][3] += a2 * b3;
            acc[3][0] += a3 * b0; acc[3][1] += a3 * b1; acc[3][2] += a3 * b2; acc[3][3] += a3 * b3;
        }
        barrier(CLK_LOCAL_MEM_FENCE);
    }
    for (int i = 0; i < 4; i++) {
        int gm = row0 + ty + i * 16;
        if (gm >= M) continue;
        for (int j = 0; j < 4; j++) {
            int gn = col0 + tx + j * 16;
            if (gn < N) C[gm * N + gn] = acc[i][j];
        }
    }
}

// 小矩阵或很窄的矩阵（例如注意力中每个头的 [长度, 16] 矩阵）：16x16 的块，每个工作项算一个输出元素。
// 输入是转置存储时换一种方式把块读进局部内存，让相邻的工作项读取相邻的地址
__kernel __attribute__((reqd_work_group_size(16, 16, 1)))
void matmul_small(__global const float* A, __global const float* B, __global float* C,
                  int M, int N, int K, int ta, int tb, int sa, int sb) {
    int tx = get_local_id(0), ty = get_local_id(1);
    int row0 = get_group_id(1) * 16, col0 = get_group_id(0) * 16;
    int i = row0 + ty, j = col0 + tx, bz = get_global_id(2);
    A += bz * sa;
    B += bz * sb;
    __local float As[16][17];
    __local float Bs[16][17];
    float acc = 0.0f;
    for (int k0 = 0; k0 < K; k0 += 16) {
        if (ta) {
            int gi = row0 + tx, gk = k0 + ty;
            As[tx][ty] = (gi < M && gk < K) ? A[gk * M + gi] : 0.0f;
        } else {
            int gk = k0 + tx;
            As[ty][tx] = (i < M && gk < K) ? A[i * K + gk] : 0.0f;
        }
        if (tb) {
            int gj = col0 + ty, gk = k0 + tx;
            Bs[tx][ty] = (gj < N && gk < K) ? B[gj * K + gk] : 0.0f;
        } else {
            int gk = k0 + ty;
            Bs[ty][tx] = (gk < K && j < N) ? B[gk * N + j] : 0.0f;
        }
        barrier(CLK_LOCAL_MEM_FENCE);
        #pragma unroll
        for (int p = 0; p < 16; p++) acc = fma(As[ty][p], Bs[p][tx], acc);
        barrier(CLK_LOCAL_MEM_FENCE);
    }
    if (i < M && j < N) C[bz * M * N + i * N + j] = acc;
}

// 大矩阵的乘法：每个工作组（16x16 个工作项）计算 128x128 的输出块，每个工作项计算 8x8 个元素。
// 局部内存的每一行多留一个位置，避免转置写入时的存储体冲突。
#define BT 128
#define BK 16
__kernel __attribute__((reqd_work_group_size(16, 16, 1)))
void matmul_big(__global const float* A, __global const float* B, __global float* C,
                int M, int N, int K, int ta, int tb, int sa, int sb) {
    int bz = get_global_id(2);
    A += bz * sa;
    B += bz * sb;
    C += bz * M * N;
    int tx = get_local_id(0), ty = get_local_id(1);
    int row0 = get_group_id(1) * BT, col0 = get_group_id(0) * BT;
    __local float As[BK][BT + 1];
    __local float Bs[BK][BT + 1];
    float acc[8][8];
    #pragma unroll
    for (int i = 0; i < 8; i++) {
        #pragma unroll
        for (int j = 0; j < 8; j++) acc[i][j] = 0.0f;
    }
    int lid = ty * 16 + tx;
    for (int k0 = 0; k0 < K; k0 += BK) {
        #pragma unroll
        for (int l = 0; l < 8; l++) {
            int idx = lid + l * 256;
            // 让相邻的工作项读取全局内存中相邻的元素
            int m = ta ? idx % BT : idx / BK, ka = ta ? idx / BT : idx % BK;
            int gm = row0 + m, gk = k0 + ka;
            float v = 0.0f;
            if (gm < M && gk < K) v = ta ? A[gk * M + gm] : A[gm * K + gk];
            As[ka][m] = v;
            int n = tb ? idx / BK : idx % BT, kb = tb ? idx % BK : idx / BT;
            int gn = col0 + n, gk2 = k0 + kb;
            float w = 0.0f;
            if (gk2 < K && gn < N) w = tb ? B[gn * K + gk2] : B[gk2 * N + gn];
            Bs[kb][n] = w;
        }
        barrier(CLK_LOCAL_MEM_FENCE);
        #pragma unroll
        for (int kk = 0; kk < BK; kk++) {
            float a[8], b[8];
            #pragma unroll
            for (int i = 0; i < 8; i++) a[i] = As[kk][ty + 16 * i];
            #pragma unroll
            for (int j = 0; j < 8; j++) b[j] = Bs[kk][tx + 16 * j];
            #pragma unroll
            for (int i = 0; i < 8; i++) {
                #pragma unroll
                for (int j = 0; j < 8; j++) acc[i][j] = fma(a[i], b[j], acc[i][j]);
            }
        }
        barrier(CLK_LOCAL_MEM_FENCE);
    }
    #pragma unroll
    for (int i = 0; i < 8; i++) {
        int gm = row0 + ty + 16 * i;
        if (gm >= M) break;
        #pragma unroll
        for (int j = 0; j < 8; j++) {
            int gn = col0 + tx + 16 * j;
            if (gn < N) C[gm * N + gn] = acc[i][j];
        }
    }
}

// softmax / log_softmax：每个工作组处理一行
__kernel void softmax_rows(__global const float* x, __global float* y, int rows, int cols, int logmode,
                           __local float* tmp) {
    int r = get_group_id(0);
    int lid = get_local_id(0), lsz = get_local_size(0);
    if (r >= rows) return;
    __global const float* row = x + r * cols;
    __global float* out = y + r * cols;
    float m = -INFINITY;
    for (int j = lid; j < cols; j += lsz) m = fmax(m, row[j]);
    tmp[lid] = m;
    barrier(CLK_LOCAL_MEM_FENCE);
    for (int s = lsz / 2; s > 0; s >>= 1) {
        if (lid < s) tmp[lid] = fmax(tmp[lid], tmp[lid + s]);
        barrier(CLK_LOCAL_MEM_FENCE);
    }
    m = tmp[0];
    barrier(CLK_LOCAL_MEM_FENCE);
    float sum = 0.0f;
    for (int j = lid; j < cols; j += lsz) sum += exp(row[j] - m);
    tmp[lid] = sum;
    barrier(CLK_LOCAL_MEM_FENCE);
    for (int s = lsz / 2; s > 0; s >>= 1) {
        if (lid < s) tmp[lid] += tmp[lid + s];
        barrier(CLK_LOCAL_MEM_FENCE);
    }
    sum = tmp[0];
    if (logmode) {
        float ls = log(sum);
        for (int j = lid; j < cols; j += lsz) out[j] = row[j] - m - ls;
    } else {
        float inv = 1.0f / sum;
        for (int j = lid; j < cols; j += lsz) out[j] = exp(row[j] - m) * inv;
    }
}

// 层归一化：每个工作组处理一行（先求均值，再求方差，最后写出 y 与 xhat）
__kernel void layer_norm(__global const float* x, __global const float* w, __global const float* b,
                         __global float* y, __global float* xhat, __global float* rstd,
                         int rows, int d, float eps, __local float* tmp) {
    int r = get_group_id(0);
    int lid = get_local_id(0), lsz = get_local_size(0);
    if (r >= rows) return;
    __global const float* row = x + r * d;
    float s = 0.0f;
    for (int j = lid; j < d; j += lsz) s += row[j];
    tmp[lid] = s;
    barrier(CLK_LOCAL_MEM_FENCE);
    for (int k = lsz / 2; k > 0; k >>= 1) {
        if (lid < k) tmp[lid] += tmp[lid + k];
        barrier(CLK_LOCAL_MEM_FENCE);
    }
    float mean = tmp[0] / d;
    barrier(CLK_LOCAL_MEM_FENCE);
    float v = 0.0f;
    for (int j = lid; j < d; j += lsz) { float t = row[j] - mean; v += t * t; }
    tmp[lid] = v;
    barrier(CLK_LOCAL_MEM_FENCE);
    for (int k = lsz / 2; k > 0; k >>= 1) {
        if (lid < k) tmp[lid] += tmp[lid + k];
        barrier(CLK_LOCAL_MEM_FENCE);
    }
    float inv = 1.0f / sqrt(tmp[0] / d + eps);
    if (lid == 0) rstd[r] = inv;
    for (int j = lid; j < d; j += lsz) {
        float h = (row[j] - mean) * inv;
        xhat[r * d + j] = h;
        y[r * d + j] = h * w[j] + b[j];
    }
}

__kernel void layer_norm_back(__global const float* dy, __global const float* w, __global const float* xhat,
                              __global const float* rstd, __global float* dx, int rows, int d,
                              __local float* tmp) {
    int r = get_group_id(0);
    int lid = get_local_id(0), lsz = get_local_size(0);
    if (r >= rows) return;
    __global const float* g = dy + r * d;
    __global const float* h = xhat + r * d;
    __local float* t1 = tmp;
    __local float* t2 = tmp + lsz;
    float sg = 0.0f, sgh = 0.0f;
    for (int j = lid; j < d; j += lsz) { float gw = g[j] * w[j]; sg += gw; sgh += gw * h[j]; }
    t1[lid] = sg;
    t2[lid] = sgh;
    barrier(CLK_LOCAL_MEM_FENCE);
    for (int k = lsz / 2; k > 0; k >>= 1) {
        if (lid < k) { t1[lid] += t1[lid + k]; t2[lid] += t2[lid + k]; }
        barrier(CLK_LOCAL_MEM_FENCE);
    }
    float mg = t1[0] / d, mgh = t2[0] / d, inv = rstd[r];
    for (int j = lid; j < d; j += lsz) dx[r * d + j] = inv * (g[j] * w[j] - mg - h[j] * mgh);
}

// RMSNorm：每个工作组处理一行
__kernel void rms_norm(__global const float* x, __global const float* w, __global float* y, __global float* xhat,
                       __global float* rstd, int rows, int d, float eps, __local float* tmp) {
    int r = get_group_id(0);
    int lid = get_local_id(0), lsz = get_local_size(0);
    if (r >= rows) return;
    __global const float* row = x + r * d;
    float s = 0.0f;
    for (int j = lid; j < d; j += lsz) s += row[j] * row[j];
    tmp[lid] = s;
    barrier(CLK_LOCAL_MEM_FENCE);
    for (int k = lsz / 2; k > 0; k >>= 1) {
        if (lid < k) tmp[lid] += tmp[lid + k];
        barrier(CLK_LOCAL_MEM_FENCE);
    }
    float inv = 1.0f / sqrt(tmp[0] / d + eps);
    if (lid == 0) rstd[r] = inv;
    for (int j = lid; j < d; j += lsz) {
        float h = row[j] * inv;
        xhat[r * d + j] = h;
        y[r * d + j] = h * w[j];
    }
}

__kernel void rms_norm_back(__global const float* dy, __global const float* w, __global const float* xhat,
                            __global const float* rstd, __global float* dx, int rows, int d, __local float* tmp) {
    int r = get_group_id(0);
    int lid = get_local_id(0), lsz = get_local_size(0);
    if (r >= rows) return;
    __global const float* g = dy + r * d;
    __global const float* h = xhat + r * d;
    float s = 0.0f;
    for (int j = lid; j < d; j += lsz) s += g[j] * w[j] * h[j];
    tmp[lid] = s;
    barrier(CLK_LOCAL_MEM_FENCE);
    for (int k = lsz / 2; k > 0; k >>= 1) {
        if (lid < k) tmp[lid] += tmp[lid + k];
        barrier(CLK_LOCAL_MEM_FENCE);
    }
    float mgh = tmp[0] / d, inv = rstd[r];
    for (int j = lid; j < d; j += lsz) dx[r * d + j] = inv * (g[j] * w[j] - h[j] * mgh);
}

// 重排各维：shape 是输入的形状，strides 是输出第 d 维对应的输入步长，oshape 是输出的形状
__kernel void permute(__global const float* x, __global float* y, int n, int nd, int8 oshape, int8 strides) {
    int i = get_global_id(0);
    if (i >= n) return;
    int dims[8] = {oshape.s0, oshape.s1, oshape.s2, oshape.s3, oshape.s4, oshape.s5, oshape.s6, oshape.s7};
    int st[8] = {strides.s0, strides.s1, strides.s2, strides.s3, strides.s4, strides.s5, strides.s6, strides.s7};
    int rem = i, off = 0;
    for (int d = nd - 1; d >= 0; d--) {
        off += (rem % dims[d]) * st[d];
        rem /= dims[d];
    }
    y[i] = x[off];
}

// narrow：y[o, l, i] = x[o, start + l, i]；place 是反方向
__kernel void narrow(__global const float* x, __global float* y, int outer, int dim, int inner, int start, int length) {
    int t = get_global_id(0);
    int total = outer * length * inner;
    if (t >= total) return;
    int i = t % inner, l = (t / inner) % length, o = t / (inner * length);
    y[t] = x[(o * dim + start + l) * inner + i];
}

__kernel void place(__global const float* x, __global float* y, int outer, int dim, int inner, int start, int length) {
    int t = get_global_id(0);
    int total = outer * length * inner;
    if (t >= total) return;
    int i = t % inner, l = (t / inner) % length, o = t / (inner * length);
    y[(o * dim + start + l) * inner + i] = x[t];
}

void atomic_add_f(volatile __global float* addr, float v) {
    union { unsigned int u; float f; } old, nw;
    do {
        old.f = *addr;
        nw.f = old.f + v;
    } while (atomic_cmpxchg((volatile __global unsigned int*)addr, old.u, nw.u) != old.u);
}

__kernel void index_select(__global const float* w, __global const float* idx, __global float* out, int nidx, int rows, int cols) {
    int t = get_global_id(0);
    if (t >= nidx * cols) return;
    int i = t / cols, j = t % cols;
    int r = (int)idx[i];
    out[t] = (r >= 0 && r < rows) ? w[r * cols + j] : 0.0f;
}

// embedding 的反向：order 是按目标行排好的梯度行号，offsets[r]..offsets[r+1] 是第 r 行的那一段。
// 每个工作项负责输出的一个元素，自己把对应的梯度加起来（不需要原子操作，结果也是确定的）
__kernel void index_add_csr(__global const float* g, __global const float* order, __global const float* offsets,
                            __global float* out, int rows, int cols) {
    int t = get_global_id(0);
    if (t >= rows * cols) return;
    int r = t / cols, j = t % cols;
    int a = (int)offsets[r], b = (int)offsets[r + 1];
    float s = 0.0f;
    for (int p = a; p < b; p++) s += g[(int)order[p] * cols + j];
    out[t] += s;
}

__kernel void pick(__global const float* x, __global const float* idx, __global float* out, int rows, int cols) {
    int i = get_global_id(0);
    if (i >= rows) return;
    int c = (int)idx[i];
    out[i] = (c >= 0 && c < cols) ? x[i * cols + c] : 0.0f;
}

__kernel void pick_back(__global const float* g, __global const float* idx, __global float* out, int rows, int cols) {
    int i = get_global_id(0);
    if (i >= rows) return;
    int c = (int)idx[i];
    if (c >= 0 && c < cols) out[i * cols + c] += g[i];
}

// 卷积的展开：cols[b, (c, ki, kj), (oy, ox)] = x[b, c, oy*sh - ph + ki, ox*sw - pw + kj]
__kernel void im2col(__global const float* x, __global float* cols, int n, int c, int h, int w,
                     int kh, int kw, int sh, int sw, int ph, int pw, int oh, int ow) {
    int t = get_global_id(0);
    int rowsPer = c * kh * kw, colsPer = oh * ow;
    if (t >= n * rowsPer * colsPer) return;
    int pos = t % colsPer;
    int r = (t / colsPer) % rowsPer;
    int b = t / (colsPer * rowsPer);
    int ci = r / (kh * kw), kr = r % (kh * kw);
    int ki = kr / kw, kj = kr % kw;
    int oy = pos / ow, ox = pos % ow;
    int iy = oy * sh - ph + ki, ix = ox * sw - pw + kj;
    float v = 0.0f;
    if (iy >= 0 && iy < h && ix >= 0 && ix < w) v = x[((b * c + ci) * h + iy) * w + ix];
    cols[t] = v;
}

// col2im：每个工作项负责输入中的一个位置，把所有覆盖它的展开项加起来（不需要原子操作）
__kernel void col2im(__global const float* cols, __global float* x, int n, int c, int h, int w,
                     int kh, int kw, int sh, int sw, int ph, int pw, int oh, int ow) {
    int t = get_global_id(0);
    if (t >= n * c * h * w) return;
    int ix = t % w, iy = (t / w) % h, ci = (t / (w * h)) % c, b = t / (w * h * c);
    int rowsPer = c * kh * kw, colsPer = oh * ow;
    float acc = 0.0f;
    for (int ki = 0; ki < kh; ki++) {
        int ty = iy + ph - ki;
        if (ty < 0 || ty % sh != 0) continue;
        int oy = ty / sh;
        if (oy >= oh) continue;
        for (int kj = 0; kj < kw; kj++) {
            int tx = ix + pw - kj;
            if (tx < 0 || tx % sw != 0) continue;
            int ox = tx / sw;
            if (ox >= ow) continue;
            int r = (ci * kh + ki) * kw + kj;
            acc += cols[(b * rowsPer + r) * colsPer + oy * ow + ox];
        }
    }
    x[t] += acc;
}

__kernel void maxpool(__global const float* x, __global float* y, __global float* idx, int n, int c, int h, int w,
                      int kh, int kw, int sh, int sw, int ph, int pw, int oh, int ow) {
    int t = get_global_id(0);
    if (t >= n * c * oh * ow) return;
    int ox = t % ow, oy = (t / ow) % oh, plane = t / (ow * oh);
    __global const float* src = x + plane * h * w;
    float best = -INFINITY;
    int bi = -1;
    for (int ki = 0; ki < kh; ki++) {
        int iy = oy * sh - ph + ki;
        if (iy < 0 || iy >= h) continue;
        for (int kj = 0; kj < kw; kj++) {
            int ix = ox * sw - pw + kj;
            if (ix < 0 || ix >= w) continue;
            float v = src[iy * w + ix];
            if (bi < 0 || v > best) { best = v; bi = iy * w + ix; }
        }
    }
    y[t] = best;
    idx[t] = (float)bi;
}

__kernel void maxpool_back(__global const float* g, __global const float* idx, __global float* dx, int n, int c, int h, int w,
                           int oh, int ow) {
    int t = get_global_id(0);
    if (t >= n * c * oh * ow) return;
    int plane = t / (ow * oh);
    int i = (int)idx[t];
    if (i >= 0) atomic_add_f(&dx[plane * h * w + i], g[t]);
}

uint hash32(uint x) {
    x ^= x >> 16;
    x *= 0x7feb352du;
    x ^= x >> 15;
    x *= 0x846ca68bu;
    x ^= x >> 16;
    return x;
}

float uniform01(uint i, uint lo, uint hi) {
    uint r = hash32(hash32(i + lo) ^ hi);
    return ((float)(r >> 8) + 1.0f) * (1.0f / 16777216.0f);
}

__kernel void random_fill(__global float* d, int n, uint lo, uint hi, int normal) {
    int i = get_global_id(0);
    if (i >= n) return;
    if (!normal) {
        d[i] = uniform01((uint)i, lo, hi) - 1.0f / 16777216.0f;
        return;
    }
    float u1 = uniform01((uint)(2 * i), lo, hi);
    float u2 = uniform01((uint)(2 * i + 1), lo, hi);
    d[i] = sqrt(-2.0f * log(u1)) * cos(6.283185307179586f * u2);
}

__kernel void dropout(__global const float* x, __global float* y, int n, uint lo, uint hi, float p, float scale) {
    int i = get_global_id(0);
    if (i >= n) return;
    y[i] = uniform01((uint)i, lo, hi) - 1.0f / 16777216.0f >= p ? x[i] * scale : 0.0f;
}

__kernel void adam_step(__global float* p, __global const float* g, __global float* m, __global float* v, int n,
                        float lr, float b1, float b2, float eps, float wd, float bc1, float bc2, int decoupled) {
    int i = get_global_id(0);
    if (i >= n) return;
    float gi = g[i];
    float pi = p[i];
    if (decoupled) pi -= lr * wd * pi; else gi += wd * pi;
    float mi = b1 * m[i] + (1.0f - b1) * gi;
    float vi = b2 * v[i] + (1.0f - b2) * gi * gi;
    m[i] = mi;
    v[i] = vi;
    p[i] = pi - lr * (mi / bc1) / (sqrt(vi / bc2) + eps);
}

__kernel void sgd_step(__global float* p, __global const float* g, __global float* buf, int n,
                       float lr, float momentum, float wd, int usebuf) {
    int i = get_global_id(0);
    if (i >= n) return;
    float gi = g[i] + wd * p[i];
    if (usebuf) {
        float b = momentum * buf[i] + gi;
        buf[i] = b;
        gi = b;
    }
    p[i] -= lr * gi;
}
`
