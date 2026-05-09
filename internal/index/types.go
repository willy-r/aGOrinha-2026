package index

const (
	Dims        = 14
	K           = 7
	NumClusters = 1024
	NProbe      = 32
	// VecScale maps float32 → int16: [0,1]→[0,1000], -1 sentinel→-1000.
	// Max squared dist: 14×(2000)²=56,000,000 — fits in int32.
	VecScale = 1000.0
)

// Index is the in-memory IVF index loaded from a binary file.
type Index struct {
	NumVecs   int
	Centroids [NumClusters][Dims]float32 // cluster centroids (float32)
	// Offsets[c]..Offsets[c+1] = vector range for cluster c in Vecs/Labels.
	// int32 for direct C interop via CGo.
	Offsets   [NumClusters + 1]int32
	Vecs      [][16]int16 // dims 0-13 real, 14-15 = 0 padding for 256-bit AVX2 load
	Labels    []uint8     // 0=legit 1=fraud, uint8 for direct C interop
	MCCRisk   map[string]float32
	Responses [K + 1][]byte // pre-computed JSON responses indexed by fraud count (0–5)
}

// FraudRequest mirrors the POST /fraud-score request body.
type FraudRequest struct {
	ID          string        `json:"id"`
	Transaction TxInput       `json:"transaction"`
	Customer    CustomerInput `json:"customer"`
	Merchant    MerchantInput `json:"merchant"`
	Terminal    TerminalInput `json:"terminal"`
	LastTx      *LastTxInput  `json:"last_transaction"`
}

type TxInput struct {
	Amount       float64 `json:"amount"`
	Installments int     `json:"installments"`
	RequestedAt  string  `json:"requested_at"`
}

type CustomerInput struct {
	AvgAmount      float64  `json:"avg_amount"`
	TxCount24h     int      `json:"tx_count_24h"`
	KnownMerchants []string `json:"known_merchants"`
}

type MerchantInput struct {
	ID        string  `json:"id"`
	MCC       string  `json:"mcc"`
	AvgAmount float64 `json:"avg_amount"`
}

type TerminalInput struct {
	IsOnline    bool    `json:"is_online"`
	CardPresent bool    `json:"card_present"`
	KmFromHome  float64 `json:"km_from_home"`
}

type LastTxInput struct {
	Timestamp     string  `json:"timestamp"`
	KmFromCurrent float64 `json:"km_from_current"`
}
