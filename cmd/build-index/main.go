package main

import (
	"compress/gzip"
	"encoding/json"
	"gorinha-2026/internal/index"
	"log"
	"os"
	"runtime"
)

func main() {
	if len(os.Args) != 3 {
		log.Fatalf("usage: build-index <references.json.gz> <output.bin>")
	}
	inputPath := os.Args[1]
	outputPath := os.Args[2]

	log.Printf("build-index: reading %s using %d CPUs", inputPath, runtime.NumCPU())
	refs := loadRefs(inputPath)
	log.Printf("build-index: loaded %d references", len(refs))

	log.Printf("build-index: running k-means (clusters=%d, nprobe=%d, maxIter=25)…",
		index.NumClusters, index.NProbe)
	if err := index.BuildAndWrite(refs, index.NumClusters, index.NProbe, 25, outputPath); err != nil {
		log.Fatalf("build-index: %v", err)
	}
	log.Printf("build-index: wrote %s", outputPath)
}

func loadRefs(path string) []index.RawRef {
	f, err := os.Open(path)
	if err != nil {
		log.Fatalf("open: %v", err)
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		log.Fatalf("gzip: %v", err)
	}
	defer gr.Close()

	type jsonEntry struct {
		Vector [index.Dims]float32 `json:"vector"`
		Label  string              `json:"label"`
	}

	refs := make([]index.RawRef, 0, 3_000_000)
	dec := json.NewDecoder(gr)

	// consume opening '['
	if _, err := dec.Token(); err != nil {
		log.Fatalf("expected '[': %v", err)
	}
	var entry jsonEntry
	for dec.More() {
		if err := dec.Decode(&entry); err != nil {
			log.Fatalf("decode: %v", err)
		}
		refs = append(refs, index.RawRef{
			V:       entry.Vector,
			IsFraud: entry.Label == "fraud",
		})
	}
	return refs
}
