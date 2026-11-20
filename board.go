package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"log"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	kolobraid "github.com/ciranda-dev/kolo-braid"
	kolostore "github.com/ciranda-dev/kolo-store"
	koloswarm "github.com/ciranda-dev/kolo-swarm"
)

// Waits, each with its source.
const (
	// writeWait is how long a change waits for the own strand to be confirmed after the board was opened: the braid's
	// default ConfirmWait (kolo-braid docs/limits.md, Limits).
	writeWait = 10 * time.Second
	// eventGap is the least time between two "board:state" events: the braid's default AnnounceGap, the rate at which
	// it learns of changes from one peer (kolo-braid docs/limits.md, Limits).
	eventGap = 100 * time.Millisecond
)

// board is the one board of the app: its braid, its state, and who is connected. The state machine is
//
//	creating -> active
//	joining (finding -> waiting -> syncing) -> active
//	active -> removed
//	creating | joining | active | removed -> ended      (a failure, or LeaveBoard)
//
// An ended board is forgotten, except one that could not be opened, which stays until LeaveBoard removes its files.
type board struct {
	svc    *BoardService
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup // the goroutines of the board
	done   chan struct{}  // closed when a join has finished, one way or the other

	braid atomic.Pointer[kolobraid.Braid]
	db    kolostore.Backend
	topic koloswarm.Topic

	wake chan struct{} // the emitter: a signal that something changed
	name chan struct{} // the profile writer: the name or the board changed

	mu      sync.Mutex // guards the fields below
	state   string
	phase   string
	err     *BoardError
	peers   map[string]*koloswarm.Peer
	seen    map[oid]bool // the activity records from watch on that were already counted as changes by others
	watch   uint64       // the activity records before this number are settled and were looked at
	unseen  int
	last    *Change
	member  bool // this member has been seen in the roster: the marker says so too
	closing bool
	listed  bool // the view listed this member since the board was attached
	stuck   bool // this member's own changes stopped settling: see stallLoop
	// leaveTried is set when LeaveBoard could not send the removal: leaving again deletes only this computer's copy.
	leaveTried bool

	serveWG    sync.WaitGroup // the connections the board serves
	finishOnce sync.Once
	closeOnce  sync.Once
}

func newBoard(s *BoardService, state, phase string) *board {
	b := &board{svc: s, done: make(chan struct{}), wake: make(chan struct{}, 1), name: make(chan struct{}, 1),
		state: state, phase: phase, peers: map[string]*koloswarm.Peer{}}
	b.ctx, b.cancel = context.WithCancel(s.ctx)
	return b
}

func (b *board) kick() {
	select {
	case b.wake <- struct{}{}:
	default:
	}
}

func (b *board) kickName() {
	select {
	case b.name <- struct{}{}:
	default:
	}
}

// move changes the state when it is still from; it reports whether it did.
func (b *board) move(from, to, phase string) bool {
	b.mu.Lock()
	ok := b.state == from
	if ok {
		b.state, b.phase = to, phase
	}
	b.mu.Unlock()
	if ok {
		b.kick()
	}
	return ok
}

func (b *board) setPhase(phase string) {
	b.mu.Lock()
	b.phase = phase
	b.mu.Unlock()
	b.kick()
}

func (b *board) getState() (string, string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state, b.phase
}

// attach gives the board its braid and starts what runs while it exists.
func (b *board) attach(braid *kolobraid.Braid, db kolostore.Backend) {
	b.db = db
	b.topic = koloswarm.Topic(braid.Topic())
	b.braid.Store(braid)
	b.wg.Go(b.eventLoop)
	b.wg.Go(b.emitLoop)
	b.wg.Go(b.profileLoop)
	b.wg.Go(b.stallLoop)
	b.initSeen()
	b.kickName()
}

// close stops the goroutines of the board and closes its braid and store.
func (b *board) close() {
	b.closeOnce.Do(func() {
		b.mu.Lock()
		b.closing = true
		b.mu.Unlock()
		b.cancel()
		b.wg.Wait()
		b.serveWG.Wait()
		if br := b.braid.Load(); br != nil {
			br.Close()
		}
		if b.db != nil {
			b.db.Close()
		}
	})
}

// awaitView waits until the view, as the board shows it, satisfies ok, at most writeWait.
func (b *board) awaitView(ok func(*model) bool) error {
	br := b.braid.Load()
	deadline := time.After(writeWait)
	for {
		v := br.View()
		m := readModel(v)
		v.Close()
		if ok(m) {
			return nil
		}
		select {
		case <-time.After(10 * time.Millisecond):
		case <-deadline:
			return ErrNotReady
		case <-b.ctx.Done():
			return b.ctx.Err()
		}
	}
}

// peerUp and peerDown keep the set of members this computer is connected to.
func (b *board) peerUp(p *koloswarm.Peer) {
	b.mu.Lock()
	b.peers[string(p.PublicKey())] = p
	b.mu.Unlock()
	b.kick()
}

func (b *board) peerDown(p *koloswarm.Peer) {
	b.mu.Lock()
	if b.peers[string(p.PublicKey())] == p {
		delete(b.peers, string(p.PublicKey()))
	}
	b.mu.Unlock()
	b.kick()
}

func (b *board) clearPeers() {
	b.mu.Lock()
	clear(b.peers)
	b.mu.Unlock()
	b.kick()
}

func (b *board) online(key []byte) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	p := b.peers[string(key)]
	return p != nil && p.Conn().Context().Err() == nil
}

// eventLoop follows the braid's events: what changes the board's state is handled here, everything else is a reason to
// show the board again.
func (b *board) eventLoop() {
	br := b.braid.Load()
	for {
		ev, err := br.Next(b.ctx)
		if err != nil {
			return
		}
		switch ev.Kind {
		case kolobraid.EventRemoved:
			b.markRemoved()
		case kolobraid.EventStrandLost, kolobraid.EventHalted, kolobraid.EventDiverged, kolobraid.EventSettlementHalted:
			log.Printf("board: %v at %d", ev.Kind, ev.Position)
		}
		b.checkListed()
		b.kick()
	}
}

func (b *board) markRemoved() {
	b.mu.Lock()
	if b.state == stateActive {
		b.state = stateRemoved
	}
	b.mu.Unlock()
	b.kick()
}

// emitLoop shows the board to the windows when something changed, at most once per eventGap.
func (b *board) emitLoop() {
	for {
		select {
		case <-b.ctx.Done():
			return
		case <-b.wake:
		}
		b.svc.boardChanged(b)
		select {
		case <-b.ctx.Done():
			return
		case <-time.After(eventGap):
		}
	}
}

// initSeen counts the activity that is already there as seen: only what comes later is a change by someone else.
func (b *board) initSeen() {
	br := b.braid.Load()
	v, s := br.View(), br.Settled()
	defer v.Close()
	defer s.Close()
	from := activityTotal(s)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.seen = map[oid]bool{}
	b.watch = from
	for _, it := range readActivityFrom(v, from) {
		b.seen[it.Entry] = true
	}
}

// profileLoop writes the member's own name into the board whenever the view has another one, once the board accepts
// writes. A write that has to wait for another member is tried again after writeWait.
func (b *board) profileLoop() {
	for {
		select {
		case <-b.ctx.Done():
			return
		case <-b.name:
		case <-time.After(writeWait):
		}
		want := b.svc.displayName()
		if want == "" {
			continue
		}
		st, _ := b.getState()
		br := b.braid.Load()
		if st != stateActive || br == nil || b.nameInView(br) == want {
			continue
		}
		if _, err := b.write(op{Kind: opProfile, Text: want}); err != nil {
			if b.ctx.Err() == nil && !errors.Is(err, kolobraid.ErrUnconfirmed) {
				log.Print("board: writing the name: ", explain(err).Reason)
			}
			continue
		}
		b.kick()
	}
}

func (b *board) nameInView(br *kolobraid.Braid) string {
	v := br.View()
	defer v.Close()
	n, err := v.Get(keyOf(prefixName, b.selfKey()))
	if err != nil {
		return ""
	}
	return string(n)
}

// write appends one operation to the own strand and waits until the view shows it, so that the next change is planned
// from a board that holds this one. It waits at most writeWait for the strand to be confirmed.
func (b *board) write(o op) (kolobraid.EntryRef, error) {
	br := b.braid.Load()
	if br == nil {
		return kolobraid.EntryRef{}, ErrNoBoard
	}
	body, err := o.encode()
	if err != nil {
		return kolobraid.EntryRef{}, err
	}
	ctx, cancel := context.WithTimeout(b.ctx, writeWait)
	defer cancel()
	from := br.Status().Settled // the entry comes after everything settled now
	ref, err := br.Append(ctx, body)
	if err != nil {
		if errors.Is(err, kolobraid.ErrRemoved) {
			b.markRemoved()
		}
		return ref, err
	}
	b.awaitApplied(br, ref, from)
	return ref, nil
}

// awaitApplied waits until the view has applied the entry, rejected or not, at most writeWait. The entry is found by its
// ID in the order from position from on, and applied is when the view's last applied position has reached it.
func (b *board) awaitApplied(br *kolobraid.Braid, ref kolobraid.EntryRef, from uint64) {
	deadline := time.After(writeWait)
	var pos uint64
	found := false
	for {
		if !found {
			for o, err := range br.Entries(from) {
				if err != nil {
					break
				}
				if o.ID == ref.ID {
					pos, found = o.Position, true
					break
				}
			}
		}
		if found {
			v := br.View()
			applied := v.Position() >= pos
			v.Close()
			if applied {
				return
			}
		}
		select {
		case <-time.After(5 * time.Millisecond):
		case <-deadline:
			return
		case <-b.ctx.Done():
			return
		}
	}
}

// selfKey is the key of this member: the braid's own identity.
func (b *board) selfKey() []byte { return b.svc.id.PublicKey() }

// ---- the board as a window shows it ----

// snapshot reads the board: the view with the settled one next to it, to tell what is settled.
func (b *board) snapshot() Board {
	seq := b.svc.boardSeq.Add(1) // before the reads: a later Seq read the board later
	st, phase := b.getState()
	out := Board{Seq: seq, State: st, Phase: phase, Columns: []Column{}, People: []Person{}, Requests: []JoinRequest{}, Self: hex.EncodeToString(b.selfKey())}
	b.mu.Lock()
	out.Error = b.err
	out.Unseen = b.unseen
	out.Sync.Stuck = b.stuck
	b.mu.Unlock()
	out.Leave = b.leavePlan()
	br := b.braid.Load()
	if br == nil {
		return out
	}
	gid := br.GroupID()
	out.ID = hex.EncodeToString(gid[:])
	v, s := br.View(), br.Settled()
	defer v.Close()
	defer s.Close()
	mv, ms := readModel(v), readModel(s)
	out.Title = cleanLine(mv.Title)
	status := br.Status()

	settledCards := map[oid][]byte{}
	for _, c := range ms.Columns {
		for _, k := range c.Cards {
			settledCards[k.ID] = k.encode()
		}
	}
	settledCols := map[oid][]byte{}
	for _, c := range ms.Columns {
		settledCols[c.ID] = c.encode()
	}
	for _, c := range mv.Columns {
		col := Column{ID: c.ID.String(), Title: cleanLine(c.Title), Cards: []Card{}, Syncing: !bytes.Equal(settledCols[c.ID], c.encode())}
		if col.Syncing {
			out.Sync.ColumnsSyncing++
		}
		for _, k := range c.Cards {
			sync := !bytes.Equal(settledCards[k.ID], k.encode())
			if sync {
				out.Sync.CardsSyncing++
			} else {
				out.Sync.CardsSettled++
			}
			col.Cards = append(col.Cards, Card{ID: k.ID.String(), Title: cleanLine(k.Title), Description: cleanText(k.Desc),
				Assignee: assigneeKey(k.card), Color: colors[min(int(k.Color), maxColor)], CreatedBy: k.CreatedBy.String(), LastBy: k.LastBy.String(), Syncing: sync})
		}
		out.Columns = append(out.Columns, col)
	}
	out.People = b.people(v, s, mv, out.Columns)
	out.Sync.Pending = pendingEntries(br, status.Settled)
	out.Sync.Seats, out.Sync.Quorum = status.Seats, status.Quorum
	for _, p := range out.People {
		if p.Member && p.Seat && p.Online {
			out.Sync.SeatsOnline++
		}
	}
	out.Sync.SingleSettler = status.SingleSettler
	out.Sync.PendingSeats = len(status.PendingConfig)
	out.Sync.Settling = status.SettlementHalted == nil && out.Sync.SeatsOnline >= status.Quorum
	if status.SettlementHalted != nil {
		out.Sync.Halted = boardErr("settlement-halted", status.SettlementHalted, "")
	}
	out.CanEdit, out.Blocked = b.canEdit(st, status)
	if out.Error == nil && st == stateActive {
		out.Error = problem(status)
	}
	return out
}

func assigneeKey(c card) string {
	if !c.HasAssignee {
		return ""
	}
	return c.Assignee.String()
}

// problem is the status of a running braid as an error, when it stopped for a reason that is not the person's to fix.
func problem(st kolobraid.Status) *BoardError {
	switch {
	case st.Halted != nil:
		return boardErr("board-halted", st.Halted, "")
	case st.StrandLost:
		return boardErr("strand-lost", kolobraid.ErrStrandLost, "")
	}
	return nil
}

// canEdit says whether a change would be written now, and if not, why not.
func (b *board) canEdit(state string, st kolobraid.Status) (bool, *BoardError) {
	switch {
	case state == stateRemoved:
		return false, boardErr("you-were-removed", kolobraid.ErrRemoved, "")
	case state != stateActive:
		return false, boardErr("board-not-active", ErrBusy, "")
	case st.Halted != nil:
		return false, boardErr("board-halted", st.Halted, "")
	case st.StrandLost:
		return false, boardErr("strand-lost", kolobraid.ErrStrandLost, "")
	case st.Unconfirmed:
		return false, boardErr("strand-unconfirmed", kolobraid.ErrUnconfirmed, "")
	}
	return true, nil
}

// people lists the members of the view and the former members that cards name, with the names they wrote.
func (b *board) people(v, s *kolobraid.Snapshot, mv *model, cols []Column) []Person {
	settled := map[string]bool{}
	for _, m := range s.Members() {
		settled[string(m.Key)] = true
	}
	self := b.selfKey()
	out := []Person{} // a joiner's view has no roster until the board arrives: the window reads a list, never null
	have := map[oid]bool{}
	add := func(key []byte, m *kolobraid.Member) {
		var id oid
		copy(id[:], key)
		if have[id] || len(key) != idSize {
			return
		}
		have[id] = true
		p := Person{Key: id.String(), Name: peerName(mv.Names[id], key), Tag: keyTag(key), You: bytes.Equal(key, self)}
		if m != nil {
			p.Member, p.Admin, p.Seat = true, m.Roles&kolobraid.RoleAdmin != 0, m.Seat
			p.Syncing = !settled[string(key)]
		}
		p.Online = p.You && m != nil || b.online(key)
		out = append(out, p)
	}
	members := v.Members()
	slices.SortFunc(members, func(a, c kolobraid.Member) int { return bytes.Compare(a.Key, c.Key) })
	for _, m := range members {
		add(m.Key, &m)
	}
	for _, c := range cols {
		for _, k := range c.Cards {
			for _, hex := range []string{k.CreatedBy, k.LastBy, k.Assignee} {
				if id, ok := parseOID(hex); ok {
					add(id[:], nil)
				}
			}
		}
	}
	return out
}

// memberKey parses a key of a member in hex and checks that it names a member of the view.
func (b *board) memberKey(hexKey string) (ed25519.PublicKey, error) {
	id, ok := parseOID(hexKey)
	br := b.braid.Load()
	if !ok || br == nil {
		return nil, boardErr("member-not-found", ErrNoSuchItem, "")
	}
	v := br.View()
	defer v.Close()
	for _, m := range v.Members() {
		if bytes.Equal(m.Key, id[:]) {
			return ed25519.PublicKey(slices.Clone(m.Key)), nil
		}
	}
	return nil, boardErr("member-not-found", ErrNoSuchItem, "")
}

// pendingEntries counts the data entries after position settled that are not settled and not void: the changes of members
// that are in the view and could still move. The braid's own bookkeeping entries are not counted.
func pendingEntries(br *kolobraid.Braid, settled uint64) (n int) {
	for o, err := range br.Entries(settled) {
		if err != nil {
			break
		}
		if o.Kind == kolobraid.KindData && !o.Settled && !o.Void {
			n++
		}
	}
	return n
}
