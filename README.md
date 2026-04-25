# 🐔 aGOrinha 2026

Implementação do desafio da **Rinha de Backend 2026** utilizando **Go (v1.24)**, com foco em detecção de fraudes via busca vetorial de alta performance.

> Repositório oficial da Rinha de Backend: [zanfranceschi/rinha-de-backend-2026](https://github.com/zanfranceschi/rinha-de-backend-2026)

## 🔥 Descrição

Esta solução implementa uma API HTTP para detecção de fraudes em transações de cartão de crédito utilizando busca pelos **k vizinhos mais próximos (KNN)** em um dataset de referência com **100.000 vetores**.

- `GET /ready`: health check — retorna 2xx quando o serviço está pronto para receber requisições.
- `POST /fraud-score`: recebe os dados de uma transação, calcula um score de fraude e retorna a decisão de aprovação.

O sistema transforma o payload em um **vetor de 14 dimensões**, busca os 5 vizinhos mais próximos no dataset de referência usando **distância Euclidiana**, e decide: `approved = fraud_score < 0.6`.

## 📁 Estrutura

```bash
gorinha-2026/
├── cmd/
│   └── server/             # Entrypoint do servidor HTTP
├── internal/
│   ├── api/                # Handlers da API (fasthttp)
│   ├── config/             # Leitura de variáveis de ambiente
│   └── index/              # Índice em memória, vetorização e busca KNN
├── resources/
│   ├── references.json.gz  # 100k vetores de referência rotulados
│   ├── mcc_risk.json       # Score de risco por código MCC
│   └── normalization.json  # Constantes de normalização
├── Dockerfile
├── docker-compose.yml
├── nginx.conf
├── go.mod
└── go.sum
```

## ⚙️ Tecnologias Utilizadas

* Linguagem: **Go 1.24**
* Web server: **fasthttp**
* Persistência: **Em memória** (índice carregado na inicialização)
* Load balancer: **NGINX**
* Orquestração: **Docker Compose**

## 🧠 Estratégia de Detecção

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

### Busca KNN

* Distância Euclidiana quadrática (sem `sqrt` — preserva a ordenação)
* Top-5 rastreado com array fixo `[5]neighbor` alocado na stack — **zero alocações de heap** no caminho quente
* Arrays `[14]float32` de tamanho fixo permitem ao compilador desenrolar e vetorizar o loop com instruções SIMD (AVX2)

### Decisão

```
fraud_score = fraudes_entre_os_5 / 5
approved    = fraud_score < 0.6
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
{ "approved": false, "fraud_score": 1.0 }
```

## 🚀 Recursos de Infraestrutura

| Serviço | CPU | Memória |
|---------|-----|---------|
| app1    | 0.45 | 165 MB |
| app2    | 0.45 | 165 MB |
| nginx   | 0.10 | 20 MB  |
| **Total** | **1.00** | **350 MB** |
