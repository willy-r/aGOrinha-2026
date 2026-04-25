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

// squaredDist computes squared Euclidean distance between two 14D vectors.
// No sqrt needed — ordering is preserved for KNN comparisons.
// Fixed-size pointer args allow the compiler to unroll and vectorize the loop.
func squaredDist(a, b *[14]float32) float32 {
	var sum float32
	for i := 0; i < 14; i++ {
		d := a[i] - b[i]
		sum += d * d
	}
	return sum
}

// Vectorize converts a FraudRequest into a 14D float32 query vector.
func Vectorize(req *FraudRequest, mccRisk map[string]float32) ([14]float32, error) {
	var v [14]float32

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

type neighbor struct {
	distSq  float32
	isFraud bool
}

// KNNSearch returns the count of fraud-labeled entries among the 5 nearest
// neighbors of query. Zero heap allocations — all temporaries are stack-allocated.
func KNNSearch(idx *Index, query *[14]float32) int {
	const K = 5

	var top [K]neighbor
	for i := range top {
		top[i].distSq = math.MaxFloat32
	}
	maxDist := float32(math.MaxFloat32)
	maxIdx := 0

	for i := range idx.Refs {
		d := squaredDist(&idx.Refs[i].V, query)
		if d < maxDist {
			top[maxIdx] = neighbor{d, idx.Refs[i].IsFraud}
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

	count := 0
	for i := range top {
		if top[i].isFraud {
			count++
		}
	}
	return count
}

// PrecomputeResponses builds the 6 JSON response byte slices indexed by fraud count (0–5).
func PrecomputeResponses(idx *Index) {
	scores := [6]float64{0.0, 0.2, 0.4, 0.6, 0.8, 1.0}
	for i, s := range scores {
		idx.Responses[i] = []byte(fmt.Sprintf(`{"approved":%t,"fraud_score":%.1f}`, s < 0.6, s))
	}
}

func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t, err = time.Parse(time.RFC3339Nano, s)
	}
	return t.UTC(), err
}
