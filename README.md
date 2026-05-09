# 🐔 aGOrinha 2026

Implementação do desafio da **Rinha de Backend 2026** utilizando **Go (v1.24)**, com foco em detecção de fraudes via busca vetorial de alta performance.

> Repositório oficial da Rinha de Backend: [zanfranceschi/rinha-de-backend-2026](https://github.com/zanfranceschi/rinha-de-backend-2026)

## 🔥 Descrição

Esta solução implementa uma API HTTP para detecção de fraudes em transações de cartão de crédito utilizando **IVF (Inverted File Index)** com **SIMD AVX2** sobre **3 milhões de vetores de referência**.

- `GET /ready`: health check — retorna 2xx quando o serviço está pronto para receber requisições.
- `POST /fraud-score`: recebe os dados de uma transação, calcula um score de fraude e retorna a decisão de aprovação.

O sistema transforma o payload em um **vetor de 14 dimensões**, busca os 7 vizinhos mais próximos via IVF e decide com base na proporção de fraudes entre eles.

## 📁 Estrutura

```bash
gorinha-2026/
├── cmd/
│   ├── server/             # Entrypoint do servidor HTTP
│   └── build-index/        # Ferramenta CLI que constrói o índice IVF binário
├── internal/
│   ├── api/                # Handlers da API (fasthttp)
│   ├── config/             # Leitura de variáveis de ambiente
│   └── index/              # IVF, vetorização, busca AVX2, k-means
│       └── ivf_avx2.c      # Inner loop em C com _mm256_madd_epi16
├── resources/
│   ├── references.json.gz  # 3M vetores de referência rotulados (input do build)
│   ├── mcc_risk.json       # Score de risco por código MCC
│   └── normalization.json  # Constantes de normalização
├── Dockerfile
├── docker-compose.yml
├── nginx.conf
├── go.mod
└── go.sum
```

## ⚙️ Tecnologias Utilizadas

* Linguagem: **Go 1.24** + **C (CGo)** para o inner loop AVX2
* Web server: **fasthttp**
* Persistência: **Em memória** — índice binário (~99 MB) carregado na inicialização
* Load balancer: **NGINX**
* Orquestração: **Docker Compose**

## 🧠 Estratégia de Detecção

### Pipeline de build (tempo de `docker build`)

1. `cmd/build-index` lê `references.json.gz` (3M vetores)
2. Executa **k-means++** com 1024 clusters e 25 iterações para construir o índice IVF
3. Quantiza os vetores de `float32` para `int16` (escala ×1000) — reduz memória de 192 MB para 96 MB
4. Escreve `resources/index.bin` (~99 MB) já embutido na imagem final

### Busca IVF em tempo de execução

```
query float32[14]
  → quantiza para int16[16]  (0-padding para load AVX2 de 256 bits)
  → distância float32 para os 1024 centroides
  → seleciona os 48 clusters mais próximos (NProbe=48)
  → 1 chamada CGo → ivf_cluster_scan() varre ~140K vetores via _mm256_madd_epi16
  → top-7 vizinhos mais próximos → conta fraudes → retorna resposta pré-computada
```

**`_mm256_madd_epi16`** calcula 8 produtos int16 e os soma em int32 em uma única instrução AVX2, habilitando a distância Euclidiana quadrática sem conversão para float.

### Vetorização (14 dimensões)

| Dim | Campo | Fórmula |
|-----|-------|---------|
| 0 | Valor da transação | `clamp(amount / 10000)` |
| 1 | Parcelas | `clamp(installments / 12)` |
| 2 | Valor vs. média do cliente | `clamp((amount / avg_amount) / 10)` |
| 3 | Hora do dia (UTC) | `hour / 23` |
| 4 | Dia da semana | `weekday / 6` (seg=0, dom=6) |
| 5 | Minutos desde a última transação | `clamp(minutos / 1440)` ou **-1 se nula** |
| 6 | Distância da última transação | `clamp(km / 1000)` ou **-1 se nula** |
| 7 | Distância de casa | `clamp(km_from_home / 1000)` |
| 8 | Contagem de transações em 24h | `clamp(tx_count_24h / 20)` |
| 9 | Terminal online | `1` ou `0` |
| 10 | Cartão presente | `1` ou `0` |
| 11 | Merchant desconhecido | `1` ou `0` |
| 12 | Risco do MCC | valor de `mcc_risk.json` (padrão: 0.5) |
| 13 | Valor médio do merchant | `clamp(merchant.avg_amount / 10000)` |

### Decisão

```
fraud_score = fraudes_entre_os_7 / 7
approved    = fraud_count < 2  (≈ 28,6% — limiar Bayes-ótimo para custo FN = 3× FP)
```

## 🧪 Endpoints

### GET /ready

Retorna `200 OK` quando o índice foi carregado e o serviço está pronto.

### POST /fraud-score

**Request:**

```json
{
  "id": "uuid",
  "transaction": { "amount": 500.0, "installments": 1, "requested_at": "2026-04-25T12:00:00Z" },
  "customer": { "avg_amount": 200.0, "tx_count_24h": 3, "known_merchants": ["m1"] },
  "merchant": { "id": "m2", "mcc": "5411", "avg_amount": 300.0 },
  "terminal": { "is_online": true, "card_present": false, "km_from_home": 15.0 },
  "last_transaction": { "timestamp": "2026-04-25T11:50:00Z", "km_from_current": 5.0 }
}
```

**Response:**

```json
{ "approved": false, "fraud_score": 0.4286 }
```

## ⚡ Performance

Medições em i5-13420H:

| Operação | Latência | Alocações |
|----------|----------|-----------|
| `IVFSearch` (NProbe=48) | ~130 µs | ≤2 (overhead CGo) |
| `Vectorize` | ~103 ns | 0 |

## 🚀 Recursos de Infraestrutura

| Serviço | CPU | Memória |
|---------|-----|---------|
| app1    | 0.45 | 165 MB |
| app2    | 0.45 | 165 MB |
| nginx   | 0.10 | 20 MB  |
| **Total** | **1.00** | **350 MB** |
