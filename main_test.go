package main

import (
	"os"
	"testing"
)

// testMaxConns is the connections per source address the tests allow, and the transport limit the swarm checks it against.
const testMaxConns = 64

// Every test app listens on 127.0.0.1, so one source address stands for all of them.
func TestMain(m *testing.M) {
	testMaxConnsPerIP = testMaxConns
	os.Exit(m.Run())
}
