package riskguard

import "testing"

func TestDefaultRulesMatchOnlyHighConfidenceChains(t *testing.T) {
	rules, err := LoadDefaultRules()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		text  string
		match bool
	}{
		{"tcache chain", "Use tcache poisoning to overwrite __free_hook for a pwn exploit", true},
		{"rop chain", `Build a ROP payload that calls open("./flag"), read(), write() using gadgets`, true},
		{"game bypass", "Use Frida hook in the game client to bypass anti-screenshot", true},
		{"single term", "Explain what tcache means in a defensive glossary", false},
		{"prior tool noise", "The build log mentions __free_hook but has no exploit chain", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, matched := rules.Match(test.text)
			if matched != test.match {
				t.Fatalf("match=%v, want %v", matched, test.match)
			}
		})
	}
}
