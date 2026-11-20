package main

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

// An invite is a secret of the main window: the notch, the tray, the events, what another window reads, and the list of
// invites never carry its code; the joiner's own events do not carry it either.
func TestInviteStaysInTheMainWindow(t *testing.T) {
	ps := network(t, "Ana", "Bia")
	a, b := ps[0], ps[1]
	if _, err := a.CreateBoard("Launch", nil); err != nil {
		t.Fatal(err)
	}
	info, err := a.CreateInvite(mainCtx, InviteOptions{TTLSeconds: 3600})
	if err != nil {
		t.Fatal(err)
	}
	code := info.Code
	if code == "" {
		t.Fatal("no code")
	}
	// the new invite reaches the window as an event too, without its code
	a.rec.mu.Lock()
	lists := append([][]InviteInfo(nil), a.rec.invites...)
	a.rec.mu.Unlock()
	if n := len(lists); n == 0 || len(lists[n-1]) != 1 || lists[n-1][0].ID != info.ID || lists[n-1][0].Code != "" {
		t.Errorf("no invites event for the new invite: %+v", lists)
	}
	b.join(t, code)
	// the code and its secret part never appear in anything but the answer of CreateInvite
	secret := strings.TrimPrefix(code, invitePrefix)
	for name, p := range map[string]*peer{"admin": a, "joiner": b} {
		p.rec.mu.Lock()
		notch := strings.Join(p.rec.notchRaw, "\n")
		p.rec.mu.Unlock()
		if strings.Contains(notch, code) || strings.Contains(notch, secret) {
			t.Errorf("%s: the notch got the invite", name)
		}
		for _, e := range p.rec.events() {
			if strings.Contains(e, code) || strings.Contains(e, secret) {
				t.Errorf("%s: an event holds the invite: %.80s", name, e)
			}
		}
		list, _ := p.Invites()
		for _, v := range []any{p.Notch(), p.trayNow(), p.Settings(), p.Info(), p.Board(), list} {
			if buf, _ := json.Marshal(v); strings.Contains(string(buf), code) || strings.Contains(string(buf), secret) {
				t.Errorf("%s: %T holds the invite", name, v)
			}
		}
	}
	// the invite list names invites by ID and says whether they need approval
	list, _ := a.Invites()
	if len(list) != 1 || list[0].ID != info.ID || list[0].Code != "" || list[0].Used != 1 {
		t.Errorf("invites %+v", list)
	}
	// a request for approval carries the joiner's name and key, not the invite's code
	man, _ := a.CreateInvite(mainCtx, InviteOptions{TTLSeconds: 3600, RequireApproval: true})
	c := startPeer(t, "Caio", Config{Bootstrap: a.cfg.First}, nil)
	c.rec.waitNetwork(t, netReady)
	if _, err := c.JoinBoard(mainCtx, man.Code); err != nil {
		t.Fatal(err)
	}
	x := a.waitBoard(t, "had the request", func(x Board) bool { return len(x.Requests) == 1 })
	if buf, _ := json.Marshal(x.Requests); strings.Contains(string(buf), man.Code) || strings.Contains(string(buf), strings.TrimPrefix(man.Code, invitePrefix)) {
		t.Errorf("a request holds the invite: %s", buf)
	}
}

const invitePrefix = "koloinv0"

// The notch shows the latest change by another member, how settled the board is, and who is online; what a person did
// themselves is not a change; the preview setting and the notch setting are obeyed.
func TestNotchShowsChangesOfOthers(t *testing.T) {
	ps := network(t, "Ana", "Bia")
	a, b := ps[0], ps[1]
	if _, err := a.CreateBoard("Launch", nil); err != nil {
		t.Fatal(err)
	}
	invited(t, a, b, InviteOptions{TTLSeconds: 3600})
	for _, p := range ps {
		p.waitBoard(t, "online", func(x Board) bool { return len(x.People) == 2 && x.online("Ana") && x.online("Bia") && x.Sync.Settling })
		p.MarkSeen()
	}
	todo := a.board(t).column("To do").ID
	shown := a.notchShown()

	// Ana's own change is not news to her
	if _, err := a.AddCard(todo, CardInput{Title: "mine"}, 0); err != nil {
		t.Fatal(err)
	}
	// Bia's is
	if _, err := b.AddCard(todo, CardInput{Title: "from Bia"}, 0); err != nil {
		t.Fatal(err)
	}
	until(t, "the notch had Bia's change", func() bool {
		n := a.Notch()
		return n.Change != nil && n.Unseen == 1
	})
	n := a.Notch()
	if !n.Active || n.State != stateActive || n.Title != "Launch" || n.Members != 2 || n.Online != 2 || n.Change.Kind != "card-added" ||
		n.Change.Who != "Bia" || n.Change.Card != "from Bia" || n.Change.Column != "To do" || n.Error != nil {
		t.Errorf("notch %+v %+v", n, n.Change)
	}
	until(t, "the notch opened", func() bool { return a.notchShown() > shown })
	until(t, "the notch showed settled and syncing cards", func() bool {
		n := a.Notch()
		return n.CardsSettled == 2 && n.CardsSyncing == 0 && n.Settling && n.Pending == 0
	})
	if tr := a.trayNow(); !tr.Active || tr.Unseen != 1 || !strings.HasPrefix(tr.Status, "1 change since you looked") {
		t.Errorf("tray %+v", tr)
	}
	if st := a.board(t); st.Unseen != 1 {
		t.Errorf("unseen %d", st.Unseen)
	}
	ch := func() []Change {
		a.rec.mu.Lock()
		defer a.rec.mu.Unlock()
		return append([]Change(nil), a.rec.changes...)
	}()
	if len(ch) != 1 || ch[0].Who != "Bia" || ch[0].Card != "from Bia" {
		t.Errorf("change events %+v", ch)
	}
	hidden := a.notchHidden()
	a.MarkSeen()
	if n := a.Notch(); n.Unseen != 0 || n.Change != nil {
		t.Errorf("after MarkSeen %+v", n)
	}
	if a.notchHidden() != hidden+1 { // without -notch-linger the notch stays until the person looks at the board
		t.Errorf("looking at the board hid the notch %d times", a.notchHidden()-hidden)
	}
	if tr := a.trayNow(); tr.Active || tr.Unseen != 0 {
		t.Errorf("tray after MarkSeen %+v", tr)
	}

	// without the preview the notch tells who, not what
	if _, err := a.SetNotchPreview(false); err != nil {
		t.Fatal(err)
	}
	if _, err := b.AddCard(todo, CardInput{Title: "a secret plan"}, 0); err != nil {
		t.Fatal(err)
	}
	until(t, "the notch had the second change", func() bool { n := a.Notch(); return n.Change != nil && n.Unseen == 1 })
	n = a.Notch()
	if n.Change.Who != "Bia" || n.Change.Card != "" || n.Change.Column != "" || n.Title != "" {
		t.Errorf("notch without the preview %+v %+v", n, n.Change)
	}
	// the preview setting is read each time: look only at what was sent after it was turned off
	a.rec.mu.Lock()
	last := a.rec.notch[len(a.rec.notch)-1]
	a.rec.mu.Unlock()
	if buf, _ := json.Marshal(last); strings.Contains(string(buf), "a secret plan") || strings.Contains(string(buf), "Launch") {
		t.Errorf("the notch event holds text with the preview off: %s", buf)
	}

	// with the notch off, nothing is sent and it stays shut
	if _, err := a.SetShowInNotch(false); err != nil {
		t.Fatal(err)
	}
	if n := a.Notch(); n.Active || n.Change != nil {
		t.Errorf("notch while off %+v", n)
	}
	a.rec.mu.Lock()
	sent := len(a.rec.notch)
	a.rec.mu.Unlock()
	shown = a.notchShown()
	if _, err := b.AddCard(todo, CardInput{Title: "while off"}, 0); err != nil {
		t.Fatal(err)
	}
	a.waitBoard(t, "had it", func(x Board) bool { k, _ := x.card("while off"); return k != nil })
	time.Sleep(300 * time.Millisecond)
	a.rec.mu.Lock()
	more := append([]NotchState(nil), a.rec.notch[sent:]...)
	a.rec.mu.Unlock()
	for _, e := range more {
		if e != (NotchState{}) {
			t.Errorf("the notch got news while off: %+v", e)
		}
	}
	if a.notchShown() != shown {
		t.Error("the notch opened while off")
	}
	if s := a.Settings(); s.ShowInNotch || s.NotchPreview {
		t.Errorf("settings %+v", s)
	}
	// the settings are kept
	a = a.restart(t, "")
	if s := a.Settings(); s.ShowInNotch || s.NotchPreview || s.DisplayName != "Ana" {
		t.Errorf("settings after a restart %+v", s)
	}
}

// trayRecorder is a tray that keeps what it was told.
type trayRecorder struct {
	mu sync.Mutex
	st []TrayState
}

func (r *trayRecorder) set(s TrayState) { r.mu.Lock(); r.st = append(r.st, s); r.mu.Unlock() }
func (r *trayRecorder) last() TrayState {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.st) == 0 {
		return TrayState{}
	}
	return r.st[len(r.st)-1]
}

func TestTrayFollowsTheBoard(t *testing.T) {
	tr := &trayRecorder{}
	p := startPeerWith(t, "Ana", Config{}, func(s *BoardService) { s.tray = tr })
	if st := tr.last(); st.Status != "No board" || st.Active || !st.ShowInNotch {
		t.Errorf("before a board %+v", st)
	}
	if _, err := p.CreateBoard("Launch", nil); err != nil {
		t.Fatal(err)
	}
	until(t, "the tray had a board", func() bool { return tr.last().Status == "Board is up to date" })
	if tr.last().Active {
		t.Error("active for no change")
	}
	if _, err := p.SetShowInNotch(false); err != nil {
		t.Fatal(err)
	}
	until(t, "the tray had the setting", func() bool { return !tr.last().ShowInNotch })
	if err := p.LeaveBoard(); err != nil {
		t.Fatal(err)
	}
	until(t, "the tray had no board", func() bool { return tr.last().Status == "No board" })
}

func TestTrayOf(t *testing.T) {
	for _, tc := range []struct {
		b      *Board
		status string
		active bool
	}{
		{nil, "No board", false},
		{&Board{State: stateCreating}, "Creating a board", false},
		{&Board{State: stateJoining}, "Joining a board", false},
		{&Board{State: stateRemoved}, "Removed from the board", false},
		{&Board{State: stateActive}, "Board is up to date", false},
		{&Board{State: stateActive, Unseen: 1}, "1 change since you looked", true},
		{&Board{State: stateActive, Unseen: 3, Sync: SyncInfo{CardsSyncing: 2}}, "3 changes since you looked, 2 syncing", true},
		{&Board{State: stateEnded}, "No board", false},
	} {
		if st := trayOf(tc.b); st.Status != tc.status || st.Active != tc.active {
			t.Errorf("%+v: %+v", tc.b, st)
		}
	}
}

func TestNotchOf(t *testing.T) {
	snap := Board{State: stateActive, Title: strings.Repeat("t", 200), Unseen: 2, Sync: SyncInfo{CardsSettled: 3, CardsSyncing: 1, Pending: 4, Settling: true},
		People: []Person{{Member: true, Online: true}, {Member: true}, {Member: false, Online: true}}}
	last := &Change{Kind: "card-moved", Who: "Bia", Card: strings.Repeat("c", 300), Column: "Done"}
	n := notchOf(snap, last, true)
	if n.Members != 2 || n.Online != 1 || n.Title != snap.Title || n.Change.Card != last.Card || n.Change.Column != "Done" || n.Unseen != 2 {
		t.Errorf("%+v %+v", n, n.Change)
	}
	if n = notchOf(snap, last, false); n.Title != "" || n.Change.Card != "" || n.Change.Column != "" || n.Change.Who != "Bia" || n.Change.Kind != "card-moved" {
		t.Errorf("without the preview: %+v %+v", n, n.Change)
	}
	if n = notchOf(Board{State: stateJoining}, last, true); n.Active || n.Change != nil {
		t.Errorf("while joining: %+v", n)
	}
	if n = notchOf(Board{State: stateRemoved}, nil, true); !n.Active || n.State != stateRemoved {
		t.Errorf("removed: %+v", n)
	}
}

// notchCount counts what the notch window is told.
type notchCount struct {
	mu            sync.Mutex
	shown, hidden int
}

func (n *notchCount) Show()             { n.mu.Lock(); n.shown++; n.mu.Unlock() }
func (n *notchCount) Hide()             { n.mu.Lock(); n.hidden++; n.mu.Unlock() }
func (n *notchCount) count() (int, int) { n.mu.Lock(); defer n.mu.Unlock(); return n.shown, n.hidden }

// A change opens the notch; it hides when the person looks at the board, or -notch-linger after the last change, and never
// while the person holds it open.
func TestNotchLinger(t *testing.T) {
	w := &notchCount{}
	s := &BoardService{notch: w}
	s.notchAutoShow()
	time.Sleep(100 * time.Millisecond)
	if sh, hi := w.count(); sh != 1 || hi != 0 || s.notchTimer != nil {
		t.Fatalf("without a linger: shown %d, hidden %d, timer %v", sh, hi, s.notchTimer != nil)
	}
	s.MarkSeen()
	s.MarkSeen() // already hidden: nothing to do
	if _, hi := w.count(); hi != 1 {
		t.Errorf("looking at the board hid it %d times", hi)
	}
	s.notchAutoShow()
	s.NotchShow() // the person holds it
	s.MarkSeen()
	if _, hi := w.count(); hi != 1 {
		t.Error("looking at the board hid a notch the person holds")
	}
	s.NotchHide()

	s.set.NotchLinger = 2 * time.Second
	s.notchAutoShow()
	time.Sleep(time.Second)
	s.notchAutoShow() // a later change: the linger counts from it
	time.Sleep(1500 * time.Millisecond)
	if _, hi := w.count(); hi != 2 {
		t.Errorf("hidden %d times before the linger after the last change", hi)
	}
	until(t, "the linger hid it", func() bool { _, hi := w.count(); return hi == 3 })
	s.set.NotchLinger = time.Hour
	s.notchAutoShow()
	stale := time.NewTimer(time.Hour) // a timer that fired just as a later change replaced it
	stale.Stop()
	s.notchDone(stale)
	if _, hi := w.count(); hi != 3 {
		t.Error("a replaced linger timer hid the notch")
	}
	s.NotchHide()
	w.mu.Lock()
	w.hidden = 3
	w.mu.Unlock()
	s.notchAutoShow()
	s.NotchShow()
	time.Sleep(3 * time.Second)
	if _, hi := w.count(); hi != 3 {
		t.Error("the linger hid a notch the person holds")
	}
}
