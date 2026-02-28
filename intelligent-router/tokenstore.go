package main

import (
	"sync"
	"time"
)

// TokenStore tracks token usage per API key using a sliding time window.
// It is safe for concurrent use from multiple goroutines.
// Design mirrors the custom rate-limiter (example-of-extPRoc-server).
type TokenStore struct {
	mu      sync.Mutex
	entries map[string]*tokenEntry
	window  time.Duration
}

type tokenEntry struct {
	tokens    int64
	windowEnd time.Time
}

// NewTokenStore creates a TokenStore with the given sliding-window duration.
// A window of 0 defaults to 60 seconds.
func NewTokenStore(window time.Duration) *TokenStore {
	if window <= 0 {
		window = 60 * time.Second
	}
	return &TokenStore{
		entries: make(map[string]*tokenEntry),
		window:  window,
	}
}

// getOrCreate returns the token entry for key, resetting it if the window
// has expired.  Must be called with s.mu held.
func (s *TokenStore) getOrCreate(key string) *tokenEntry {
	now := time.Now()
	e, ok := s.entries[key]
	if !ok || now.After(e.windowEnd) {
		e = &tokenEntry{tokens: 0, windowEnd: now.Add(s.window)}
		s.entries[key] = e
	}
	return e
}

// GetTotal returns the current token count for key within the active window.
func (s *TokenStore) GetTotal(key string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getOrCreate(key).tokens
}

// AddTokens adds count tokens for key within the active window.
func (s *TokenStore) AddTokens(key string, count int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.getOrCreate(key).tokens += count
}

// GetPressure returns a value in [0.0, 1.0] representing the routing budget
// pressure for key given the configured thresholds:
//
//	0.0  -> tokens_used < cfg.Threshold              (no pressure)
//	0..1 -> linear ramp: (used - threshold) / (quota - threshold)
//	1.0  -> tokens_used >= cfg.Quota                 (maximum pressure)
//
// Returns 0.0 when cfg.Quota <= 0 (token budget not configured).
func (s *TokenStore) GetPressure(key string, cfg TokenBudgetConfig) float64 {
	if cfg.Quota <= 0 {
		return 0.0
	}
	used := s.GetTotal(key)
	if used < cfg.Threshold {
		return 0.0
	}
	if used >= cfg.Quota {
		return 1.0
	}
	span := cfg.Quota - cfg.Threshold
	if span <= 0 {
		return 1.0
	}
	return float64(used-cfg.Threshold) / float64(span)
}
