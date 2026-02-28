//go:build !cgo

package main

// mlclassifier_stub.go: no-op MLClassifier used when CGO is disabled.
//
// This file is compiled instead of mlclassifier.go whenever CGO_ENABLED=0
// (the default for the pure-Go build and all existing tests).
//
// The stub satisfies the same interface as the real MLClassifier so the
// rest of the server code compiles and runs without any changes regardless
// of whether the ML classifier is available.

// MLClassifier is a no-op when built without CGO.
// Enable the real BERT classifier by building with CGO_ENABLED=1 using Dockerfile.ml.
type MLClassifier struct{}

// NewMLClassifier always succeeds and returns a no-op classifier.
func NewMLClassifier(_, _ string, _ int, _ bool, _ float64) (*MLClassifier, error) {
	return &MLClassifier{}, nil
}

// Classify always returns ("", 0) — the caller treats this as "no match"
// and falls through to "unknown".
func (m *MLClassifier) Classify(_ string) (string, float64) {
	return "", 0
}
