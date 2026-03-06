package main

import "sync"

const slidingWindowSize = 10

// LatencyTracker maintains a per-model sliding window of the last N observed
// request latencies in milliseconds. Snapshot() returns the arithmetic mean
// of each model's history. It is safe for concurrent use.
type LatencyTracker struct {
	mu      sync.Mutex
	history map[string][]float64
}

// NewLatencyTracker creates a new tracker pre-seeded with the given initial values.
func NewLatencyTracker(initial map[string]float64) *LatencyTracker {
	h := make(map[string][]float64, len(initial))
	for k, v := range initial {
		h[k] = []float64{v}
	}
	return &LatencyTracker{history: h}
}

// EnsureModel initialises a model entry with initialMs only if the model is
// not already tracked. Safe to call on every config hot-reload.
func (lt *LatencyTracker) EnsureModel(name string, initialMs float64) {
	lt.mu.Lock()
	defer lt.mu.Unlock()
	if _, ok := lt.history[name]; !ok {
		lt.history[name] = []float64{initialMs}
	}
}

// Record appends a latency observation for model. Only the last slidingWindowSize
// observations are retained.
func (lt *LatencyTracker) Record(model string, ms float64) {
	lt.mu.Lock()
	defer lt.mu.Unlock()
	h := append(lt.history[model], ms)
	if len(h) > slidingWindowSize {
		h = h[len(h)-slidingWindowSize:]
	}
	lt.history[model] = h
}

// Snapshot returns the arithmetic mean latency for all tracked models.
func (lt *LatencyTracker) Snapshot() map[string]float64 {
	lt.mu.Lock()
	defer lt.mu.Unlock()
	snap := make(map[string]float64, len(lt.history))
	for k, vals := range lt.history {
		if len(vals) == 0 {
			continue
		}
		var sum float64
		for _, v := range vals {
			sum += v
		}
		snap[k] = sum / float64(len(vals))
	}
	return snap
}
