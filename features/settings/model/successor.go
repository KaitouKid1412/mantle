package model

import (
	"strconv"
	"strings"
)

// splitVersion separates a model ID into its family and numeric version:
// "claude-opus-4-6" → ("claude-opus", [4 6]); "claude-haiku-4-5-20251001" →
// ("claude-haiku", [4 5]). IDs without a trailing version give a nil version.
func splitVersion(id string) (family string, version []int) {
	parts := strings.Split(Family(id), "-")
	i := len(parts)
	for i > 0 {
		n, err := strconv.Atoi(parts[i-1])
		if err != nil || n > 999 { // dates were already stripped; guard anyway
			break
		}
		version = append([]int{n}, version...)
		i--
	}
	return strings.Join(parts[:i], "-"), version
}

func newer(a, b []int) bool {
	for i := 0; i < len(a) || i < len(b); i++ {
		var x, y int
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			return x > y
		}
	}
	return false
}

// Successor finds the model that replaces a pinned default. saved is the "model"
// setting; only a full model ID (not an alias) can be outdated. The successor is the
// alias row of the same family whose resolved model is newer, e.g. saved
// "claude-opus-4-6" and an "opus" row resolving to "claude-opus-5-5". Long-context
// variants only replace long-context pins.
func Successor(saved string, rows []Row) (Row, bool) {
	fam, ver := splitVersion(saved)
	if len(ver) == 0 || fam == "" || !strings.Contains(saved, "-") {
		return Row{}, false
	}
	long := strings.HasSuffix(strings.ToLower(saved), "[1m]")
	var best Row
	var bestVer []int
	for _, r := range rows {
		if r.IsDefault || r.Disabled || r.Info.ResolvedModel == "" || strings.Contains(r.Value, "-") {
			continue // aliases only: "opus", "opus[1m]", not pinned IDs
		}
		if strings.HasSuffix(strings.ToLower(r.Info.ResolvedModel), "[1m]") != long {
			continue
		}
		rf, rv := splitVersion(r.Info.ResolvedModel)
		if rf != fam || !newer(rv, ver) {
			continue
		}
		if bestVer == nil || newer(rv, bestVer) {
			best, bestVer = r, rv
		}
	}
	return best, bestVer != nil
}
