package index

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"unsafe"
)

// Load reads the binary IVF index and the MCC risk table from resourceDir.
func Load(idx *Index, resourceDir string) error {
	if err := loadMCCRisk(idx, filepath.Join(resourceDir, "mcc_risk.json")); err != nil {
		return fmt.Errorf("loading mcc_risk: %w", err)
	}
	if err := loadBinaryIndex(idx, filepath.Join(resourceDir, "index.bin")); err != nil {
		return fmt.Errorf("loading index.bin: %w", err)
	}
	PrecomputeResponses(idx)
	return nil
}

func loadMCCRisk(idx *Index, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &idx.MCCRisk)
}

func loadBinaryIndex(idx *Index, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	// Header
	var magic [4]byte
	if _, err := io.ReadFull(f, magic[:]); err != nil {
		return fmt.Errorf("reading magic: %w", err)
	}
	if string(magic[:]) != binMagic {
		return fmt.Errorf("invalid magic %q, want %q", magic, binMagic)
	}

	var numVecs, numClusters, dims, nProbe uint32
	for _, p := range []*uint32{&numVecs, &numClusters, &dims, &nProbe} {
		if err := binary.Read(f, binary.LittleEndian, p); err != nil {
			return err
		}
	}
	if int(numClusters) != NumClusters || int(dims) != Dims {
		return fmt.Errorf("index mismatch: clusters=%d dims=%d, want %d/%d", numClusters, dims, NumClusters, Dims)
	}
	idx.NumVecs = int(numVecs)

	// Centroids: NumClusters × Dims float32
	if err := binary.Read(f, binary.LittleEndian, &idx.Centroids); err != nil {
		return fmt.Errorf("reading centroids: %w", err)
	}

	// Offsets: (NumClusters+1) uint32
	var rawOffsets [NumClusters + 1]uint32
	if err := binary.Read(f, binary.LittleEndian, &rawOffsets); err != nil {
		return fmt.Errorf("reading offsets: %w", err)
	}
	for i, o := range rawOffsets {
		idx.Offsets[i] = int32(o)
	}

	// Vectors: numVecs × 16 int16 (32 bytes each, padded for AVX2)
	idx.Vecs = make([][16]int16, numVecs)
	vecsBytes := unsafe.Slice((*byte)(unsafe.Pointer(&idx.Vecs[0])), int(numVecs)*16*2)
	if _, err := io.ReadFull(f, vecsBytes); err != nil {
		return fmt.Errorf("reading vectors: %w", err)
	}

	// Labels: numVecs uint8 (0=legit, 1=fraud)
	idx.Labels = make([]uint8, numVecs)
	if _, err := io.ReadFull(f, idx.Labels); err != nil {
		return fmt.Errorf("reading labels: %w", err)
	}

	return nil
}
