package testinfra

import (
	"slices"
	"testing"
)

func TestDynamicConfigArgs(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want []string
	}{
		{"unset adds no flag", "", nil},
		{"blank adds no flag", "  ;  ", nil},
		{"one pair", "limit.historyCount.suggestContinueAsNew=40",
			[]string{"--dynamic-config-value", "limit.historyCount.suggestContinueAsNew=40"}},
		{"several pairs, trimmed", " a.b=1 ; c.d=\"x\" ",
			[]string{"--dynamic-config-value", "a.b=1", "--dynamic-config-value", "c.d=\"x\""}},
		{"a malformed pair reaches the CLI", "nokey",
			[]string{"--dynamic-config-value", "nokey"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := dynamicConfigArgs(tc.raw); !slices.Equal(got, tc.want) {
				t.Fatalf("dynamicConfigArgs(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}
