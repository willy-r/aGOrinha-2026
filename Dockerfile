FROM golang:1.24 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build the index builder (runs on host during docker build — no CPU limits)
RUN CGO_ENABLED=0 \
    GOOS=linux \
    GOARCH=amd64 \
    GOAMD64=v3 \
    go build \
    -ldflags="-s -w" \
    -o build-index \
    ./cmd/build-index

# Build IVF index from references: k-means clustering baked into the image.
# This runs with all host CPU cores available (no container limits).
RUN ./build-index ./resources/references.json.gz ./resources/index.bin

# Build the server binary
RUN CGO_ENABLED=0 \
    GOOS=linux \
    GOARCH=amd64 \
    GOAMD64=v3 \
    go build \
    -ldflags="-s -w" \
    -o gorinha-be \
    ./cmd/server

FROM alpine:3.21

WORKDIR /app

COPY --from=builder /app/gorinha-be .
COPY --from=builder /app/resources/index.bin ./resources/
COPY --from=builder /app/resources/mcc_risk.json ./resources/

EXPOSE 9999

ENTRYPOINT ["./gorinha-be"]
