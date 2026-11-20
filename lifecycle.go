package main

import (
	"bytes"
	"context"
	"errors"
	"log"
	"path/filepath"
	"strings"
	"time"

	kolobraid "github.com/ciranda-dev/kolo-braid"
	invite "github.com/ciranda-dev/kolo-invite"
	kolostore "github.com/ciranda-dev/kolo-store"
	koloswarm "github.com/ciranda-dev/kolo-swarm"
)

const (
	// joinSyncTimeout is how long a join waits to see itself as a member after the welcome: the time after which the braid
	// closes the connection of a key it does not know (kolo-braid docs/limits.md, Sync bounds).
	joinSyncTimeout = time.Minute
)

// defaultColumns are the columns of a board whose creator gave none; the window passes its own, translated.
var defaultColumns = []string{"To do", "Doing", "Done"}

func (b *board) isJoining() bool {
	st, _ := b.getState()
	return st == stateJoining || st == stateCreating
}

// finish says that nothing of the board's setup is running any more; it may be called more than once.
func (b *board) finish() { b.finishOnce.Do(func() { close(b.done) }) }

// goServe runs f as one of the board's connections, unless the board is closing.
func (b *board) goServe(f func()) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closing {
		return false
	}
	b.serveWG.Add(1)
	go func() {
		defer b.serveWG.Done()
		f()
	}()
	return true
}

// begin registers a new board as the only one. It checks what every way to a board has in common.
func (s *BoardService) begin(state, phase string) (*board, error) {
	if _, err := cleanName(s.displayName()); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.ctx == nil || !s.running:
		return nil, boardErr("app-closed", ErrClosed, "")
	case s.restarting:
		return nil, boardErr("busy-restarting", ErrBusy, "")
	case s.b != nil && s.b.isJoining():
		return nil, boardErr("busy-joining", ErrBusy, "")
	case s.b != nil:
		return nil, boardErr("board-exists", ErrBusy, "")
	}
	b := newBoard(s, state, phase)
	s.b = b
	return b, nil
}

// abandon is the end of a board that did not come to be: its files are removed and the windows are told why.
func (s *BoardService) abandon(b *board, why error) {
	b.close()
	s.removeBoardFiles()
	s.ended(b, explain(why))
	b.finish()
}

func (s *BoardService) removeBoardFiles() {
	dir := s.settingsCopy().DataDir
	if err := removeAllInDir(filepath.Join(dir, markerFile)); err != nil {
		log.Print("removing the board marker: ", err)
	}
	if err := removeAllInDir(filepath.Join(dir, boardDir)); err != nil {
		log.Print("removing the board: ", err)
	}
}

func (s *BoardService) boardOptions(db kolostore.Backend) kolobraid.Options {
	return kolobraid.Options{Backend: db, App: appTag, Apply: apply}
}

// openExisting opens the board of the data directory, if one was completed. Something left by a join or a create that
// did not finish is removed. A board that cannot be opened stays as an ended one, so that LeaveBoard can remove it.
func (s *BoardService) openExisting() {
	dir := s.set.DataDir
	m, ok, err := readMarker(dir)
	if err != nil {
		log.Print("reading the board marker: ", err)
		return
	}
	if !ok {
		s.removeBoardFiles()
		return
	}
	b := newBoard(s, stateActive, "")
	b.finish()
	s.mu.Lock()
	s.b = b
	s.mu.Unlock()
	fail := func(err error) {
		b.mu.Lock()
		b.state, b.err = stateEnded, boardErr("board-open", err, "")
		b.mu.Unlock()
	}
	db, err := kolostore.OpenPebble(filepath.Join(dir, boardDir))
	if err != nil {
		fail(err)
		return
	}
	braid, err := kolobraid.Open(s.ctx, s.id, s.boardOptions(db))
	if err != nil {
		db.Close()
		fail(err)
		return
	}
	b.attach(braid, db)
	b.mu.Lock()
	b.member = m.Member
	b.mu.Unlock()
	if !m.Member {
		b.move(stateActive, stateJoining, phaseSyncing)
		s.wg.Go(func() { s.finishJoin(b) })
		return
	}
	s.wg.Go(func() { s.checkStillMember(b) })
}

// checkStillMember tells whether a board that this member was in still lists it. The roster may not be in the view yet
// right after the board is opened, so the board is taken to have removed this member only when it still does not list it
// after writeWait.
func (s *BoardService) checkStillMember(b *board) {
	deadline := time.NewTimer(writeWait)
	defer deadline.Stop()
	tick := time.NewTicker(eventGap)
	defer tick.Stop()
	for !b.isMember() {
		select {
		case <-tick.C:
		case <-deadline.C:
			b.markRemoved()
			return
		case <-b.ctx.Done():
			return
		}
	}
}

// isMember reports whether the view lists this member, and remembers when it does (checkListed).
func (b *board) isMember() bool {
	br := b.braid.Load()
	if br == nil {
		return false
	}
	v := br.View()
	defer v.Close()
	for _, m := range v.Members() {
		if bytes.Equal(m.Key, b.selfKey()) {
			b.mu.Lock()
			b.listed = true
			b.mu.Unlock()
			return true
		}
	}
	return false
}

// noteMember remembers in the marker that this member has been seen as a member: a board that no longer lists it was left
// by a removal, not by a join that has not finished.
func (b *board) noteMember() {
	b.mu.Lock()
	known := b.member
	b.mu.Unlock()
	if known || !b.isMember() {
		return
	}
	br := b.braid.Load()
	gid := br.GroupID()
	if err := writeMarker(b.svc.settingsCopy().DataDir, marker{Version: 1, Group: gid.String(), Member: true}); err != nil {
		log.Print("writing the board marker: ", err)
		return
	}
	b.mu.Lock()
	b.member = true
	b.mu.Unlock()
}

// CreateBoard makes a board with the given title and columns (the three of defaultColumns when none is given) and returns
// it: the creator is its first member and its admin. It needs no network.
func (s *BoardService) CreateBoard(title string, columnTitles []string) (Board, error) {
	title, err := checkLine(title, maxBoardTitleRunes, "board-title", ErrBadText)
	if err != nil {
		return Board{}, err
	}
	if len(columnTitles) == 0 {
		columnTitles = defaultColumns
	}
	cols := make([]string, len(columnTitles))
	for i, c := range columnTitles {
		if cols[i], err = checkLine(c, maxColumnTitleRunes, "column-title", ErrBadText); err != nil {
			return Board{}, err
		}
	}
	s.lifeMu.Lock()
	defer s.lifeMu.Unlock()
	b, err := s.begin(stateCreating, "")
	if err != nil {
		return Board{}, err
	}
	b.finish()
	if err := s.create(b, title, cols); err != nil {
		s.abandon(b, err)
		return Board{}, explain(err)
	}
	return b.snapshot(), nil
}

func (s *BoardService) create(b *board, title string, cols []string) error {
	dir := s.set.DataDir
	if err := removeAllInDir(filepath.Join(dir, boardDir)); err != nil {
		return err
	}
	db, err := kolostore.OpenPebble(filepath.Join(dir, boardDir))
	if err != nil {
		return err
	}
	braid, err := kolobraid.Create(b.ctx, s.id, kolobraid.GroupConfig{App: appTag}, s.boardOptions(db))
	if err != nil {
		db.Close()
		return err
	}
	gid := braid.GroupID()
	if err := writeMarker(dir, marker{Version: 1, Group: gid.String(), Member: true}); err != nil {
		braid.Close()
		db.Close()
		return err
	}
	b.attach(braid, db)
	b.mu.Lock()
	b.member = true
	b.mu.Unlock()
	if _, err := b.write(op{Kind: opProfile, Text: s.displayName()}); err != nil {
		return err
	}
	if _, err := b.write(op{Kind: opTitle, Text: title}); err != nil {
		return err
	}
	var prev []byte
	for _, c := range cols {
		pos := between(prev, nil)
		if pos == nil {
			return ErrNoRoom
		}
		if _, err := b.write(op{Kind: opColAdd, Text: c, Pos: pos}); err != nil {
			return err
		}
		prev = pos
	}
	if err := b.awaitView(func(m *model) bool { return len(m.Columns) == len(cols) && m.Title == title }); err != nil {
		return err
	}
	b.move(stateCreating, stateActive, "")
	s.attachNetwork(b)
	return nil
}

// attachNetwork joins the board's topic on the network that runs, if one does; the network that starts later does it
// itself.
func (s *BoardService) attachNetwork(b *board) {
	if ctx, sw := s.netNow(); sw != nil {
		s.joinBoardTopic(ctx, sw, b)
	}
}

// netNow returns the context and the swarm of the network when it is ready, else nils.
func (s *BoardService) netNow() (context.Context, *koloswarm.Swarm) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.net.State != netReady || s.sw == nil {
		return nil, nil
	}
	return s.netCtx, s.sw
}

// ---- joining ----

// timeoutAfter fires d from now; zero never fires.
func timeoutAfter(d time.Duration) <-chan time.Time {
	if d == 0 {
		return nil
	}
	return time.After(d)
}

// joinWait is what the swarm loop looks for while a join waits for its inviter.
type joinWait struct {
	code invite.Code
	ch   chan *koloswarm.Peer
}

// offerJoin hands a connection to the join that waits for it, if it is the inviter's.
func (s *BoardService) offerJoin(p *koloswarm.Peer) bool {
	s.mu.Lock()
	w := s.joinWait
	s.mu.Unlock()
	if w == nil || !w.code.Matches(p.PublicKey()) {
		return false
	}
	select {
	case w.ch <- p:
	default:
	}
	return true
}

// maxCodeText bounds the pasted text JoinBoard looks at: an invite code is far shorter, and the window sends what was
// pasted.
const maxCodeText = 1024

func parseInviteText(text string) (invite.Code, error) {
	if len(text) > maxCodeText {
		return invite.Code{}, invite.ErrInvalidCode
	}
	return invite.Parse(strings.Join(strings.Fields(text), ""))
}

// JoinBoard joins the board of an invite. Spaces and line breaks in the pasted text are ignored. It returns at once with
// the board in state "joining"; the join moves on in "board:state" events: finding the person who invited, waiting for
// their answer (an admin may have to approve), then the board arriving. It ends as "ended" with an error when it fails.
func (s *BoardService) JoinBoard(ctx context.Context, text string) (Board, error) {
	if !fromMainWindow(ctx) {
		return Board{}, boardErr("main-window-only", ErrMainOnly, "")
	}
	code, err := parseInviteText(text)
	if err != nil {
		return Board{}, boardErr("invalid-invite", err, "")
	}
	s.lifeMu.Lock()
	defer s.lifeMu.Unlock()
	if s.swarm() == nil {
		return Board{}, explain(ErrNotReady)
	}
	b, err := s.begin(stateJoining, phaseFinding)
	if err != nil {
		return Board{}, err
	}
	s.wg.Go(func() {
		defer b.finish()
		if err := s.runJoin(b, code); err != nil {
			s.abandon(b, err)
		}
	})
	b.kick()
	return b.snapshot(), nil
}

// runJoin is the joiner's side of the flow the braid and kolo-invite describe: a new strand and its request, the inviter's
// connection, the request with the invite, the welcome, and Complete.
func (s *BoardService) runJoin(b *board, code invite.Code) error {
	ctx := b.ctx
	dir := s.set.DataDir
	if err := removeAllInDir(filepath.Join(dir, boardDir)); err != nil {
		return err
	}
	db, err := kolostore.OpenPebble(filepath.Join(dir, boardDir))
	if err != nil {
		return err
	}
	b.db = db
	join, err := kolobraid.NewJoin(ctx, s.id, s.boardOptions(db))
	if err != nil {
		return err
	}
	request, err := join.Request(joinData(s.displayName()))
	if err != nil {
		return err
	}
	sw := s.swarm()
	if sw == nil {
		return ErrNotReady
	}
	w := &joinWait{code: code, ch: make(chan *koloswarm.Peer, 1)}
	s.mu.Lock()
	s.joinWait = w
	s.mu.Unlock()
	stopWaiting := func() {
		s.mu.Lock()
		s.joinWait = nil
		s.mu.Unlock()
	}
	defer stopWaiting()
	if err := sw.Join(ctx, code.Topic(), &koloswarm.JoinOptions{Lookup: true}); err != nil {
		return boardErr("lookup-failed", err, "")
	}
	var peer *koloswarm.Peer
	select {
	case peer = <-w.ch:
	case <-timeoutAfter(s.set.FindTimeout):
		sw.Leave(code.Topic())
		if len(s.set.relays) > 0 {
			return boardErr("inviter-not-found-relays", ErrNotFound, "")
		}
		return ErrNotFound
	case <-ctx.Done():
		sw.Leave(code.Topic())
		return ctx.Err()
	}
	b.setPhase(phaseWaiting)
	actx, cancel := context.WithCancel(ctx)
	if s.set.JoinTimeout > 0 {
		actx, cancel = context.WithTimeout(ctx, s.set.JoinTimeout)
	}
	defer cancel()
	welcome, err := invite.JoinConn(actx, peer.Conn(), s.id, code, &invite.JoinOptions{Data: request})
	stopWaiting()          // from here on a connection of the inviter is the board's
	sw.Leave(code.Topic()) // the invite-only connection closes; the board gets its own
	peer.Conn().Close()
	if err != nil {
		return err
	}
	if !b.move(stateJoining, stateJoining, phaseSyncing) {
		return ctx.Err()
	}
	braid, err := join.Complete(ctx, welcome)
	if err != nil {
		if errors.Is(err, kolobraid.ErrMalformed) || errors.Is(err, kolobraid.ErrInvalidArgument) {
			return ErrBadWelcome
		}
		return err
	}
	gid := braid.GroupID()
	if err := writeMarker(dir, marker{Version: 1, Group: gid.String()}); err != nil {
		braid.Close()
		return err
	}
	b.attach(braid, db)
	s.attachNetwork(b)
	return s.finishJoin(b)
}

// finishJoin waits until the board lists this member, then makes the board active. If that does not happen in time,
// the join failed and everything it made is removed.
func (s *BoardService) finishJoin(b *board) error {
	deadline := time.NewTimer(joinSyncTimeout)
	defer deadline.Stop()
	tick := time.NewTicker(eventGap)
	defer tick.Stop()
	for !b.isMember() {
		select {
		case <-tick.C:
		case <-deadline.C:
			err := boardErr("join-sync-timeout", ErrNotFound, "")
			s.abandonLate(b, err)
			return nil
		case <-b.ctx.Done():
			return b.ctx.Err()
		}
	}
	b.noteMember()
	b.move(stateJoining, stateActive, "")
	b.kickName()
	return nil
}

// abandonLate ends a join that failed after its braid was attached.
func (s *BoardService) abandonLate(b *board, why error) {
	s.abandon(b, why)
}

// LeaveBoard leaves the board, or cancels a join, as Board.Leave says: an admin first removes itself for every member,
// then this computer forgets the board and removes its copy of it, and the invites that were made here end. The last
// admin and the only settler are refused; a removal that did not reach another member keeps the copy (leave-not-sent),
// and leaving again deletes it.
func (s *BoardService) LeaveBoard() error {
	s.lifeMu.Lock()
	defer s.lifeMu.Unlock()
	b := s.board()
	if b == nil {
		return boardErr("no-board", ErrNoBoard, "")
	}
	switch b.leavePlan() {
	case leaveLastAdmin:
		return boardErr("last-admin", ErrLastAdmin, "")
	case leaveLastSettler:
		return boardErr("last-settler", ErrLastSettler, "")
	case leaveEveryone:
		if !b.removeSelf() {
			b.mu.Lock()
			b.leaveTried = true
			b.mu.Unlock()
			b.kick()
			return boardErr("leave-not-sent", ErrNotSent, "")
		}
	}
	b.cancel()
	<-b.done
	if s.board() != b { // a join that was cancelled ended itself
		return nil
	}
	if sw := s.swarm(); sw != nil && b.braid.Load() != nil {
		sw.Leave(b.topic)
	}
	s.revokeAll()
	b.close()
	s.removeBoardFiles()
	s.ended(b, nil)
	return nil
}
