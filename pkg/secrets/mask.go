package secrets

import "strings"

const (
	fullMaskThreshold       = 9
	mediumSecretThreshold   = 10
	longSecretThreshold     = 16
	shortVisibleEdgeChars   = 2
	mediumVisibleEdgeChars  = 3
	longVisibleEdgeChars    = 4
	fallbackVisibleEdgeChar = 1
	edgeMultiplier          = 2
)

// Obfuscate masks a secret while preserving limited edge visibility for longer values.
func Obfuscate(secret string) string {
	runes := []rune(secret)
	length := len(runes)
	if length == 0 {
		return ""
	}
	if length < fullMaskThreshold {
		return strings.Repeat("*", length)
	}

	visibleEdge := shortVisibleEdgeChars
	if length >= mediumSecretThreshold {
		visibleEdge = mediumVisibleEdgeChars
	}
	if length >= longSecretThreshold {
		visibleEdge = longVisibleEdgeChars
	}
	if visibleEdge*edgeMultiplier >= length {
		visibleEdge = fallbackVisibleEdgeChar
	}

	start := string(runes[:visibleEdge])
	end := string(runes[length-visibleEdge:])
	middleMask := strings.Repeat("*", length-(visibleEdge*edgeMultiplier))
	return start + middleMask + end
}
