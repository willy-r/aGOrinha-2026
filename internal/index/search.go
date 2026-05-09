package index

/*
#cgo CFLAGS: -O3 -march=haswell -mavx2
#include <stdint.h>

int ivf_cluster_scan(
    const int16_t *vecs,
    const uint8_t *labels,
    const int32_t *offsets,
    const int32_t *probes,
    int            nprobe,
    const int16_t *query,
    int            k
);
*/
import "C"
import (
	"fmt"
	"math"
	"slices"
	"time"
	"unsafe"
)

func clamp(v float32) float32 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

type centroidDist struct {
	dist float32
	id   int
}

// IVFSearch finds the K nearest neighbors via IVF and returns the fraud count.
//
// Hot path breakdown (all stack-allocated, zero heap allocations):
//  1. Quantize query float32 → int16  [16]int16 on stack
//  2. Compute float32 dist to all centroids  [NumClusters]centroidDist on stack (~16 KB)
//  3. Partial-select the NProbe nearest centroids  [NProbe]int32 on stack
//  4. One CGo call into the AVX2 C function that scans the probed clusters
func IVFSearch(idx *Index, query *[Dims]float32) int {
	// 1. Quantize query to int16, padded to 16 elements for the AVX2 load.
	var qInt16 [16]int16 // elements 14-15 stay 0
	for i, f := range query {
		qInt16[i] = int16(math.Round(float64(f) * VecScale))
	}

	// 2. Compute distance from query to every centroid (float32, trivial CPU cost).
	var cDists [NumClusters]centroidDist
	for c := range NumClusters {
		var d float32
		for i := range Dims {
			diff := query[i] - idx.Centroids[c][i]
			d += diff * diff
		}
		cDists[c] = centroidDist{d, c}
	}

	// 3. Partial selection — move the NProbe nearest centroids to the front.
	partialMinSelect(&cDists, NProbe)

	var probes [NProbe]int32
	for p := range NProbe {
		probes[p] = int32(cDists[p].id)
	}

	// 4. AVX2 cluster scan in C — one CGo call for the whole search.
	return int(C.ivf_cluster_scan(
		(*C.int16_t)(unsafe.Pointer(&idx.Vecs[0])),
		(*C.uint8_t)(unsafe.Pointer(&idx.Labels[0])),
		(*C.int32_t)(unsafe.Pointer(&idx.Offsets[0])),
		(*C.int32_t)(unsafe.Pointer(&probes[0])),
		C.int(NProbe),
		(*C.int16_t)(unsafe.Pointer(&qInt16[0])),
		C.int(K),
	))
}

// partialMinSelect rearranges arr so the first n elements are the n smallest.
// O(NumClusters × n) — for NumClusters=1024 and n=32 this is ~32 K comparisons.
func partialMinSelect(arr *[NumClusters]centroidDist, n int) {
	for i := range n {
		minIdx := i
		for j := i + 1; j < NumClusters; j++ {
			if arr[j].dist < arr[minIdx].dist {
				minIdx = j
			}
		}
		arr[i], arr[minIdx] = arr[minIdx], arr[i]
	}
}

// PrecomputeResponses builds K+1 pre-computed JSON responses indexed by fraud count.
// Threshold: fraud if ≥2/K neighbors are fraud (≈0.286 ≈ Bayes-optimal given FN costs 3× FP).
func PrecomputeResponses(idx *Index) {
	for i := range K + 1 {
		score := float64(i) / float64(K)
		approved := i < 2
		idx.Responses[i] = fmt.Appendf(nil, `{"approved":%t,"fraud_score":%.4f}`, approved, score)
	}
}

// Vectorize converts a FraudRequest into a 14D float32 query vector.
func Vectorize(req *FraudRequest, mccRisk map[string]float32) ([Dims]float32, error) {
	var v [Dims]float32

	reqAt, err := parseTime(req.Transaction.RequestedAt)
	if err != nil {
		return v, fmt.Errorf("invalid requested_at: %w", err)
	}

	v[0] = clamp(float32(req.Transaction.Amount) / 10000)
	v[1] = clamp(float32(req.Transaction.Installments) / 12)

	if req.Customer.AvgAmount > 0 {
		v[2] = clamp(float32(req.Transaction.Amount/req.Customer.AvgAmount) / 10)
	} else {
		v[2] = 1.0
	}

	v[3] = float32(reqAt.Hour()) / 23.0
	// time.Weekday: Sunday=0..Saturday=6; challenge uses Monday=0..Sunday=6
	v[4] = float32((int(reqAt.Weekday())+6)%7) / 6.0

	if req.LastTx != nil {
		if lastAt, err := parseTime(req.LastTx.Timestamp); err == nil {
			minutesSince := reqAt.Sub(lastAt).Minutes()
			v[5] = clamp(float32(minutesSince) / 1440)
			v[6] = clamp(float32(req.LastTx.KmFromCurrent) / 1000)
		} else {
			v[5] = -1
			v[6] = -1
		}
	} else {
		v[5] = -1
		v[6] = -1
	}

	v[7] = clamp(float32(req.Terminal.KmFromHome) / 1000)
	v[8] = clamp(float32(req.Customer.TxCount24h) / 20)

	if req.Terminal.IsOnline {
		v[9] = 1
	}
	if req.Terminal.CardPresent {
		v[10] = 1
	}

	if slices.Contains(req.Customer.KnownMerchants, req.Merchant.ID) {
		v[11] = 0
	} else {
		v[11] = 1
	}

	mccVal, ok := mccRisk[req.Merchant.MCC]
	if !ok {
		mccVal = 0.5
	}
	v[12] = mccVal

	v[13] = clamp(float32(req.Merchant.AvgAmount) / 10000)

	return v, nil
}

func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t, err = time.Parse(time.RFC3339Nano, s)
	}
	return t.UTC(), err
}
