package main

import (
	"testing"
)

func TestKeywordClassifier_OR_Match(t *testing.T) {
	rules := []KeywordRule{
		{Name: "finance", Keywords: []string{"stock", "investment", "portfolio"}, Operator: "OR"},
	}
	kc, err := NewKeywordClassifier(rules)
	if err != nil {
		t.Fatalf("NewKeywordClassifier: %v", err)
	}

	domain, confidence := kc.Classify("I want to invest in stocks")
	if domain != "finance" {
		t.Errorf("expected 'finance', got %q", domain)
	}
	if confidence != 1.0 {
		t.Errorf("expected confidence 1.0, got %f", confidence)
	}
}

func TestKeywordClassifier_OR_NoMatch(t *testing.T) {
	rules := []KeywordRule{
		{Name: "finance", Keywords: []string{"stock", "bond"}, Operator: "OR"},
	}
	kc, _ := NewKeywordClassifier(rules)

	domain, confidence := kc.Classify("I enjoy cooking pasta")
	if domain != "" {
		t.Errorf("expected no domain, got %q", domain)
	}
	if confidence != 0 {
		t.Errorf("expected 0 confidence, got %f", confidence)
	}
}

func TestKeywordClassifier_AND_AllRequired(t *testing.T) {
	rules := []KeywordRule{
		{Name: "algotrading", Keywords: []string{"algorithm", "trading"}, Operator: "AND"},
	}
	kc, _ := NewKeywordClassifier(rules)

	if d, _ := kc.Classify("algorithmic trading strategy"); d != "algotrading" {
		t.Errorf("AND both present: expected 'algotrading', got %q", d)
	}
	if d, _ := kc.Classify("algorithm only"); d != "" {
		t.Errorf("AND one missing: expected no match, got %q", d)
	}
	if d, _ := kc.Classify("trading only"); d != "" {
		t.Errorf("AND one missing: expected no match, got %q", d)
	}
}

func TestKeywordClassifier_NOR_MatchWhenNonePresent(t *testing.T) {
	rules := []KeywordRule{
		{Name: "general", Keywords: []string{"finance", "legal", "medical"}, Operator: "NOR"},
	}
	kc, _ := NewKeywordClassifier(rules)

	if d, _ := kc.Classify("tell me about cooking"); d != "general" {
		t.Errorf("NOR match expected, got %q", d)
	}
	if d, _ := kc.Classify("tell me about finance"); d != "" {
		t.Errorf("NOR should not match when excluded word present, got %q", d)
	}
}

func TestKeywordClassifier_CaseSensitive(t *testing.T) {
	rules := []KeywordRule{
		{Name: "strict", Keywords: []string{"Python"}, Operator: "OR", CaseSensitive: true},
	}
	kc, _ := NewKeywordClassifier(rules)

	if d, _ := kc.Classify("I love Python programming"); d != "strict" {
		t.Errorf("case-sensitive match: expected 'strict', got %q", d)
	}
	if d, _ := kc.Classify("I love python programming"); d != "" {
		t.Errorf("case-sensitive: lowercase should not match, got %q", d)
	}
}

func TestKeywordClassifier_CaseInsensitive(t *testing.T) {
	rules := []KeywordRule{
		{Name: "finance", Keywords: []string{"Stock"}, Operator: "OR", CaseSensitive: false},
	}
	kc, _ := NewKeywordClassifier(rules)

	if d, _ := kc.Classify("buying stock today"); d != "finance" {
		t.Errorf("case-insensitive: expected 'finance', got %q", d)
	}
	if d, _ := kc.Classify("buying STOCK today"); d != "finance" {
		t.Errorf("case-insensitive: expected 'finance' for uppercase, got %q", d)
	}
}

func TestKeywordClassifier_FirstRuleWins(t *testing.T) {
	rules := []KeywordRule{
		{Name: "finance", Keywords: []string{"money"}, Operator: "OR"},
		{Name: "general", Keywords: []string{"money"}, Operator: "OR"},
	}
	kc, _ := NewKeywordClassifier(rules)

	if d, _ := kc.Classify("show me the money"); d != "finance" {
		t.Errorf("first-rule-wins: expected 'finance', got %q", d)
	}
}

func TestKeywordClassifier_WordBoundary(t *testing.T) {
	rules := []KeywordRule{
		{Name: "finance", Keywords: []string{"stock"}, Operator: "OR"},
	}
	kc, _ := NewKeywordClassifier(rules)

	// Word boundary: "stock" should NOT match inside "livestock"
	if d, _ := kc.Classify("he owns livestock"); d != "" {
		t.Errorf("word boundary: 'stock' inside 'livestock' should not match, got %q", d)
	}
	if d, _ := kc.Classify("buy stock now"); d != "finance" {
		t.Errorf("word boundary: standalone 'stock' should match, got %q", d)
	}
}

func TestNewKeywordClassifier_InvalidOperator(t *testing.T) {
	rules := []KeywordRule{
		{Name: "bad", Keywords: []string{"test"}, Operator: "XOR"},
	}
	_, err := NewKeywordClassifier(rules)
	if err == nil {
		t.Error("expected error for invalid operator XOR, got nil")
	}
}

func TestNewKeywordClassifier_EmptyRules(t *testing.T) {
	kc, err := NewKeywordClassifier(nil)
	if err != nil {
		t.Fatalf("unexpected error for empty rules: %v", err)
	}
	if d, c := kc.Classify("anything"); d != "" || c != 0 {
		t.Errorf("empty classifier: expected no match, got domain=%q confidence=%f", d, c)
	}
}
