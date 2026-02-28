package atomicity

// EffectiveMaxCommits returns the max-commits cap for planning based on
// the base value and atomicity profile.
//
// Profiles:
//   - cohesive: fewer, larger commits — cap at base/2 (min 2)
//   - balanced: default — use base as-is
//   - strict: more, smaller commits — cap at base*2 (max 50)
func EffectiveMaxCommits(base int, profile string) int {
	p := NormalizeProfile(profile)
	if base <= 0 {
		base = 20
	}
	switch p {
	case "cohesive":
		n := base / 2
		if n < 2 {
			return 2
		}
		return n
	case "strict":
		n := base * 2
		if n > 50 {
			return 50
		}
		return n
	default:
		return base
	}
}
