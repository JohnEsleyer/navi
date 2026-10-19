package navi

import (
	"os"
	"testing"
)

// TestMain points persistence at an in-memory store for the whole package.
//
// Without this, every New(Config{...}) in the tests below resolves its empty
// DBPath to the user's real config directory, so all engines would share one
// on-disk store: a test's session would be resumed by the next test's New()
// via ListSessions()[0], injecting a non-empty context window and making the
// suite order-dependent and flaky. ":memory:" gives each engine a private
// store that is discarded on exit.
func TestMain(m *testing.M) {
	os.Setenv("NAVI_DB_PATH", ":memory:")
	os.Exit(m.Run())
}
