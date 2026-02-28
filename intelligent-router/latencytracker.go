package main

import "sync"

// emaAlpha is the smoothing factor for exponential moving average updates.
// A value of 0.2 weights new observations at 20% and history at 80%.
const emaAlpha = 0.2

// LatencyTracker maintains a per-model exponential moving average of observed
// request latency in milliseconds.  It is safe for concurrent use.
type LatencyTracker struct {
	mu    sync.Mutex
	avgMs map[string]float64
}

// NewLatencyTracker creates a new tracker pre-seeded with the given initial values.
func NewLatencyTracker(initial map[string]float64) *LatencyTracker {
	m := make(map[string]float64, len(initial))
	for k, v := range initial {
		m[k] = v
	}
	return &LatencyTracker{avgMs: m}
}

// EnsureModel initialises a model entry with initialMs only if the model is
// not already tracked.  Safe to call on every config hot-reload.
func (lt *LatencyTracker) EnsureModel(name string, initialMs float64) {
	lt.mu.Lock()
	defer lt.mu.Unlock()
	if _, ok := lt.avgMs[name]; !ok {
		lt.avgMs[name] = initialMs
	}
}

// Record updates the EMA for model using the observed latency in milliseconds.
func (lt *LatencyTracker) Record(model string, ms float64) {
	lt.mu.Lock()
	defer lt.mu.Unlock()
	if prev, ok := lt.avgMs[model]; ok {
		lt.avgMs[model] = emaAlpha*ms + (1-emaAlpha)*prev
	} else {
		lt.avgMs[model] = ms
	}
}

// Snapshot returns a copy of the current latency averages for all tracked models.
func (lt *LatencyTracker) Snapshot() map[string]float64 {
	lt.mu.Lock()
	defer lt.mu.Unlock()
	snap := make(map[string]float64, len(lt.avgMs))
	for k, v := range lt.avgMs {
		snap[k] = v
	}
	return snap
}
