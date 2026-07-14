package riskguard

import (
	_ "embed"
	"encoding/json"
	"errors"
	"regexp"
)

//go:embed rules.json
var defaultRulesJSON []byte

type ruleSpec struct {
	ID  string   `json:"id"`
	All []string `json:"all"`
	Any []string `json:"any"`
}

type compiledRule struct {
	id  string
	all []*regexp.Regexp
	any []*regexp.Regexp
}

type RuleSet struct {
	version string
	rules   []compiledRule
}

func LoadDefaultRules() (*RuleSet, error) {
	return compileRules(defaultRulesJSON)
}

func compileRules(raw []byte) (*RuleSet, error) {
	var document struct {
		Version string     `json:"version"`
		Rules   []ruleSpec `json:"rules"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, err
	}
	idPattern := regexp.MustCompile(`^[a-z][a-z0-9_]{2,63}$`)
	if !idPattern.MatchString(document.Version) || len(document.Rules) == 0 || len(document.Rules) > 32 {
		return nil, errors.New("invalid risk rule document")
	}
	result := &RuleSet{version: document.Version, rules: make([]compiledRule, 0, len(document.Rules))}
	seen := map[string]struct{}{}
	for _, spec := range document.Rules {
		if !idPattern.MatchString(spec.ID) || len(spec.All) == 0 || len(spec.All)+len(spec.Any) > 12 {
			return nil, errors.New("invalid risk rule")
		}
		if _, exists := seen[spec.ID]; exists {
			return nil, errors.New("duplicate risk rule")
		}
		seen[spec.ID] = struct{}{}
		rule := compiledRule{id: spec.ID}
		var err error
		if rule.all, err = compilePatterns(spec.All); err != nil {
			return nil, err
		}
		if rule.any, err = compilePatterns(spec.Any); err != nil {
			return nil, err
		}
		result.rules = append(result.rules, rule)
	}
	return result, nil
}

func compilePatterns(patterns []string) ([]*regexp.Regexp, error) {
	result := make([]*regexp.Regexp, 0, len(patterns))
	for _, pattern := range patterns {
		if len(pattern) == 0 || len(pattern) > 512 {
			return nil, errors.New("invalid risk pattern")
		}
		compiled, err := regexp.Compile(pattern)
		if err != nil {
			return nil, err
		}
		result = append(result, compiled)
	}
	return result, nil
}

func (r *RuleSet) Match(text string) (string, bool) {
	if r == nil || text == "" {
		return "", false
	}
	for _, rule := range r.rules {
		matched := true
		for _, pattern := range rule.all {
			if !pattern.MatchString(text) {
				matched = false
				break
			}
		}
		if !matched {
			continue
		}
		if len(rule.any) == 0 {
			return rule.id, true
		}
		for _, pattern := range rule.any {
			if pattern.MatchString(text) {
				return rule.id, true
			}
		}
	}
	return "", false
}

func (r *RuleSet) Version() string {
	if r == nil {
		return ""
	}
	return r.version
}
