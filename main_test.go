package main

import (
	"os"
	"testing"
)

// Every test app listens on 127.0.0.1, so one source address stands for all of them.
func TestMain(m *testing.M) {
	testMaxConnsPerIP = defaultMaxMembers
	os.Exit(m.Run())
}
