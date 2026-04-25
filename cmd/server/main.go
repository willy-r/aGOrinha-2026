package main

import (
	"gorinha-2026/internal/api"
	"gorinha-2026/internal/config"
	"gorinha-2026/internal/index"
	"log"
	"sync/atomic"

	"github.com/valyala/fasthttp"
)

func main() {
	resourceDir := config.GetEnv("RESOURCES_DIR", "resources")
	listenAddr := config.GetEnv("LISTEN_ADDR", ":9999")

	idx := &index.Index{}
	ready := &atomic.Bool{}

	log.Println("Loading index...")
	if err := index.Load(idx, resourceDir); err != nil {
		log.Fatalf("failed to load index: %v", err)
	}
	log.Printf("Index loaded: %d reference entries", len(idx.Refs))
	ready.Store(true)

	router := api.NewRouter(idx, ready)

	server := &fasthttp.Server{
		Handler:            safeHandler(router.HandleRequest),
		MaxConnsPerIP:      0,
		ReduceMemoryUsage:  false,
	}

	log.Printf("Listening on %s", listenAddr)
	log.Fatal(server.ListenAndServe(listenAddr))
}

func safeHandler(h fasthttp.RequestHandler) fasthttp.RequestHandler {
	return func(ctx *fasthttp.RequestCtx) {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("panic recovered: %v", r)
				ctx.SetStatusCode(fasthttp.StatusInternalServerError)
				ctx.SetBodyString(`{"error":"internal error"}`)
			}
		}()
		h(ctx)
	}
}
