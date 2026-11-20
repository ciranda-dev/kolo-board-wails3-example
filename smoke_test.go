//go:build smoke

package main

import (
	"os"
	"strings"
	"testing"
)

// TestSmokeAgainstRunningApp makes a board between two services through the entry node of a running app, such as
// `./bin/kolo-board -first 127.0.0.1:4000`: go test -tags smoke -run Smoke with KOLO_SMOKE_ENTRY=127.0.0.1:4000. Nothing
// touches a real data directory; both services use temporary ones.
func TestSmokeAgainstRunningApp(t *testing.T) {
	entry := os.Getenv("KOLO_SMOKE_ENTRY")
	if entry == "" {
		t.Skip("set KOLO_SMOKE_ENTRY to the entry node of a running app")
	}
	a := startPeer(t, "Ana", Config{Bootstrap: entry}, nil)
	a.rec.waitNetwork(t, netReady)
	b := startPeer(t, "Bia", Config{Bootstrap: entry}, nil)
	b.rec.waitNetwork(t, netReady)
	if _, err := a.CreateBoard("Smoke", nil); err != nil {
		t.Fatal(err)
	}
	info, err := a.CreateInvite(mainCtx, InviteOptions{TTLSeconds: 3600})
	if err != nil {
		t.Fatal(err)
	}
	b.join(t, info.Code)
	todo := a.board(t).column("To do").ID
	if _, err := a.AddCard(todo, CardInput{Title: "from Ana"}, 0); err != nil {
		t.Fatal(err)
	}
	b.waitBoard(t, "had the card", func(x Board) bool { k, _ := x.card("from Ana"); return k != nil })
	b.rec.mu.Lock()
	defer b.rec.mu.Unlock()
	for _, e := range b.rec.notchRaw {
		if strings.Contains(e, info.Code) {
			t.Fatalf("the notch got the invite: %s", e)
		}
	}
	t.Logf("a board of 2 people through %s: card shared, %d notch events, none with the invite", entry, len(b.rec.notchRaw))
}
