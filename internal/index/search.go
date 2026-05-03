package index

import (
	"fmt"
	"math"
	"time"
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

// squaredDist computes squared Euclidean distance between two int16 vectors.
// With VecScale=1000 the max result is 14×(2000)²=56,000,000 — fits in int32.
func squaredDist(a, b *[Dims]int16) int32 {
	var sum int32
	for i := 0; i < Dims; i++ {
		d := int32(a[i]) - int32(b[i])
		sum += d * d
	}
	return sum
}

// quantize maps a float32 vector to int16: [0,1]→[0,1000], -1 sentinel→-1000.
func quantize(v *[Dims]float32) [Dims]int16 {
	var q [Dims]int16
	for i, f := range v {
		q[i] = int16(math.Round(float64(f) * VecScale))
	}
	return q
}

type centroidDist struct {
	dist float32
	id   int
}

type neighbor struct {
	distSq  int32
	isFraud bool
}

// IVFSearch finds the K nearest neighbors via IVF: probe NProbe nearest clusters,
// scan their vectors, return fraud count. All temporaries are stack-allocated.
func IVFSearch(idx *Index, query *[Dims]float32) int {
	qInt16 := quantize(query)

	// Compute float32 distance from query to every centroid (stack array, 2 KB).
	var cDists [NumClusters]centroidDist
	for c := range NumClusters {
		var d float32
		for i := range Dims {
			diff := query[i] - idx.Centroids[c][i]
			d += diff * diff
		}
		cDists[c] = centroidDist{d, c}
	}

	// Partial selection: move NProbe smallest to the front without a full sort.
	partialMinSelect(&cDists, NProbe)

	// Scan vectors inside the NProbe nearest clusters.
	var top [K]neighbor
	for i := range top {
		top[i].distSq = math.MaxInt32
	}
	maxDist := int32(math.MaxInt32)
	maxIdx := 0

	for p := range NProbe {
		cid := cDists[p].id
		start := idx.Offsets[cid]
		end := idx.Offsets[cid+1]
		for i := start; i < end; i++ {
			d := squaredDist(&idx.Vecs[i], &qInt16)
			if d < maxDist {
				top[maxIdx] = neighbor{d, idx.Labels[i]}
				maxDist = top[0].distSq
				maxIdx = 0
				for j := 1; j < K; j++ {
					if top[j].distSq > maxDist {
						maxDist = top[j].distSq
						maxIdx = j
					}
				}
			}
		}
	}

	fraudCount := 0
	for _, n := range top {
		if n.isFraud {
			fraudCount++
		}
	}
	return fraudCount
}

// partialMinSelect rearranges cDists so the first n elements are the n smallest.
// O(NumClusters × n) — fast for small n and NumClusters=256.
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
func PrecomputeResponses(idx *Index) {
	scores := [K + 1]float64{0.0, 0.2, 0.4, 0.6, 0.8, 1.0}
	for i, s := range scores {
		idx.Responses[i] = []byte(fmt.Sprintf(`{"approved":%t,"fraud_score":%.1f}`, s < 0.6, s))
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
		lastAt, _ := parseTime(req.LastTx.Timestamp)
		minutesSince := reqAt.Sub(lastAt).Minutes()
		v[5] = clamp(float32(minutesSince) / 1440)
		v[6] = clamp(float32(req.LastTx.KmFromCurrent) / 1000)
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

	isUnknown := float32(1)
	for _, m := range req.Customer.KnownMerchants {
		if m == req.Merchant.ID {
			isUnknown = 0
			break
		}
	}
	v[11] = isUnknown

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
