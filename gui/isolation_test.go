package main

import (
	"os"
	"testing"
)

// TestMain points persistence at an in-memory store for the whole GUI package,
// for the same reason as in pkg/navi: NewApp() and newTestEngine() both call
// navi.New with an empty DBPath, which otherwise resolves to the user's real
// config directory. Without this, every test engine would share, and mutate,
// one on-disk store.
func TestMain(m *testing.M) {
	os.Setenv("NAVI_DB_PATH", ":memory:")
	os.Exit(m.Run())
}
