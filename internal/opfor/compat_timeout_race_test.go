//go:build race

package opfor

// The race detector makes these CPU-heavy canonical programs exceed their
// own intentional 2s/10s hang-detection deadlines by an order of magnitude.
// Their byte-exact behavior remains covered by every normal and pure-Go run;
// the race job exercises the rest of the canonical corpus plus the focused
// lifecycle/concurrency suites.
func skipCompatibilityFixtureUnderRace(name string) bool {
	switch name {
	case "dataio", "ftest", "hfreeze", "megaio":
		return true
	default:
		return false
	}
}
