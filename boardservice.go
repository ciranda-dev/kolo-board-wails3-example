package main

import (
	"context"
	"errors"
	"io/fs"
	"log"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	kolodht "github.com/ciranda-dev/kolo-dht"
	koloid "github.com/ciranda-dev/kolo-id"
	invite "github.com/ciranda-dev/kolo-invite"
	kolosecure "github.com/ciranda-dev/kolo-secure"
	kolostore "github.com/ciranda-dev/kolo-store"
	koloswarm "github.com/ciranda-dev/kolo-swarm"
	kolotransport "github.com/ciranda-dev/kolo-transport"
)

// testMaxConnsPerIP is a test seam: when not 0 it replaces kolo-transport's 4 connections per source address, so a test
// can run many apps on 127.0.0.1. The app never sets it.
var testMaxConnsPerIP int

// BoardService is the Go side of the window: it makes and joins a board, edits it, and lets people in. Every method is
// safe for concurrent use, and the window learns what happens through emit.
type BoardService struct {
	cfg Config
	// emit sends an event to the main window only: it is the one window that may know an invite.
	emit func(name string, data any)
	// emitNotch sends an event to the notch window only; nil: no notch window. It never gets an invite.
	emitNotch func(name string, data any)
	notch     notchWindow // nil: no notch window
	openApp   func()      // brings the main window to the front
	tray      trayUI      // nil: no tray icon

	// identity is a test hook: an identity kept in memory instead of the identity file.
	identity *koloid.Identity
	// accept is a test hook: when set, the swarm accepts a direct connection only from an address it allows.
	accept func(netip.AddrPort) bool
	// bootstrap is a test hook: it replaces the swarm's Bootstrap, which in the public network sends to the public servers.
	bootstrap func(context.Context, *koloswarm.Swarm) error

	startMu sync.Mutex // one Start or ServiceShutdown at a time
	started bool
	set     settings
	id      *koloid.Identity
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	inv     *invite.Inviter
	invDB   kolostore.Backend
	invMeta *inviteMeta

	lifeMu sync.Mutex // one create, join, leave or restart of the network at a time

	netMu     sync.Mutex // one start or stop of the network at a time
	netCancel context.CancelFunc
	netWG     sync.WaitGroup
	netEmitMu sync.Mutex // one "board:network" at a time, built and sent together, so they arrive in order

	relayMu     sync.Mutex
	relayStates map[string]map[string]RelayStatus // fromSwarm or fromCheck, then the relay key: what it last said
	relayLogged map[string]bool                   // relay key and reason: logged once for the app's run

	trayMu sync.Mutex // one tray update at a time, so they arrive in order

	prefMu sync.RWMutex // guards saved; held only briefly, never while calling out
	saved  savedSettings

	boardSeq atomic.Uint64 // the Seq of the last board read

	mu         sync.Mutex // guards the fields below
	net        Network
	sw         *koloswarm.Swarm
	netCtx     context.Context // the context of the network that runs
	node       *kolodht.Node
	hosting    string // the address of the entry node this app runs in -local, "" when another window does
	b          *board
	joinWait   *joinWait
	requests   map[invite.RequestID]*pendingRequest
	lastTray   TrayState
	restarting bool
	running    bool // between Start and ServiceShutdown: boards may start

	notchMu    sync.Mutex // one send to the notch window at a time, with the setting read as it sends
	notchTimer *time.Timer
	notchHold  bool   // the person asked for the notch: it stays until they hide it
	notchAuto  bool   // a change opened the notch: looking at the board hides it
	notchHook  func() // test hook, run between reading the notch state and sending it
}

// NewBoardService returns a service that reads cfg when it starts and tells the main window what happens through emit.
// The notch window gets its own events, through emitNotch.
func NewBoardService(cfg Config, emit func(name string, data any)) *BoardService {
	return &BoardService{cfg: cfg, emit: emit}
}

// Start opens the data directory, the identity and the board, if there is one, and starts the network in the
// background. It fails with a *BoardError of kind "config" for a bad setting. A second call returns the same Info.
func (s *BoardService) Start() (Info, error) {
	s.startMu.Lock()
	defer s.startMu.Unlock()
	if !s.started {
		if err := s.start(); err != nil {
			return Info{}, configError(err)
		}
		s.started = true
		s.pushTray()
	}
	return s.Info(), nil
}

func configSetting(ce *ConfigError) *BoardError {
	cause := ce.Err.Error()
	return &BoardError{Kind: kindConfig, Reason: "config-setting", Detail: ce.Setting, Cause: cause, Message: sentence("config-setting", ce.Setting, cause), Err: ce}
}

func configError(err error) error {
	var ce *ConfigError
	var be *BoardError
	switch {
	case errors.As(err, &ce):
		return configSetting(ce)
	case errors.As(err, &be):
		return be
	}
	return boardErr("data-dir", err, "")
}

func (s *BoardService) start() error {
	set, err := s.cfg.resolve()
	if err != nil {
		return err
	}
	if err := prepareDataDir(set.DataDir); err != nil {
		return err
	}
	saved, err := loadSaved(set.DataDir)
	if err != nil {
		return err
	}
	s.set = set
	s.prefMu.Lock()
	s.saved = saved
	s.prefMu.Unlock()
	if s.id = s.identity; s.id == nil {
		if s.id, err = loadIdentity(filepath.Join(set.DataDir, identityFile)); err != nil {
			return err
		}
	}
	s.ctx, s.cancel = context.WithCancel(context.Background())
	if err := s.openInviter(); err != nil {
		s.cancel()
		return err
	}
	s.mu.Lock()
	s.running = true
	s.requests = map[invite.RequestID]*pendingRequest{}
	s.mu.Unlock()
	s.openExisting()
	s.startNetwork()
	return nil
}

// loadIdentity reads the identity at path, or makes one and saves it, so that a board is always opened with the key it
// was made with.
func loadIdentity(path string) (*koloid.Identity, error) {
	id, err := koloid.Load(path)
	if !errors.Is(err, fs.ErrNotExist) {
		return id, err
	}
	if id, err = koloid.Generate(); err != nil {
		return nil, err
	}
	if err = id.SaveNew(path); err != nil && !errors.Is(err, koloid.ErrSavedNotSynced) {
		return nil, err
	}
	return id, nil
}

// ServiceShutdown stops the network, the board and the inviter. Wails calls it when the app quits.
func (s *BoardService) ServiceShutdown() error {
	s.startMu.Lock()
	defer s.startMu.Unlock()
	if !s.started {
		return nil
	}
	s.lifeMu.Lock()
	defer s.lifeMu.Unlock()
	s.mu.Lock()
	s.running = false // no board starts from now on
	b := s.b
	s.mu.Unlock()
	s.cancel()
	if b != nil {
		<-b.done
		b.close()
	}
	s.wg.Wait()
	s.stopNetwork()
	s.inv.Close()
	s.invDB.Close()
	s.mu.Lock()
	if s.notchTimer != nil {
		s.notchTimer.Stop()
		s.notchTimer = nil
	}
	s.b = nil
	s.mu.Unlock()
	s.started = false
	return nil
}

// Info returns the data directory, the version and the network state.
func (s *BoardService) Info() Info {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := ""
	if s.id != nil {
		key = s.id.PublicKeyString()
	}
	return Info{DataDir: s.set.DataDir, Version: version, Network: s.net, KeyID: key}
}

func (s *BoardService) settingsCopy() settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.set
}

func (s *BoardService) prefs() savedSettings {
	s.prefMu.RLock()
	defer s.prefMu.RUnlock()
	return s.saved
}

func (s *BoardService) displayName() string { return s.prefs().DisplayName }

// Settings returns the settings the app runs with.
func (s *BoardService) Settings() Settings {
	set := s.settingsCopy()
	p := s.prefs()
	first := ""
	if set.first.IsValid() {
		first = set.first.String()
	}
	return Settings{Mode: set.mode, EntryNodes: set.nodesText, EntryNodesFrom: set.nodesFrom, FirstNode: first, LocalNode: localNode(set), DataDir: set.DataDir, Listen: set.Listen,
		ConnectionRelays: set.relaysText, ConnectionRelaysFrom: set.relaysFrom,
		ShowInNotch: p.showInNotch(), NotchPreview: p.notchPreview(), DisplayName: p.DisplayName,
		FindTimeoutMs: set.FindTimeout.Milliseconds(), JoinTimeoutMs: set.JoinTimeout.Milliseconds(),
		LookupIntervalMs: set.LookupInterval.Milliseconds(), NotchLingerMs: set.NotchLinger.Milliseconds()}
}

// saveSaved changes the saved settings with f and writes them.
func (s *BoardService) saveSaved(f func(*savedSettings)) error {
	s.prefMu.Lock()
	defer s.prefMu.Unlock()
	next := s.saved
	f(&next)
	if err := saveSettings(s.settingsCopy().DataDir, next); err != nil {
		return err
	}
	s.saved = next
	return nil
}

// Reconnect starts the network again with the same settings, after it failed. It refuses while a join is running.
func (s *BoardService) Reconnect() error {
	s.startMu.Lock()
	defer s.startMu.Unlock()
	if !s.started {
		return explain(ErrNotReady)
	}
	s.lifeMu.Lock()
	defer s.lifeMu.Unlock()
	if !s.beginRestart() {
		return boardErr("busy-joining", ErrBusy, "")
	}
	defer s.endRestart()
	s.stopNetwork()
	s.startNetwork()
	return nil
}

// beginRestart marks the network as restarting unless a join runs, in one step, so no join starts on a network that is
// about to close.
func (s *BoardService) beginRestart() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.restarting || s.b != nil && s.b.isJoining() {
		return false
	}
	s.restarting = true
	return true
}

func (s *BoardService) endRestart() {
	s.mu.Lock()
	s.restarting = false
	s.mu.Unlock()
}

// SetDisplayName checks and saves the name other members see. The board gets it as soon as it accepts a change.
func (s *BoardService) SetDisplayName(name string) (Settings, error) {
	clean, err := cleanName(name)
	if err != nil {
		return Settings{}, err
	}
	if err := s.saveSaved(func(p *savedSettings) { p.DisplayName = clean }); err != nil {
		return Settings{}, boardErr("internal", err, "")
	}
	if b := s.board(); b != nil {
		b.kickName()
	}
	return s.Settings(), nil
}

// ---- the network ----

// startNetwork starts the first entry node, if configured, and the swarm, in the background.
func (s *BoardService) startNetwork() {
	s.netMu.Lock()
	defer s.netMu.Unlock()
	ctx, cancel := context.WithCancel(s.ctx)
	s.netCancel = cancel
	s.setNetwork(netStarting, nil)
	s.netWG.Go(func() { s.runNetwork(ctx) })
	s.checkRelays(ctx)
}

func (s *BoardService) stopNetwork() {
	s.netMu.Lock()
	defer s.netMu.Unlock()
	if s.netCancel != nil {
		s.netCancel()
	}
	s.mu.Lock()
	sw, node := s.sw, s.node
	s.sw, s.node, s.netCtx, s.hosting = nil, nil, nil, ""
	s.mu.Unlock()
	if sw != nil {
		sw.Close()
	}
	s.netWG.Wait()
	if node != nil {
		node.Close()
	}
	s.forgetRelays()
	if b := s.board(); b != nil {
		b.clearPeers()
	}
}

func (s *BoardService) runNetwork(ctx context.Context) {
	set := s.settingsCopy()
	var node *kolodht.Node
	host := set.first
	if set.local.IsValid() {
		host = set.local
	}
	if host.IsValid() {
		nid, err := koloid.Generate()
		if err == nil {
			node, err = kolodht.New(ctx, host.String(), nid, &kolodht.Config{Server: true})
		}
		switch {
		case err != nil && set.local.IsValid():
			// The port is taken, most likely by the window of this app that opened first: join its entry node.
			node = nil
		case err != nil:
			s.setNetwork(netFailed, boardErr("first-node-failed", err, set.first.String()))
			return
		}
	}
	cfg := swarmConfig(set)
	cfg.DHT.Secure.Transport.AcceptFunc = s.accept
	sw, err := koloswarm.New(ctx, set.Listen, s.id, cfg)
	if err == nil {
		// Without entry nodes of its own, kolo-swarm bootstraps from the public ones.
		boot := s.bootstrap
		if boot == nil {
			boot = func(ctx context.Context, sw *koloswarm.Swarm) error { return sw.Bootstrap(ctx, nil) }
		}
		if err = boot(ctx, sw); err != nil {
			sw.Close()
		}
	}
	if err != nil {
		if node != nil {
			node.Close()
		}
		if ctx.Err() == nil {
			s.setNetwork(netFailed, networkError(set, node, err))
		}
		return
	}
	s.mu.Lock()
	if ctx.Err() != nil {
		s.mu.Unlock()
		sw.Close()
		if node != nil {
			node.Close()
		}
		return
	}
	s.sw, s.node, s.netCtx = sw, node, ctx
	if node != nil && set.local.IsValid() {
		s.hosting = set.local.String()
	}
	s.mu.Unlock()
	log.Print("kolo-id ", s.id.PublicKeyString())
	s.setNetwork(netReady, nil)
	s.netWG.Go(func() {
		if err := s.inv.Announce(ctx, sw); err != nil && ctx.Err() == nil && !errors.Is(err, invite.ErrClosed) {
			log.Print("invite announce: ", err)
		}
	})
	if b := s.board(); b != nil && b.braid.Load() != nil {
		s.joinBoardTopic(ctx, sw, b)
	}
	s.swarmLoop(ctx, sw)
}

// swarmConfig is the swarm's configuration: the entry nodes (none in the public network, where kolo-swarm uses its
// built-in ones), the lookup interval and the connection relays of the settings, and the app's limits. The longest wait
// before a dial is retried is the lookup interval, as kolo-swarm keeps them in step (docs/limits.md:25).
func swarmConfig(set settings) *koloswarm.Config {
	return &koloswarm.Config{
		BootstrapNodes: set.nodes,
		LookupInterval: set.LookupInterval,
		RetryMax:       set.LookupInterval,
		Relays:         set.relays, // a peer that cannot be reached directly is reached through one, with the same Conn
		DHT:            &kolodht.Config{Secure: &kolosecure.Config{Transport: &kolotransport.Config{MaxConnsPerIP: testMaxConnsPerIP}}},
	}
}

func (s *BoardService) setNetwork(state string, why *BoardError) {
	s.netEmitMu.Lock()
	defer s.netEmitMu.Unlock()
	set := s.settingsCopy()
	n := Network{Mode: set.mode, State: state, EntryNodes: set.entryNodeCount(), LocalNode: localNode(set), Error: why, Relays: s.relayStatuses()}
	if why != nil {
		n.Message = why.Message
	}
	s.mu.Lock()
	n.FirstNode = s.hosting
	if set.first.IsValid() {
		n.FirstNode = set.first.String()
	}
	n.Seq = s.net.Seq + 1
	s.net = n
	s.mu.Unlock()
	s.emit("board:network", n)
}

// localNode is the address of -local, "" in the other networks.
func localNode(set settings) string {
	if set.local.IsValid() {
		return set.local.String()
	}
	return ""
}

// networkError is the error of a network that could not start, named for the network it was meant to reach. A swarm that
// did not start in the public network tells the person the three ways out.
func networkError(set settings, hosted *kolodht.Node, err error) *BoardError {
	switch {
	case set.mode == modePublic:
		return boardErr("public-unreachable", err, "")
	case set.mode == modeLocal && hosted == nil && errors.Is(err, kolodht.ErrNoNodes):
		return boardErr("local-port-taken", err, set.local.String()) // held by a program that is not this app
	}
	return boardErr("bootstrap-failed", err, set.nodesText)
}

// pushRelays sends the network again when the relays' states changed.
func (s *BoardService) pushRelays() {
	s.netEmitMu.Lock()
	defer s.netEmitMu.Unlock()
	relays := s.relayStatuses()
	s.mu.Lock()
	if sameRelays(relays, s.net.Relays) {
		s.mu.Unlock()
		return
	}
	s.net.Relays, s.net.Seq = relays, s.net.Seq+1
	n := s.net
	s.mu.Unlock()
	s.emit("board:network", n)
}

// swarm returns the swarm when the network is ready.
func (s *BoardService) swarm() *koloswarm.Swarm {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.net.State != netReady {
		return nil
	}
	return s.sw
}

func (s *BoardService) board() *board {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b
}

// joinBoardTopic joins the topic of the board's braid until it works, in the background: joining announces the board,
// which needs the entry nodes to answer.
func (s *BoardService) joinBoardTopic(ctx context.Context, sw *koloswarm.Swarm, b *board) {
	interval := s.settingsCopy().LookupInterval
	s.netWG.Go(func() {
		for ctx.Err() == nil && b.ctx.Err() == nil {
			err := sw.Join(ctx, b.topic, &koloswarm.JoinOptions{Announce: true, Lookup: true})
			if err == nil || ctx.Err() != nil {
				return
			}
			log.Print("board topic: ", err)
			select {
			case <-ctx.Done():
				return
			case <-b.ctx.Done():
				return
			case <-time.After(interval):
			}
		}
	})
}

// swarmLoop hands every connection the swarm makes to what wants it: a join waiting for its inviter, the board when the
// peer shares its topic, else the inviter, which answers requests to join.
func (s *BoardService) swarmLoop(ctx context.Context, sw *koloswarm.Swarm) {
	for {
		ev, err := sw.Next(ctx)
		if err != nil {
			return
		}
		switch ev.Kind {
		case koloswarm.Connected:
			s.take(ctx, ev.Peer)
		case koloswarm.Lost:
			if b := s.board(); b != nil && ev.Peer != nil {
				b.peerDown(ev.Peer)
			}
		case koloswarm.RelayChanged:
			s.relayChanged(fromSwarm, ev)
		}
	}
}

func (s *BoardService) take(ctx context.Context, p *koloswarm.Peer) {
	if s.offerJoin(p) {
		return
	}
	if b := s.board(); b != nil && b.braid.Load() != nil && slices.Contains(p.Topics(), b.topic) {
		s.serve(ctx, b, p)
		return
	}
	s.netWG.Go(func() { s.inv.ServePeer(ctx, p) })
}

// serve syncs the board with a peer until the connection, the network or the board ends. The connection is direct or
// through a connection relay; either way its key is the one the peer proved, and the braid serves only members.
func (s *BoardService) serve(netCtx context.Context, b *board, p *koloswarm.Peer) {
	conn := p.Conn()
	br := b.braid.Load()
	cctx, cancel := context.WithCancel(netCtx)
	stop := context.AfterFunc(b.ctx, cancel)
	started := b.goServe(func() {
		defer cancel()
		defer stop()
		b.peerUp(p)
		if err := br.ServeConn(cctx, conn); err != nil && cctx.Err() == nil && !errors.Is(err, context.Canceled) {
			log.Print("board: peer ", keyTag(p.PublicKey()), ": ", plainCause(err))
		}
		b.peerDown(p)
	})
	if !started {
		cancel()
		stop()
		conn.Close()
	}
}

// ---- what the windows ask ----

// Board returns the board now, or nil when there is none. The invite of a join is never part of it.
func (s *BoardService) Board() *Board {
	b := s.board()
	if b == nil {
		return nil
	}
	snap := b.snapshot()
	snap.Requests = s.requestList()
	return &snap
}

// ReportError writes an error of a window to the log, so that a person who runs the app from a terminal sees why a window
// failed. The window sends the error's message and component stack only, never board text or keys; the text is cleaned
// and cut to maxCardTitleRunes characters, the text bound of a title.
func (s *BoardService) ReportError(text string) {
	log.Print("frontend error: ", truncate(cleanText(text), maxCardTitleRunes))
}

// MarkSeen says that the window showed the changes of other members: the count of unseen changes, the notch and the tray
// icon go back to nothing new.
func (s *BoardService) MarkSeen() {
	if b := s.board(); b != nil {
		b.mu.Lock()
		b.unseen, b.last = 0, nil
		b.mu.Unlock()
		b.kick()
	}
	s.notchDone(nil)
}

// boardChanged tells the windows that the board changed, and counts the changes of other members.
func (s *BoardService) boardChanged(b *board) {
	changes := b.collectChanges()
	snap := b.snapshot()
	snap.Requests = s.requestList()
	s.emit("board:state", snap)
	for _, ch := range changes {
		s.emit("board:change", ch)
	}
	s.sendNotch(&snap, len(changes) > 0)
	s.pushTray()
}

// ended tells the windows that the board is over, with why, and forgets it.
func (s *BoardService) ended(b *board, why *BoardError) {
	s.mu.Lock()
	if s.b == b {
		s.b = nil
	}
	s.mu.Unlock()
	snap := Board{Seq: s.boardSeq.Add(1), State: stateEnded, Error: why, Columns: []Column{}, People: []Person{}, Requests: []JoinRequest{}}
	s.emit("board:state", snap)
	s.sendNotch(nil, false)
	s.pushTray()
}

func removeAllInDir(path string) error {
	err := os.RemoveAll(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
