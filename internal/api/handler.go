package api

import (
	"bytes"
	"gorinha-2026/internal/index"
	"sync/atomic"

	json "github.com/goccy/go-json"
	"github.com/valyala/fasthttp"
)

type Router struct {
	idx   *index.Index
	ready *atomic.Bool
}

func NewRouter(idx *index.Index, ready *atomic.Bool) *Router {
	return &Router{idx: idx, ready: ready}
}

func (r *Router) HandleRequest(ctx *fasthttp.RequestCtx) {
	path := ctx.Path()
	switch {
	case bytes.Equal(path, []byte("/fraud-score")):
		if ctx.IsPost() {
			r.handleFraudScore(ctx)
			return
		}
		ctx.SetStatusCode(fasthttp.StatusMethodNotAllowed)
	case bytes.Equal(path, []byte("/ready")):
		r.handleReady(ctx)
	default:
		ctx.SetStatusCode(fasthttp.StatusNotFound)
	}
}

func (r *Router) handleReady(ctx *fasthttp.RequestCtx) {
	if r.ready.Load() {
		ctx.SetStatusCode(fasthttp.StatusOK)
		ctx.SetBodyString("ok")
	} else {
		ctx.SetStatusCode(fasthttp.StatusServiceUnavailable)
		ctx.SetBodyString("loading")
	}
}

func (r *Router) handleFraudScore(ctx *fasthttp.RequestCtx) {
	var req index.FraudRequest
	if err := json.Unmarshal(ctx.PostBody(), &req); err != nil {
		ctx.SetStatusCode(fasthttp.StatusBadRequest)
		ctx.SetContentType("application/json")
		ctx.SetBodyString(`{"error":"invalid json"}`)
		return
	}

	query, err := index.Vectorize(&req, r.idx.MCCRisk)
	if err != nil {
		ctx.SetStatusCode(fasthttp.StatusBadRequest)
		ctx.SetContentType("application/json")
		ctx.SetBodyString(`{"error":"invalid payload"}`)
		return
	}

	fraudCount := index.IVFSearch(r.idx, &query)

	ctx.SetStatusCode(fasthttp.StatusOK)
	ctx.SetContentType("application/json")
	ctx.SetBody(r.idx.Responses[fraudCount])
}
