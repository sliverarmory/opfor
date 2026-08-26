//go:build !race

package opfor

func skipCompatibilityFixtureUnderRace(string) bool {
	return false
}
