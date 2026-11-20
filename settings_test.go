package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestReconnect(t *testing.T) {
	entry := network(t, "Entry")[0]
	p := startPeer(t, "Ana", Config{First: freeUDP(t)}, nil)
	p.rec.waitNetwork(t, netReady)
	if s := p.Settings(); s.Mode != modeOwn || s.EntryNodesFrom != "first" || s.EntryNodes != p.cfg.First || s.DisplayName != "Ana" || !s.ShowInNotch || !s.NotchPreview {
		t.Fatalf("%+v", s)
	}
	// a board needs no one else on the network; it moves to another network when the app starts with other settings
	if _, err := p.CreateBoard("Launch", nil); err != nil {
		t.Fatal(err)
	}
	p.ServiceShutdown()
	p = startPeer(t, "Ana", Config{DataDir: p.data, Bootstrap: entry.cfg.First}, p.id)
	p.rec.waitNetwork(t, netReady)
	if s := p.Settings(); s.Mode != modeOwn || s.EntryNodes != entry.cfg.First || s.EntryNodesFrom != "flag" {
		t.Errorf("from the flag %+v", s)
	}
	// a person joins the board on the network that came up after it was made
	q := startPeer(t, "Bia", Config{Bootstrap: entry.cfg.First}, nil)
	q.rec.waitNetwork(t, netReady)
	invited(t, p, q, InviteOptions{TTLSeconds: 3600})
	// restarting the network keeps the board and the people on it find each other again
	if err := p.Reconnect(); err != nil {
		t.Fatal(err)
	}
	p.rec.waitNetwork(t, netReady)
	if _, err := p.board(t).Columns, error(nil); err != nil || p.board(t).State != stateActive {
		t.Fatal("the board did not survive the restart of the network")
	}
	todo := p.board(t).column("To do").ID
	until(t, "could edit again", func() bool { return p.board(t).CanEdit })
	if _, err := p.AddCard(todo, CardInput{Title: "after reconnect"}, 0); err != nil {
		t.Fatal(err)
	}
	q.waitBoard(t, "saw it", func(x Board) bool { k, _ := x.card("after reconnect"); return k != nil })
}

func TestStartCleansWhatIsNotComplete(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	if err := prepareDataDir(dir); err != nil {
		t.Fatal(err)
	}
	// a store left by a join or a create that did not finish, with no marker
	if err := os.MkdirAll(filepath.Join(dir, boardDir), 0o700); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, boardDir, "junk"), []byte("x"), 0o600)
	p := startPeer(t, "Ana", Config{DataDir: dir}, nil)
	if p.Board() != nil {
		t.Fatal("a board from a store with no marker")
	}
	if _, err := os.Stat(filepath.Join(dir, boardDir)); err == nil {
		t.Error("the leftover store was not removed")
	}
	// a marker with no usable store: the board is there as an ended one, with its reason, and Leave removes it
	p.ServiceShutdown()
	if err := writeMarker(dir, marker{Version: 1, Group: "ab", Member: true}); err != nil {
		t.Fatal(err)
	}
	p = startPeer(t, "Ana", Config{DataDir: dir}, p.id)
	b := p.Board()
	if b == nil || b.State != stateEnded || b.Error == nil || b.Error.Reason != "board-open" || b.CanEdit {
		t.Fatalf("%+v", b)
	}
	if _, err := p.CreateBoard("New", nil); explain(err).Reason != "board-exists" {
		t.Errorf("a board over a broken one: %v", err)
	}
	if err := p.EditCard("x", CardEdit{Title: ptr("y")}); err == nil {
		t.Error("an edit of a board that did not open")
	}
	if err := p.LeaveBoard(); err != nil {
		t.Fatal(err)
	}
	if p.Board() != nil {
		t.Error("the broken board stays")
	}
	for _, name := range []string{markerFile, boardDir} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			t.Errorf("%s stays", name)
		}
	}
	if _, err := p.CreateBoard("New", nil); err != nil {
		t.Errorf("a board after Leave: %v", err)
	}
}

func TestShutdownDuringAJoin(t *testing.T) {
	ps := network(t, "Ana", "Bia")
	a, b := ps[0], ps[1]
	if _, err := a.CreateBoard("Launch", nil); err != nil {
		t.Fatal(err)
	}
	info, _ := a.CreateInvite(mainCtx, InviteOptions{TTLSeconds: 3600, RequireApproval: true})
	if _, err := b.JoinBoard(mainCtx, info.Code); err != nil {
		t.Fatal(err)
	}
	b.waitBoard(t, "waiting", func(x Board) bool { return x.Phase == phaseWaiting })
	done := make(chan struct{})
	go func() { b.ServiceShutdown(); close(done) }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("the service does not stop while a join waits")
	}
	if _, err := os.Stat(filepath.Join(b.data, markerFile)); err == nil {
		t.Error("a marker from a join that never finished")
	}
	// started again, it has no board and no leftover
	b = b.restart(t, "")
	if b.Board() != nil {
		t.Error("a board from a join that never finished")
	}
	if _, err := os.Stat(filepath.Join(b.data, boardDir)); err == nil {
		t.Error("a store from a join that never finished")
	}
}

func TestServiceBeforeStartAndAfterShutdown(t *testing.T) {
	s := NewBoardService(Config{DataDir: t.TempDir()}, func(string, any) {})
	if err := s.Reconnect(); explain(err).Reason != "network-not-ready" {
		t.Errorf("Reconnect: %v", err)
	}
	if _, err := s.SetShowInNotch(true); explain(err).Reason != "network-not-ready" {
		t.Errorf("SetShowInNotch: %v", err)
	}
	if _, err := s.Invites(); explain(err).Reason != "network-not-ready" {
		t.Errorf("Invites: %v", err)
	}
	if s.Board() != nil || s.Notch().Active || s.trayNow().Status != "No board" {
		t.Error("state before Start")
	}
	if err := s.ServiceShutdown(); err != nil {
		t.Error(err)
	}
	p := startPeer(t, "Ana", Config{}, nil)
	if _, err := p.CreateBoard("A", nil); err != nil {
		t.Fatal(err)
	}
	if err := p.ServiceShutdown(); err != nil {
		t.Fatal(err)
	}
	if _, err := p.CreateBoard("B", nil); err == nil {
		t.Error("a board after shutdown")
	}
	if err := p.ServiceShutdown(); err != nil {
		t.Errorf("a second shutdown: %v", err)
	}
	// Start twice is the same Start
	q := startPeer(t, "Bia", Config{}, nil)
	i1, _ := q.Start()
	i2, _ := q.Start()
	if i1.KeyID != i2.KeyID || i1.DataDir != q.data || i1.Version != version {
		t.Errorf("%+v %+v", i1, i2)
	}
}

// The window keeps the newer of an event and an answer by Seq, so each network state and each board read has a larger one,
// and a new board is never older than the end of the one before.
func TestSeqOrdersEventsAndAnswers(t *testing.T) {
	p := startPeer(t, "Ana", Config{}, nil)
	p.rec.waitNetwork(t, netReady)
	if err := p.Reconnect(); err != nil {
		t.Fatal(err)
	}
	until(t, "a second network state", func() bool {
		p.rec.mu.Lock()
		defer p.rec.mu.Unlock()
		return len(p.rec.networks) >= 4 && p.rec.networks[len(p.rec.networks)-1].State == netReady
	})
	p.rec.mu.Lock()
	nets := append([]Network(nil), p.rec.networks...)
	p.rec.mu.Unlock()
	for i := 1; i < len(nets); i++ {
		if nets[i].Seq <= nets[i-1].Seq {
			t.Fatalf("network seq %d after %d", nets[i].Seq, nets[i-1].Seq)
		}
	}
	if last := nets[len(nets)-1]; last.Seq == 0 || !reflect.DeepEqual(p.Info().Network, last) {
		t.Errorf("Start's network %+v is not the last event %+v", p.Info().Network, last)
	}

	made, err := p.CreateBoard("A", nil)
	if err != nil {
		t.Fatal(err)
	}
	now := p.board(t)
	if made.Seq == 0 || now.Seq <= made.Seq {
		t.Errorf("Board() seq %d after CreateBoard %d", now.Seq, made.Seq)
	}
	if _, err := p.AddColumn("B", 0); err != nil {
		t.Fatal(err)
	}
	until(t, "an event with the column", func() bool {
		b, ok := p.rec.lastState()
		return ok && b.column("B") != nil && b.Seq > now.Seq
	})
	if err := p.LeaveBoard(); err != nil {
		t.Fatal(err)
	}
	p.rec.mu.Lock()
	states := append([]Board(nil), p.rec.states...)
	p.rec.mu.Unlock()
	end := states[len(states)-1]
	if end.State != stateEnded {
		t.Fatalf("last event %s", end.State)
	}
	for _, b := range states[:len(states)-1] {
		if b.Seq >= end.Seq {
			t.Errorf("event %s seq %d is not older than the end %d", b.State, b.Seq, end.Seq)
		}
	}
	again, err := p.CreateBoard("C", nil)
	if err != nil {
		t.Fatal(err)
	}
	if again.Seq <= end.Seq {
		t.Errorf("a new board has seq %d, the end of the last one %d", again.Seq, end.Seq)
	}
}

// Settings tells the window the waits it runs with: none by default but the lookup interval.
func TestSettingsShowTheWaits(t *testing.T) {
	p := startPeerWith(t, "Ana", Config{}, func(s *BoardService) { s.cfg.FindTimeout, s.cfg.LookupInterval = 0, 0 })
	if s := p.Settings(); s.FindTimeoutMs != 0 || s.JoinTimeoutMs != 0 || s.NotchLingerMs != 0 || s.LookupIntervalMs != defaultLookupInterval.Milliseconds() {
		t.Errorf("defaults %+v", s)
	}
	q := startPeer(t, "Bia", Config{JoinTimeout: 2 * time.Minute, NotchLinger: 3 * time.Second}, nil)
	if s := q.Settings(); s.FindTimeoutMs != 30_000 || s.JoinTimeoutMs != 120_000 || s.NotchLingerMs != 3000 || s.LookupIntervalMs != 1000 {
		t.Errorf("set %+v", s)
	}
}
