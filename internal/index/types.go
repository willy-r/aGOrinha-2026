package index

// RefEntry is one labeled reference vector.
// Fixed [14]float32 lets the compiler unroll and SIMD-vectorize the distance loop.
// Padded to 60 bytes so each entry fits in one cache line.
type RefEntry struct {
	V       [14]float32
	IsFraud bool
	_       [3]byte
}

// Index holds all in-memory data loaded at startup.
type Index struct {
	Refs      []RefEntry
	MCCRisk   map[string]float32
	Responses [6][]byte // pre-computed JSON responses indexed by fraud count (0–5)
}

// FraudRequest mirrors the POST /fraud-score request body.
type FraudRequest struct {
	ID          string          `json:"id"`
	Transaction TxInput         `json:"transaction"`
	Customer    CustomerInput   `json:"customer"`
	Merchant    MerchantInput   `json:"merchant"`
	Terminal    TerminalInput   `json:"terminal"`
	LastTx      *LastTxInput    `json:"last_transaction"`
}

type TxInput struct {
	Amount      float64 `json:"amount"`
	Installments int    `json:"installments"`
	RequestedAt string  `json:"requested_at"`
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
