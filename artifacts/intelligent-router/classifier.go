package main

import (
	"fmt"
	"regexp"
	"unicode"
)

// preppedRule is a compiled keyword rule ready for matching.
type preppedRule struct {
	Name     string
	Operator string
	Regexps  []*regexp.Regexp
}

// KeywordClassifier classifies text into domains using keyword rules.
// It is pure-Go and requires no external ML models.
type KeywordClassifier struct {
	rules []preppedRule
}

// NewKeywordClassifier compiles keyword rules into a classifier.
func NewKeywordClassifier(rules []KeywordRule) (*KeywordClassifier, error) {
	prepped := make([]preppedRule, 0, len(rules))
	for _, rule := range rules {
		switch rule.Operator {
		case "AND", "OR", "NOR":
		default:
			return nil, fmt.Errorf("unsupported operator %q in rule %q", rule.Operator, rule.Name)
		}

		r := preppedRule{
			Name:     rule.Name,
			Operator: rule.Operator,
			Regexps:  make([]*regexp.Regexp, len(rule.Keywords)),
		}
		for i, kw := range rule.Keywords {
			re, err := regexp.Compile(buildPattern(kw, rule.CaseSensitive))
			if err != nil {
				return nil, fmt.Errorf("failed to compile keyword %q in rule %q: %w", kw, rule.Name, err)
			}
			r.Regexps[i] = re
		}
		prepped = append(prepped, r)
	}
	return &KeywordClassifier{rules: prepped}, nil
}

// buildPattern constructs a regex pattern for a keyword.
// A leading word boundary (\b) is added so "stock" does not match "livestock",
// but no trailing boundary is used so "stock" still matches "stocks" and
// "algorithm" still matches "algorithmic" (prefix / stem matching).
// Chinese characters do not use word boundaries.
func buildPattern(keyword string, caseSensitive bool) string {
	quoted := regexp.QuoteMeta(keyword)
	hasWordChar := false
	hasChinese := false
	for _, r := range keyword {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			hasWordChar = true
		}
		if unicode.Is(unicode.Han, r) {
			hasChinese = true
		}
	}
	pattern := quoted
	if hasWordChar && !hasChinese {
		// Leading boundary only: prevents matching inside a word (e.g. "livestock"),
		// but allows suffix variation (e.g. "stocks", "algorithmic").
		pattern = `\b` + pattern
	}
	if !caseSensitive {
		pattern = "(?i)" + pattern
	}
	return pattern
}

// Classify returns the domain name and confidence for the first matching rule.
// Returns ("", 0) when no rule matches.
func (c *KeywordClassifier) Classify(text string) (string, float64) {
	for _, rule := range c.rules {
		if c.matchRule(text, rule) {
			return rule.Name, 1.0
		}
	}
	return "", 0
}

// matchRule tests whether text satisfies the rule's operator + keywords.
func (c *KeywordClassifier) matchRule(text string, rule preppedRule) bool {
	switch rule.Operator {
	case "AND":
		for _, re := range rule.Regexps {
			if !re.MatchString(text) {
				return false
			}
		}
		return true
	case "OR":
		for _, re := range rule.Regexps {
			if re.MatchString(text) {
				return true
			}
		}
		return false
	case "NOR":
		for _, re := range rule.Regexps {
			if re.MatchString(text) {
				return false
			}
		}
		return true
	}
	return false
}
