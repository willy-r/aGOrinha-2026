FROM golang:1.24-alpine AS builder

WORKDIR /app

# gcc + musl-dev for CGo (AVX2 intrinsics in ivf_avx2.c)
RUN apk add --no-cache gcc musl-dev

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build the index builder (CGO_ENABLED=1 because internal/index uses CGo)
RUN CGO_ENABLED=1 \
    GOOS=linux \
    GOARCH=amd64 \
    GOAMD64=v3 \
    go build \
    -ldflags="-s -w" \
    -o build-index \
    ./cmd/build-index

# Build IVF index at Docker build time — k-means runs with all host CPU cores,
# no container limits. The resulting index.bin is baked into the image.
RUN ./build-index ./resources/references.json.gz ./resources/index.bin

# Build the server with CGo enabled for the AVX2 cluster scan
RUN CGO_ENABLED=1 \
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
