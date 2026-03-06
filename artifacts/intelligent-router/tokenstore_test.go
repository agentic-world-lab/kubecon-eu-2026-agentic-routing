package main

import (
	"testing"
	"time"
)

func TestTokenStore_InitiallyZero(t *testing.T) {
	store := NewTokenStore(time.Minute)
	if total := store.GetTotal("key1"); total != 0 {
		t.Errorf("expected 0 tokens initially, got %d", total)
	}
}

func TestTokenStore_AddTokens(t *testing.T) {
	store := NewTokenStore(time.Minute)
	store.AddTokens("key1", 100)
	store.AddTokens("key1", 50)
	if total := store.GetTotal("key1"); total != 150 {
		t.Errorf("expected 150, got %d", total)
	}
}

func TestTokenStore_IndependentKeys(t *testing.T) {
	store := NewTokenStore(time.Minute)
	store.AddTokens("alice", 200)
	store.AddTokens("bob", 300)
	if store.GetTotal("alice") != 200 {
		t.Errorf("alice: want 200, got %d", store.GetTotal("alice"))
	}
	if store.GetTotal("bob") != 300 {
		t.Errorf("bob: want 300, got %d", store.GetTotal("bob"))
	}
}

func TestTokenStore_WindowExpiry(t *testing.T) {
	store := NewTokenStore(50 * time.Millisecond)
	store.AddTokens("key1", 500)
	if store.GetTotal("key1") != 500 {
		t.Fatalf("expected 500 before expiry")
	}
	time.Sleep(100 * time.Millisecond)
	if total := store.GetTotal("key1"); total != 0 {
		t.Errorf("expected 0 after window expiry, got %d", total)
	}
}

func TestTokenStore_GetPressure_BelowThreshold(t *testing.T) {
	store := NewTokenStore(time.Minute)
	store.AddTokens("key1", 100)
	cfg := TokenBudgetConfig{Threshold: 500, Quota: 1000, WindowSeconds: 60}
	if p := store.GetPressure("key1", cfg); p != 0.0 {
		t.Errorf("expected 0 pressure below threshold, got %f", p)
	}
}

func TestTokenStore_GetPressure_AtThreshold(t *testing.T) {
	store := NewTokenStore(time.Minute)
	store.AddTokens("key1", 500)
	cfg := TokenBudgetConfig{Threshold: 500, Quota: 1000}
	if p := store.GetPressure("key1", cfg); p != 0.0 {
		t.Errorf("expected 0 pressure at threshold, got %f", p)
	}
}

func TestTokenStore_GetPressure_Midpoint(t *testing.T) {
	store := NewTokenStore(time.Minute)
	store.AddTokens("key1", 750)
	cfg := TokenBudgetConfig{Threshold: 500, Quota: 1000}
	p := store.GetPressure("key1", cfg)
	if absFloat(p-0.5) > 1e-9 {
		t.Errorf("expected 0.5 pressure at midpoint, got %f", p)
	}
}

func TestTokenStore_GetPressure_AtQuota(t *testing.T) {
	store := NewTokenStore(time.Minute)
	store.AddTokens("key1", 1000)
	cfg := TokenBudgetConfig{Threshold: 500, Quota: 1000}
	if p := store.GetPressure("key1", cfg); p != 1.0 {
		t.Errorf("expected 1.0 pressure at quota, got %f", p)
	}
}

func TestTokenStore_GetPressure_AboveQuota(t *testing.T) {
	store := NewTokenStore(time.Minute)
	store.AddTokens("key1", 2000)
	cfg := TokenBudgetConfig{Threshold: 500, Quota: 1000}
	if p := store.GetPressure("key1", cfg); p != 1.0 {
		t.Errorf("expected 1.0 pressure above quota, got %f", p)
	}
}

func TestTokenStore_GetPressure_NotConfigured(t *testing.T) {
	store := NewTokenStore(time.Minute)
	store.AddTokens("key1", 9999)
	cfg := TokenBudgetConfig{Threshold: 0, Quota: 0}
	if p := store.GetPressure("key1", cfg); p != 0.0 {
		t.Errorf("expected 0 pressure when quota=0, got %f", p)
	}
}

func absFloat(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
