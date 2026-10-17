//go:build !race

package definition_test

// raceEnabled shrinks the large inputs of the size tests: the race
// detector makes them several times slower, and CI runs every test with it.
const raceEnabled = false
