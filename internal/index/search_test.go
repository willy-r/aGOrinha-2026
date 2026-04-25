package index

import (
	"testing"
)

func BenchmarkKNNSearch(b *testing.B) {
	idx := &Index{}
	if err := Load(idx, "../../resources"); err != nil {
		b.Fatalf("load: %v", err)
	}
	query := [14]float32{0.5, 0.5, 0.5, 0.5, 0.5, 0.5, 0.5, 0.5, 0.5, 1, 1, 1, 0.5, 0.5}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		KNNSearch(idx, &query)
	}
}

func BenchmarkVectorize(b *testing.B) {
	mccRisk := map[string]float32{"5411": 0.15}
	req := &FraudRequest{
		Transaction: TxInput{Amount: 500, Installments: 1, RequestedAt: "2026-04-25T12:00:00Z"},
		Customer:    CustomerInput{AvgAmount: 200, TxCount24h: 3, KnownMerchants: []string{"m1"}},
		Merchant:    MerchantInput{ID: "m2", MCC: "5411", AvgAmount: 300},
		Terminal:    TerminalInput{IsOnline: true, CardPresent: false, KmFromHome: 15},
		LastTx:      &LastTxInput{Timestamp: "2026-04-25T11:50:00Z", KmFromCurrent: 5},
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Vectorize(req, mccRisk)
	}
}

func TestVectorizeNullLastTx(t *testing.T) {
	req := &FraudRequest{
		Transaction: TxInput{Amount: 100, Installments: 1, RequestedAt: "2026-04-25T12:00:00Z"},
		Customer:    CustomerInput{AvgAmount: 100, TxCount24h: 1, KnownMerchants: []string{}},
		Merchant:    MerchantInput{ID: "m1", MCC: "5999", AvgAmount: 100},
		Terminal:    TerminalInput{IsOnline: false, CardPresent: true, KmFromHome: 0},
		LastTx:      nil,
	}
	v, err := Vectorize(req, map[string]float32{"5999": 0.5})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v[5] != -1 {
		t.Errorf("dim[5] = %v, want -1 for null last_tx", v[5])
	}
	if v[6] != -1 {
		t.Errorf("dim[6] = %v, want -1 for null last_tx", v[6])
	}
}

func TestVectorizeClamping(t *testing.T) {
	// requested_at is far future vs last_tx to exceed 1440 minutes
	req := &FraudRequest{
		Transaction: TxInput{Amount: 99999, Installments: 100, RequestedAt: "2026-04-26T12:00:00Z"},
		Customer:    CustomerInput{AvgAmount: 1, TxCount24h: 999, KnownMerchants: []string{}},
		Merchant:    MerchantInput{ID: "x", MCC: "unknown", AvgAmount: 99999},
		Terminal:    TerminalInput{IsOnline: true, CardPresent: true, KmFromHome: 99999},
		LastTx:      &LastTxInput{Timestamp: "2026-04-25T11:50:00Z", KmFromCurrent: 99999},
	}
	v, err := Vectorize(req, map[string]float32{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// dims 0,1,2,5,6,7,8,13 should all be clamped to 1.0
	clamped := []int{0, 1, 2, 5, 6, 7, 8, 13}
	for _, i := range clamped {
		if v[i] != 1.0 {
			t.Errorf("dim[%d] = %v, want 1.0 after clamp", i, v[i])
		}
	}
}

func TestKNNSearchZeroAlloc(t *testing.T) {
	idx := &Index{}
	if err := Load(idx, "../../resources"); err != nil {
		t.Fatalf("load: %v", err)
	}
	query := [14]float32{0.1, 0.1, 0.1, 0.1, 0.1, -1, -1, 0.1, 0.1, 0, 1, 0, 0.15, 0.1}
	result := testing.AllocsPerRun(100, func() {
		KNNSearch(idx, &query)
	})
	if result != 0 {
		t.Errorf("KNNSearch allocates %v, want 0", result)
	}
}
