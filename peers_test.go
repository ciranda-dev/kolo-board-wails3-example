package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	koloid "github.com/ciranda-dev/kolo-id"
	"github.com/wailsapp/wails/v3/pkg/application"
)

const waitFor = 60 * time.Second

type namer string

func (n namer) Name() string { return string(n) }

// mainCtx and otherCtx are the contexts a bound call gets from the main window and from the notch window.
var (
	mainCtx  = context.WithValue(context.Background(), application.WindowKey, namer(mainWindowName))
	otherCtx = context.WithValue(context.Background(), application.WindowKey, namer(notchWindowName))
)

// recorder keeps what a BoardService tells the main window and the notch.
type recorder struct {
	mu       sync.Mutex
	states   []Board
	networks []Network
	changes  []Change
	invites  [][]InviteInfo
	notch    []NotchState
	all      []string // every event of the main window as name and JSON
	notchRaw []string
}

func (r *recorder) emit(name string, data any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch v := data.(type) {
	case Board:
		r.states = append(r.states, v)
	case Network:
		r.networks = append(r.networks, v)
	case Change:
		r.changes = append(r.changes, v)
	case []InviteInfo:
		r.invites = append(r.invites, v)
	}
	b, _ := json.Marshal(data)
	r.all = append(r.all, name+" "+string(b))
}

func (r *recorder) emitNotch(name string, data any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if v, ok := data.(NotchState); ok {
		r.notch = append(r.notch, v)
	}
	b, _ := json.Marshal(data)
	r.notchRaw = append(r.notchRaw, name+" "+string(b))
}

func (r *recorder) events() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.all...)
}

func (r *recorder) lastState() (Board, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.states) == 0 {
		return Board{}, false
	}
	return r.states[len(r.states)-1], true
}

func (r *recorder) waitNetwork(t *testing.T, state string) {
	t.Helper()
	until(t, "network "+state, func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		return len(r.networks) > 0 && r.networks[len(r.networks)-1].State == state
	})
}

// until polls cond until it holds, and fails the test after waitFor.
func until(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(waitFor)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("never %s", what)
}

func freeUDP(t *testing.T) string {
	t.Helper()
	c, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	return c.LocalAddr().String()
}

// freePort is a free UDP port on 127.0.0.1.
func freePort(t *testing.T) uint16 {
	t.Helper()
	return netip.MustParseAddrPort(freeUDP(t)).Port()
}

// peer is a person: a service with its own data directory and identity, and what it told its windows.
type peer struct {
	*BoardService
	rec    *recorder
	data   string
	cfg    Config
	id     *koloid.Identity
	shown  int
	hidden int
	mu     sync.Mutex
}

// Show and Hide count what the notch window was told, so that a test can see that it opened.
func (p *peer) Show() { p.mu.Lock(); p.shown++; p.mu.Unlock() }
func (p *peer) Hide() { p.mu.Lock(); p.hidden++; p.mu.Unlock() }

func (p *peer) notchShown() int  { p.mu.Lock(); defer p.mu.Unlock(); return p.shown }
func (p *peer) notchHidden() int { p.mu.Lock(); defer p.mu.Unlock(); return p.hidden }

// startPeer starts a service with its own data directory (or the one in cfg), an identity made in memory (or the given
// one) and a display name, on 127.0.0.1. A cfg with no -bootstrap, -first or -local runs its own entry node (-first).
func startPeer(t *testing.T, name string, cfg Config, id *koloid.Identity) *peer {
	t.Helper()
	return startPeerWith(t, name, cfg, nil, id)
}

// startPeerWith is startPeer with a hook that runs on the service before it starts.
func startPeerWith(t *testing.T, name string, cfg Config, setup func(*BoardService), ids ...*koloid.Identity) *peer {
	t.Helper()
	var id *koloid.Identity
	if len(ids) > 0 {
		id = ids[0]
	}
	t.Setenv("HOME", t.TempDir())
	if cfg.DataDir == "" {
		cfg.DataDir = filepath.Join(t.TempDir(), "data")
	}
	cfg.Listen = "127.0.0.1:0"
	if cfg.Bootstrap == "" && cfg.First == "" && !cfg.Local {
		// No test may reach the public network: a peer with no network of its own gets one that only it uses.
		cfg.First = freeUDP(t)
	}
	if cfg.Local && cfg.source == nil {
		cfg.source = loopbackSource(freePort(t)) // never the app's default port, which a running app may hold
	}
	if cfg.LookupInterval == 0 {
		cfg.LookupInterval = time.Second
	}
	if cfg.FindTimeout == 0 {
		cfg.FindTimeout = 30 * time.Second
	}
	if id == nil {
		var err error
		if id, err = koloid.Generate(); err != nil {
			t.Fatal(err)
		}
	}
	rec := &recorder{}
	s := NewBoardService(cfg, rec.emit)
	s.identity = id
	s.emitNotch = rec.emitNotch
	p := &peer{BoardService: s, rec: rec, data: cfg.DataDir, cfg: cfg, id: id}
	s.notch = p
	if setup != nil {
		setup(s)
	}
	if _, err := s.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.ServiceShutdown() })
	if name != "" {
		if _, err := s.SetDisplayName(name); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

// restart stops the service and starts it again on the same data directory and identity.
func (p *peer) restart(t *testing.T, name string) *peer {
	t.Helper()
	if err := p.ServiceShutdown(); err != nil {
		t.Fatal(err)
	}
	cfg := p.cfg
	cfg.DataDir = p.data
	return startPeer(t, name, cfg, p.id)
}

// network starts n people on one private network: the first runs the entry node.
func network(t *testing.T, names ...string) []*peer {
	t.Helper()
	first := freeUDP(t)
	var out []*peer
	for i, name := range names {
		cfg := Config{Bootstrap: first}
		if i == 0 {
			cfg = Config{First: first}
		}
		p := startPeer(t, name, cfg, nil)
		p.rec.waitNetwork(t, netReady)
		out = append(out, p)
	}
	return out
}

func (p *peer) board(t *testing.T) Board {
	t.Helper()
	b := p.Board()
	if b == nil {
		t.Fatal("no board")
	}
	return *b
}

// column finds a column by title in the board as the person sees it.
func (b Board) column(title string) *Column {
	for i := range b.Columns {
		if b.Columns[i].Title == title {
			return &b.Columns[i]
		}
	}
	return nil
}

func (b Board) card(title string) (*Card, *Column) {
	for i := range b.Columns {
		for j := range b.Columns[i].Cards {
			if b.Columns[i].Cards[j].Title == title {
				return &b.Columns[i].Cards[j], &b.Columns[i]
			}
		}
	}
	return nil, nil
}

func (b Board) person(name string) *Person {
	for i := range b.People {
		if b.People[i].Name == name {
			return &b.People[i]
		}
	}
	return nil
}

// online reports whether the person is a member that this computer is connected to; false for a name not there.
func (b Board) online(name string) bool {
	p := b.person(name)
	return p != nil && p.Member && p.Online
}

func (b Board) titles() string {
	var parts []string
	for _, c := range b.Columns {
		var cards []string
		for _, k := range c.Cards {
			cards = append(cards, k.Title)
		}
		parts = append(parts, c.Title+":"+strings.Join(cards, ","))
	}
	return strings.Join(parts, " | ")
}

// waitBoard polls the board until pred holds.
func (p *peer) waitBoard(t *testing.T, what string, pred func(Board) bool) Board {
	t.Helper()
	var last Board
	deadline := time.Now().Add(waitFor)
	for time.Now().Before(deadline) {
		if b := p.Board(); b != nil {
			last = *b
			if pred(last) {
				return last
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	var people []string
	for _, q := range last.People {
		people = append(people, fmt.Sprintf("%s(member=%v admin=%v seat=%v online=%v syncing=%v)", q.Name, q.Member, q.Admin, q.Seat, q.Online, q.Syncing))
	}
	braidStatus := ""
	if b := p.BoardService.board(); b != nil && b.braid.Load() != nil {
		st := b.braid.Load().Status()
		braidStatus = fmt.Sprintf("seats %d quorum %d pending %d awaiting %d unconfirmed %v halted %v settlementHalted %v", st.Seats, st.Quorum,
			len(st.PendingConfig), len(st.AwaitingReadiness), st.Unconfirmed, st.Halted, st.SettlementHalted)
	}
	t.Fatalf("never %s; %s (%s): the board is %q in state %s/%s %+v %+v; people %v", what, p.displayName(), braidStatus, last.titles(), last.State, last.Phase, last.Sync, last.Error, people)
	return last
}

// join has the person join with a code and waits until the board is active.
func (p *peer) join(t *testing.T, code string) Board {
	t.Helper()
	if _, err := p.JoinBoard(mainCtx, code); err != nil {
		t.Fatal(err)
	}
	return p.waitBoard(t, "active after a join", func(b Board) bool { return b.State == stateActive && b.person(p.displayName()) != nil })
}

// invited makes an invite at a and has b join with it.
func invited(t *testing.T, a, b *peer, opts InviteOptions) Board {
	t.Helper()
	info, err := a.CreateInvite(mainCtx, opts)
	if err != nil {
		t.Fatal(err)
	}
	return b.join(t, info.Code)
}

// nudge writes an entry that changes nothing (the member's own name again): the work a pending settler config needs to take
// effect on an idle board.
func (p *peer) nudge(t *testing.T) {
	t.Helper()
	b := p.BoardService.board()
	if b == nil {
		t.Fatal("no board")
	}
	if _, err := b.write(op{Kind: opProfile, Text: p.displayName()}); err != nil {
		t.Fatal(err)
	}
}

// awaitSeats waits until every person sees `seats` settlers, all `online` of them connected and settling, writing an entry
// from the first person every two seconds so that a pending settler config takes effect.
func awaitSeats(t *testing.T, ps []*peer, seats, online int) {
	t.Helper()
	last := time.Now().Add(-time.Hour)
	deadline := time.Now().Add(waitFor)
	for time.Now().Before(deadline) {
		ok := true
		for _, p := range ps {
			b := p.Board()
			ok = ok && b != nil && b.Sync.Seats == seats && b.Sync.SeatsOnline >= online && b.Sync.Settling
		}
		if ok {
			return
		}
		if time.Since(last) > 2*time.Second {
			ps[0].nudge(t)
			last = time.Now()
		}
		time.Sleep(50 * time.Millisecond)
	}
	for _, p := range ps {
		p.waitBoard(t, "had the settlers", func(x Board) bool { return x.Sync.Seats == seats && x.Sync.SeatsOnline >= online })
	}
}
