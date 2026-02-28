package atomicity

import "strings"

// ValidProfiles lists allowed atomicity profile values.
var ValidProfiles = []string{"cohesive", "balanced", "strict"}

// NormalizeProfile returns a valid profile name, defaulting to "balanced".
func NormalizeProfile(p string) string {
	s := strings.TrimSpace(strings.ToLower(p))
	for _, v := range ValidProfiles {
		if s == v {
			return s
		}
	}
	return "balanced"
}
