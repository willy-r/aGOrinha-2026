# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Build the IVF index (requires references.json.gz; runs k-means, outputs resources/index.bin)
go run ./cmd/build-index resources/references.json.gz resources/index.bin

# Build the server (CGo required — needs gcc)
CGO_ENABLED=1 GOAMD64=v3 go build -o gorinha-be ./cmd/server

# Run (loads resources/ dir relative to CWD)
./gorinha-be

# Test
go test -v ./internal/index

# Benchmarks
go test -bench=. -benchmem ./internal/index

# Full Docker build (runs k-means at build time — ~2-5 min)
docker compose up --build
```

No Makefile exists. No linter is configured.

## Environment Variables

| Variable        | Default          | Purpose                              |
|----------------|------------------|--------------------------------------|
| `RESOURCES_DIR` | `resources`      | Path to the resources directory      |
| `LISTEN_ADDR`   | `0.0.0.0:9999`  | HTTP listen address                  |
| `GOGC`          | Go default       | GC tuning (docker-compose sets 400)  |
| `GOMEMLIMIT`    | Go default       | Memory cap (docker-compose: 150MiB)  |

## Architecture

This is a Go fraud-detection microservice for Rinha de Backend 2026. It loads a pre-built IVF
index at startup, then serves a single endpoint — `POST /fraud-score` — that classifies
transactions via 7-nearest-neighbor search over 3M reference vectors.

**Build pipeline:**
1. `cmd/build-index` reads `resources/references.json.gz` (3M labeled vectors)
2. Runs k-means++ clustering (1024 clusters, 25 iterations) to build an IVF index
3. Writes binary `resources/index.bin` (~99 MB) — this happens at `docker build` time
4. The final Docker image contains only `gorinha-be` + `index.bin` + `mcc_risk.json`

**Request lifecycle:**
1. Parse JSON body into `FraudRequest`
2. `Vectorize()` → 14D `[14]float32` vector (fixed-size enables SIMD auto-vectorization)
3. `IVFSearch()` → quantize query to int16 → compute distance to 1024 centroids → select 48
   nearest clusters → call C AVX2 function to scan those clusters → return fraud count
4. Return pre-computed JSON response indexed by fraud count (0–7)

**Deployment topology:**
- Two identical Go app replicas (`app1`, `app2`), each using 0.45 CPU and 165 MB
- NGINX load balancer in front (round-robin, keep-alive pooling), using 0.10 CPU and 20 MB
- Total budget: 1.00 CPU, 350 MB

## Performance-Critical Invariants

The hot path in `internal/index/search.go` is zero-allocation (or ≤2 allocs from CGo overhead).

- **Fixed-size arrays everywhere** — `[14]float32` query vectors, `[16]int16` quantized vectors
  (2 elements zero-padded for AVX2), `[NProbe]int32` probe list — all stack-allocated.
- **No `math.Sqrt`** — squared Euclidean distance preserves ordering in both float32 (centroid
  scan) and int16 (AVX2 cluster scan).
- **int16 quantization** — float32 → int16 (scaled by VecScale=1000) halves memory from 192 MB
  to 96 MB, fitting within the 165 MB per-replica limit. Enables `_mm256_madd_epi16`.
- **`_mm256_madd_epi16`** — computes 8 int16-pair products and adds them into int32 in one
  instruction. Horizontal reduction takes 3 `hadd` ops. Defined in `internal/index/ivf_avx2.c`.
- **Single CGo call per request** — all cluster IDs are gathered in Go, then one `ivf_cluster_scan`
  call scans all probed clusters in C. Avoids the per-cluster CGo overhead.
- **Pre-computed responses** — `PrecomputeResponses()` builds all 8 possible JSON outcomes at
  startup. `IVFSearch()` returns an integer fraud count to index into this array.

Benchmark before changing anything in `search.go`. `TestIVFSearchAllocations` enforces ≤2 allocs/op.

## IVF Parameters

All in `internal/index/types.go`:

| Constant | Value | Notes |
|----------|-------|-------|
| `NumClusters` | 1024 | ≈sqrt(3M); ~2930 vecs/cluster average |
| `NProbe` | 48 | 4.7% of clusters; trade-off: recall vs. scan volume |
| `K` | 7 | Neighbors; `approved = fraud_count < 2` (≈28.6% threshold) |
| `VecScale` | 1000 | float32 [0,1] → int16 [0,1000]; max sq dist 14×2000²=56M (fits int32) |

## Vectorization Schema (14 dimensions)

All values normalized to [0, 1] via `clamp()`, except dims 5 and 6 which use -1 to signal
missing data (null `last_transaction`). If `last_transaction.timestamp` fails to parse, dims
5 and 6 also fall back to -1.

| Dim | Meaning                         | Formula                            |
|-----|---------------------------------|------------------------------------|
| 0   | Transaction amount              | `clamp(amount / 10000)`            |
| 1   | Installments                    | `clamp(installments / 12)`         |
| 2   | Amount vs. customer average     | `clamp((amount / avg_amount) / 10)`|
| 3   | Hour of day (UTC)               | `hour / 23`                        |
| 4   | Day of week (Mon=0)             | `weekday / 6`                      |
| 5   | Minutes since last transaction  | `clamp(minutes / 1440)` or **-1** |
| 6   | Distance from last (km)         | `clamp(km / 1000)` or **-1**      |
| 7   | Distance from home (km)         | `clamp(km / 1000)`                 |
| 8   | Transaction count in 24h        | `clamp(count / 20)`                |
| 9   | Terminal online                 | 1 / 0                              |
| 10  | Card present                    | 1 / 0                              |
| 11  | Unknown merchant                | 1 / 0                              |
| 12  | MCC risk score                  | lookup from `mcc_risk.json` (default 0.5) |
| 13  | Merchant average amount         | `clamp(avg_amount / 10000)`        |
