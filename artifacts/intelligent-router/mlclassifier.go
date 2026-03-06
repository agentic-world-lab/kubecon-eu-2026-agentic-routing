//go:build cgo

package main

// mlclassifier.go: M-Vert Pro BERT domain classifier via candle-binding.
//
// This file is ONLY compiled when CGO_ENABLED=1 (build tag "cgo").
// When CGO is disabled the stub in mlclassifier_stub.go is used instead,
// keeping all existing tests and the pure-Go binary working unchanged.
//
// Classifier priority in route():
//   1. Keyword classifier  (fast, zero latency, no ML)
//   2. ML classifier       (this file, falls through when keyword has no match)
//   3. "unknown"           (fallback when both miss)

import (
	"encoding/json"
	"fmt"
	"log"
	"os"

	candle "github.com/vllm-project/semantic-router/candle-binding"
)

// MLClassifier wraps the M-Vert Pro BERT sequence classifier exposed by the
// candle-binding Rust library.  It maps class indices returned by the model
// to human-readable domain names using the category_mapping.json file that
// is produced during model training.
type MLClassifier struct {
	mapping   map[int]string // class index → domain name (e.g. 3 → "finance")
	threshold float64
}

// NewMLClassifier initialises the BERT domain classifier.
//
// Init order (first success wins):
//  1. Standard Candle BERT sequence classifier (BertForSequenceClassification)
//  2. ModernBERT sequence classifier
//  3. mmBERT-32K intent classifier with LoRA adapter (llm-semantic-router/mmbert32k-intent-classifier-lora)
//
//   - modelPath   local filesystem path to the SafeTensors model directory
//   - mappingPath local path to category_mapping.json
//   - numClasses  number of output classes the model was trained with (0 = auto)
//   - useCPU      force CPU inference
//   - threshold   minimum confidence score to accept a classification [0,1]
func NewMLClassifier(modelPath, mappingPath string, numClasses int, useCPU bool, threshold float64) (*MLClassifier, error) {
	mapping, err := loadCategoryMapping(mappingPath)
	if err != nil {
		return nil, fmt.Errorf("load category mapping: %w", err)
	}

	// 1. Try standard Candle BERT classifier.
	ok := candle.InitCandleBertClassifier(modelPath, numClasses, useCPU)
	if !ok {
		log.Printf("[ml] InitCandleBertClassifier failed, trying ModernBERT")
		// 2. Try ModernBERT classifier.
		if err := candle.InitModernBertClassifier(modelPath, useCPU); err != nil {
			log.Printf("[ml] InitModernBertClassifier failed (%v), trying mmBERT-32K intent classifier", err)
			// 3. Try mmBERT-32K LoRA intent classifier (llm-semantic-router/mmbert32k-intent-classifier-lora).
			if err2 := candle.InitMmBert32KIntentClassifier(modelPath, useCPU); err2 != nil {
				return nil, fmt.Errorf("all classifiers failed — BERT: (init failed), ModernBERT: %v, MmBert32K: %v", err, err2)
			}
			log.Printf("[ml] mmBERT-32K intent classifier initialised from %s", modelPath)
		} else {
			log.Printf("[ml] ModernBERT classifier initialised from %s", modelPath)
		}
	} else {
		log.Printf("[ml] BERT classifier initialised from %s (%d classes)", modelPath, numClasses)
	}

	return &MLClassifier{mapping: mapping, threshold: threshold}, nil
}

// Classify runs the BERT classifier on text and returns (domainName, confidence).
// Returns ("", 0) when the confidence is below the configured threshold or
// when the returned class index has no entry in the category mapping.
// Tries all three backends in order; only the initialized one will succeed.
func (m *MLClassifier) Classify(text string) (string, float64) {
	if m == nil || text == "" {
		return "", 0
	}

	result, err := candle.ClassifyCandleBertText(text)
	if err != nil {
		result, err = candle.ClassifyModernBertText(text)
		if err != nil {
			result, err = candle.ClassifyMmBert32KIntent(text)
			if err != nil {
				log.Printf("[ml] classification error: %v", err)
				return "", 0
			}
		}
	}

	confidence := float64(result.Confidence)
	if confidence < m.threshold {
		return "", 0
	}

	domain, ok := m.mapping[result.Class]
	if !ok {
		return "", 0
	}
	return domain, confidence
}

// loadCategoryMapping parses the category_mapping.json file.
// Accepts both formats produced by M-Vert Pro training:
//
//	{"idx_to_category": {"0": "finance", "1": "health", ...}}
//	{"category_to_idx": {"finance": 0, "health": 1, ...}}
func loadCategoryMapping(path string) (map[int]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var raw struct {
		IdxToCategory map[string]string `json:"idx_to_category"`
		CategoryToIdx map[string]int    `json:"category_to_idx"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}

	mapping := make(map[int]string)

	// Prefer idx_to_category (direct lookup).
	for idxStr, name := range raw.IdxToCategory {
		var idx int
		if _, err := fmt.Sscan(idxStr, &idx); err == nil {
			mapping[idx] = name
		}
	}

	// Fall back to inverting category_to_idx.
	if len(mapping) == 0 {
		for name, idx := range raw.CategoryToIdx {
			mapping[idx] = name
		}
	}

	if len(mapping) == 0 {
		return nil, fmt.Errorf("%s contains neither idx_to_category nor category_to_idx", path)
	}
	return mapping, nil
}
