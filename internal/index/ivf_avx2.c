#include <immintrin.h>
#include <stdint.h>
#include <limits.h>

/*
 * sq_dist16: squared Euclidean distance between two 16-element int16 vectors.
 *
 * Dims 0-13 carry real data; 14-15 are zero padding so we fill one 256-bit
 * AVX2 register exactly.  With VecScale=1000 the maximum result is
 * 14 × (2000)² = 56,000,000 which fits in int32 with room to spare.
 *
 * _mm256_madd_epi16(diff, diff) multiplies adjacent int16 pairs and adds them
 * into int32, so it computes [d0²+d1², d2²+d3², …] — exactly what we need
 * for a horizontal squared-distance reduction.
 */
static inline int32_t
sq_dist16(const int16_t *restrict a, const int16_t *restrict b)
{
    __m256i va   = _mm256_loadu_si256((const __m256i *)a);
    __m256i vb   = _mm256_loadu_si256((const __m256i *)b);
    __m256i diff = _mm256_sub_epi16(va, vb);
    __m256i sq   = _mm256_madd_epi16(diff, diff); /* 8 × int32 */

    /* horizontal sum: 8 → 4 → 2 → 1 int32 */
    __m128i lo  = _mm256_castsi256_si128(sq);
    __m128i hi  = _mm256_extracti128_si256(sq, 1);
    __m128i s   = _mm_add_epi32(lo, hi);
    s = _mm_hadd_epi32(s, s);
    s = _mm_hadd_epi32(s, s);
    return _mm_cvtsi128_si32(s);
}

/*
 * ivf_cluster_scan: scan the given probe clusters and return the number of
 * fraud labels among the K nearest neighbors found.
 *
 * vecs    – all vectors sorted by cluster, each [16]int16 (stride = 32 bytes)
 * labels  – per-vector fraud flag (0=legit, 1=fraud), same order as vecs
 * offsets – cluster boundary array; cluster c spans [offsets[c], offsets[c+1])
 * probes  – cluster IDs to probe, length nprobe
 * query   – quantized query vector [16]int16
 * k       – number of nearest neighbours (always 5)
 */
int ivf_cluster_scan(
    const int16_t *vecs,
    const uint8_t *labels,
    const int32_t *offsets,
    const int32_t *probes,
    int            nprobe,
    const int16_t *query,
    int            k)
{
    int32_t top_dist[7]   = {INT32_MAX, INT32_MAX, INT32_MAX, INT32_MAX, INT32_MAX, INT32_MAX, INT32_MAX};
    uint8_t top_labels[7] = {0, 0, 0, 0, 0, 0, 0};
    int32_t max_dist = INT32_MAX;
    int     max_idx  = 0;

    for (int p = 0; p < nprobe; p++) {
        int32_t cid   = probes[p];
        int32_t start = offsets[cid];
        int32_t end   = offsets[cid + 1];
        int     n     = end - start;

        const int16_t *cvecs   = vecs   + (size_t)start * 16;
        const uint8_t *clabels = labels + start;

        for (int i = 0; i < n; i++) {
            int32_t d = sq_dist16(cvecs + (size_t)i * 16, query);
            if (d < max_dist) {
                top_dist[max_idx]   = d;
                top_labels[max_idx] = clabels[i];

                /* find the new worst slot in the heap */
                max_dist = top_dist[0];
                max_idx  = 0;
                for (int j = 1; j < k; j++) {
                    if (top_dist[j] > max_dist) {
                        max_dist = top_dist[j];
                        max_idx  = j;
                    }
                }
            }
        }
    }

    int fraud = 0;
    for (int i = 0; i < k; i++)
        fraud += top_labels[i];
    return fraud;
}
