package index

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Load reads all resource files and populates idx.
// numShards > 1 enables parallel KNN by pre-slicing Refs into sub-views.
func Load(idx *Index, resourceDir string, numShards int) error {
	if err := loadMCCRisk(idx, filepath.Join(resourceDir, "mcc_risk.json")); err != nil {
		return fmt.Errorf("loading mcc_risk: %w", err)
	}
	if err := loadReferences(idx, filepath.Join(resourceDir, "references.json.gz")); err != nil {
		return fmt.Errorf("loading references: %w", err)
	}
	if numShards > 1 {
		shardSize := (len(idx.Refs) + numShards - 1) / numShards
		idx.Shards = make([][]RefEntry, numShards)
		for i := range idx.Shards {
			start := i * shardSize
			end := start + shardSize
			if end > len(idx.Refs) {
				end = len(idx.Refs)
			}
			idx.Shards[i] = idx.Refs[start:end]
		}
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

func loadReferences(idx *Index, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gr.Close()

	idx.Refs = make([]RefEntry, 0, 100_000)

	dec := json.NewDecoder(gr)

	// consume opening '['
	if _, err := dec.Token(); err != nil {
		return fmt.Errorf("expected '[': %w", err)
	}

	type jsonEntry struct {
		Vector [14]float32 `json:"vector"`
		Label  string      `json:"label"`
	}

	var entry jsonEntry
	for dec.More() {
		if err := dec.Decode(&entry); err != nil {
			return err
		}
		idx.Refs = append(idx.Refs, RefEntry{
			V:       entry.Vector,
			IsFraud: entry.Label == "fraud",
		})
	}

	return nil
}
