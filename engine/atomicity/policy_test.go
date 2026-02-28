package atomicity

import "testing"

func TestEffectiveMaxCommits(t *testing.T) {
	tests := []struct {
		base   int
		profile string
		want   int
	}{
		{20, "balanced", 20},
		{20, "cohesive", 10},
		{20, "strict", 40},
		{5, "cohesive", 2},
		{1, "cohesive", 2},
		{30, "strict", 50},
		{100, "strict", 50},
		{0, "balanced", 20},
		{20, "invalid", 20},
		{20, "", 20},
	}
	for _, tt := range tests {
		got := EffectiveMaxCommits(tt.base, tt.profile)
		if got != tt.want {
			t.Errorf("EffectiveMaxCommits(%d, %q) = %d, want %d", tt.base, tt.profile, got, tt.want)
		}
	}
}

func TestNormalizeProfile(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"balanced", "balanced"},
		{"cohesive", "cohesive"},
		{"strict", "strict"},
		{"Balanced", "balanced"},
		{"  strict  ", "strict"},
		{"invalid", "balanced"},
		{"", "balanced"},
	}
	for _, tt := range tests {
		got := NormalizeProfile(tt.in)
		if got != tt.want {
			t.Errorf("NormalizeProfile(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
