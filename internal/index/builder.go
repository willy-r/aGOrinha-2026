package index

import (
	"bufio"
	"encoding/binary"
	"log"
	"math"
	"math/rand"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"
)

const binMagic = "IVF1"

// RawRef holds one reference vector and its fraud label from the JSON dataset.
type RawRef struct {
	V       [Dims]float32
	IsFraud bool
}

// BuildAndWrite clusters refs into numClusters via k-means (maxIter iterations),
// then writes a binary IVF index to outPath ready for LoadBinary.
func BuildAndWrite(refs []RawRef, numClusters, nProbe, maxIter int, outPath string) error {
	n := len(refs)
	vecs := make([][Dims]float32, n)
	labels := make([]bool, n)
	for i, r := range refs {
		vecs[i] = r.V
		labels[i] = r.IsFraud
	}

	centroids := kmeansInit(vecs, numClusters)
	assignments := make([]int, n)

	for iter := range maxIter {
		changed := kmeansAssign(vecs, centroids, assignments)
		kmeansUpdate(vecs, assignments, centroids, numClusters)
		log.Printf("k-means iter %d: %d assignments changed", iter+1, changed)
		if changed == 0 {
			break
		}
	}

	// Sort vectors into cluster order.
	counts := make([]int, numClusters)
	for _, a := range assignments {
		counts[a]++
	}
	offsets := make([]int, numClusters+1)
	for c := range numClusters {
		offsets[c+1] = offsets[c] + counts[c]
	}

	sortedVecs := make([][16]int16, n)
	sortedLabels := make([]uint8, n)
	pos := make([]int, numClusters)
	copy(pos, offsets[:numClusters])
	for i, a := range assignments {
		p := pos[a]
		sortedVecs[p] = quantizeVec(vecs[i])
		if labels[i] {
			sortedLabels[p] = 1
		}
		pos[a]++
	}

	return writeBinary(outPath, centroids, offsets, sortedVecs, sortedLabels, numClusters, nProbe, n)
}

// quantizeVec maps float32 dims to int16, padding to 16 elements for AVX2.
func quantizeVec(v [Dims]float32) [16]int16 {
	var q [16]int16 // elements 14-15 stay 0
	for i, f := range v {
		q[i] = int16(math.Round(float64(f) * VecScale))
	}
	return q
}

// kmeansInit uses k-means++ D² sampling for better initial centroids.
// Better spread → tighter clusters → higher recall at same NProbe.
func kmeansInit(vecs [][Dims]float32, k int) [][Dims]float32 {
	n := len(vecs)
	centroids := make([][Dims]float32, 0, k)
	centroids = append(centroids, vecs[rand.Intn(n)])

	minDists := make([]float32, n)
	for i := range minDists {
		minDists[i] = math.MaxFloat32
	}

	for c := 1; c < k; c++ {
		last := &centroids[c-1]
		var total float64
		for i := range vecs {
			if d := sqDistF32(&vecs[i], last); d < minDists[i] {
				minDists[i] = d
			}
			total += float64(minDists[i])
		}
		r := rand.Float64() * total
		var cum float64
		chosen := n - 1
		for i, d := range minDists {
			cum += float64(d)
			if cum >= r {
				chosen = i
				break
			}
		}
		centroids = append(centroids, vecs[chosen])
	}
	return centroids
}

func sqDistF32(a, b *[Dims]float32) float32 {
	var d float32
	for i := range Dims {
		diff := a[i] - b[i]
		d += diff * diff
	}
	return d
}

func kmeansAssign(vecs [][Dims]float32, centroids [][Dims]float32, assignments []int) int64 {
	var changed atomic.Int64
	numWorkers := runtime.NumCPU()
	chunkSize := (len(vecs) + numWorkers - 1) / numWorkers
	var wg sync.WaitGroup
	wg.Add(numWorkers)
	for w := range numWorkers {
		start := w * chunkSize
		end := min(start+chunkSize, len(vecs))
		go func(start, end int) {
			defer wg.Done()
			for i := start; i < end; i++ {
				nearest := nearestCentroid(&vecs[i], centroids)
				if nearest != assignments[i] {
					assignments[i] = nearest
					changed.Add(1)
				}
			}
		}(start, end)
	}
	wg.Wait()
	return changed.Load()
}

func nearestCentroid(v *[Dims]float32, centroids [][Dims]float32) int {
	minDist := float32(math.MaxFloat32)
	minIdx := 0
	for c := range centroids {
		if d := sqDistF32(v, &centroids[c]); d < minDist {
			minDist = d
			minIdx = c
		}
	}
	return minIdx
}

func kmeansUpdate(vecs [][Dims]float32, assignments []int, centroids [][Dims]float32, k int) {
	sums := make([][Dims]float64, k)
	counts := make([]int, k)
	for i, v := range vecs {
		c := assignments[i]
		counts[c]++
		for d, val := range v {
			sums[c][d] += float64(val)
		}
	}
	for c := range k {
		if counts[c] == 0 {
			continue
		}
		for d := range Dims {
			centroids[c][d] = float32(sums[c][d] / float64(counts[c]))
		}
	}
}

func writeBinary(path string, centroids [][Dims]float32, offsets []int, vecs [][16]int16, labels []uint8, numClusters, nProbe, numVecs int) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := bufio.NewWriterSize(f, 1<<20)

	// Header
	w.Write([]byte(binMagic))
	binary.Write(w, binary.LittleEndian, uint32(numVecs))
	binary.Write(w, binary.LittleEndian, uint32(numClusters))
	binary.Write(w, binary.LittleEndian, uint32(Dims))
	binary.Write(w, binary.LittleEndian, uint32(nProbe))

	// Centroids: numClusters × Dims float32
	for _, c := range centroids {
		binary.Write(w, binary.LittleEndian, c)
	}

	// Offsets: (numClusters+1) uint32
	for _, o := range offsets {
		binary.Write(w, binary.LittleEndian, uint32(o))
	}

	// Vectors: numVecs × 16 int16 (32 bytes each, padded for AVX2) — raw bytes
	vecsBytes := unsafe.Slice((*byte)(unsafe.Pointer(&vecs[0])), len(vecs)*16*2)
	w.Write(vecsBytes)

	// Labels: numVecs uint8
	w.Write(labels)

	return w.Flush()
}
