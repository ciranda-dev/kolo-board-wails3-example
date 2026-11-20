package main

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"math"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"time"

	kolobraid "github.com/ciranda-dev/kolo-braid"
	kolocodec "github.com/ciranda-dev/kolo-codec"
	invite "github.com/ciranda-dev/kolo-invite"
	kolostore "github.com/ciranda-dev/kolo-store"
)

// Bounds of an invite, each from a library. The lifetime is the caller's (kolo-invite: zero never expires, so the window must
// give one); the people an invite takes default to one (kolo-invite CreateOptions.MaxUses: zero means 1) and are at most the
// members a braid holds by default (kolo-braid docs/limits.md:12 MaxMembers). Every active invite takes a topic of the
// swarm, which holds swarmTopics, with the board's own and a join's.
const (
	maxInviteUses    = defaultMaxMembers
	maxActiveInvites = swarmTopics - 2
)

// maxActivityPage is the most items one Activity call reads: the window asks for 50, and a call may not make the app
// read the whole list.
const maxActivityPage = 200

// maxPendingRequests is how many requests to join may wait for an admin at once, all invites together: each is a name an
// admin has to read. kolo-invite's default is 1024.
const maxPendingRequests = 64

// defaultMaxMembers is kolo-braid's default MaxMembers (GroupConfig, docs/limits.md:12), which the app does not change.
const defaultMaxMembers = 64

// joinTag starts the join data a joiner sends with its request: its display name.
const joinTag = "kolo-board/join/v0"

// joinData is what a joiner puts in the request for the admin to read.
func joinData(name string) []byte {
	var e kolocodec.Encoder
	e.PutString(joinTag)
	e.PutString(truncate(name, maxNameRunes))
	return e.Bytes()
}

// parseJoinData reads the join data: the name the joiner gave. It never panics on what a stranger sends.
func parseJoinData(b []byte) (string, error) {
	d := kolocodec.NewDecoder(b)
	if t, err := d.String(len(joinTag)); err != nil || t != joinTag {
		return "", errMalformed
	}
	name, err := d.String(textBytes(maxNameRunes))
	if err != nil {
		return "", errMalformed
	}
	if err := d.Done(); err != nil {
		return "", errMalformed
	}
	return name, nil
}

// inviteMeta keeps what the app decided about each invite and the inviter does not: whether a person must approve each
// joiner. It is a small file next to the inviter's store.
type inviteMeta struct {
	mu       sync.Mutex
	path     string
	approval map[string]bool
}

func loadInviteMeta(dir string) (*inviteMeta, error) {
	m := &inviteMeta{path: filepath.Join(dir, invitesMeta), approval: map[string]bool{}}
	b, err := readSmall(m.path)
	if err != nil {
		return nil, err
	}
	if b != nil && json.Unmarshal(b, &m.approval) != nil {
		m.approval = map[string]bool{}
	}
	return m, nil
}

func (m *inviteMeta) set(id invite.ID, approval bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.approval[id.String()] = approval
	return m.saveLocked()
}

// get says whether the invite needs approval; an invite the file does not know needs it, the safe answer.
func (m *inviteMeta) get(id invite.ID) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.approval[id.String()]
	return v || !ok
}

// keep drops what is known about invites that are no longer stored.
func (m *inviteMeta) keep(infos []invite.Info) {
	m.mu.Lock()
	defer m.mu.Unlock()
	live := map[string]bool{}
	for _, i := range infos {
		live[i.ID.String()] = true
	}
	n := len(m.approval)
	for k := range m.approval {
		if !live[k] {
			delete(m.approval, k)
		}
	}
	if len(m.approval) != n {
		if err := m.saveLocked(); err != nil {
			log.Print("invite settings: ", err)
		}
	}
}

func (m *inviteMeta) saveLocked() error {
	b, err := json.Marshal(m.approval)
	if err != nil {
		return err
	}
	return writeFileAtomic(m.path, b)
}

func inviteOptions() *invite.Options { return &invite.Options{MaxPending: maxPendingRequests} }

// openInviter opens the inviter on a store of its own and starts what runs next to it: the loop of its events.
func (s *BoardService) openInviter() (err error) {
	dir := s.set.DataDir
	if s.invMeta, err = loadInviteMeta(dir); err != nil {
		return err
	}
	if s.invDB, err = kolostore.OpenPebble(filepath.Join(dir, invitesDir)); err != nil {
		return err
	}
	if s.inv, err = invite.Open(s.invDB, s.id, inviteOptions()); err != nil {
		s.invDB.Close()
		return err
	}
	if infos, err := s.inv.List(); err == nil {
		s.invMeta.keep(infos)
	}
	s.wg.Go(s.inviteLoop)
	return nil
}

// pendingRequest is a person who asked to join with an invite that needs approval.
type pendingRequest struct {
	ev   invite.Event
	name string
}

func (s *BoardService) requestList() []JoinRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]JoinRequest, 0, len(s.requests))
	for id, p := range s.requests {
		out = append(out, JoinRequest{Request: strconv.FormatUint(uint64(id), 10), Invite: p.ev.Invite.String(),
			Joiner: hex.EncodeToString(p.ev.Joiner), Name: p.name, Tag: keyTag(p.ev.Joiner)})
	}
	slices.SortFunc(out, func(a, b JoinRequest) int { return cmpDecimal(a.Request, b.Request) })
	return out
}

func cmpDecimal(a, b string) int {
	if len(a) != len(b) {
		return len(a) - len(b)
	}
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// inviteLoop answers what the inviter reports: a request that needs no approval is answered at once, the others wait for
// an admin; the rest is a reason to show the invites again.
func (s *BoardService) inviteLoop() {
	for {
		ev, err := s.inv.Next(s.ctx)
		if err != nil {
			return
		}
		switch ev.Kind {
		case invite.Requested:
			s.onRequest(ev)
		case invite.Withdrawn:
			s.mu.Lock()
			delete(s.requests, ev.Request)
			s.mu.Unlock()
			s.kickBoard()
		default:
			s.emitInvites()
			s.kickBoard()
		}
	}
}

func (s *BoardService) kickBoard() {
	if b := s.board(); b != nil {
		b.kick()
	}
}

func (s *BoardService) onRequest(ev invite.Event) {
	var raw string
	req, err := kolobraid.ParseJoinRequest(ev.Data)
	if err == nil {
		raw, err = parseJoinData(req.AppData)
	}
	b := s.board()
	if err != nil || b == nil || b.braid.Load() == nil {
		s.inv.Deny(ev.Request)
		return
	}
	if st, _ := b.getState(); st != stateActive {
		s.inv.Deny(ev.Request)
		return
	}
	name := peerName(raw, ev.Joiner)
	if !s.invMeta.get(ev.Invite) {
		if err := s.admit(b, ev); err != nil {
			log.Print("admitting ", keyTag(ev.Joiner), ": ", explain(err).Reason)
		}
		return
	}
	s.mu.Lock()
	s.requests[ev.Request] = &pendingRequest{ev: ev, name: name}
	s.mu.Unlock()
	b.kick()
}

// admit lets a joiner in: Admit appends its AddMember entry and returns the welcome, Approve sends it. When either fails
// the joiner is refused, and removed again if its entry was already written, as the braid's guide says.
func (s *BoardService) admit(b *board, ev invite.Event) error {
	br := b.braid.Load()
	ctx, cancel := context.WithTimeout(s.ctx, writeWait)
	defer cancel()
	// GrowSettlers makes the first joiners admins and settlers; without it a joiner is a plain member and nothing settles.
	welcome, err := br.Admit(ctx, ev.Joiner, ev.Data, &kolobraid.AdmitOptions{GrowSettlers: true})
	if err != nil {
		s.inv.Deny(ev.Request)
		refused := errors.Is(err, kolobraid.ErrActiveKey) || errors.Is(err, kolobraid.ErrKnownStrand) || errors.Is(err, kolobraid.ErrInvalidArgument) ||
			errors.Is(err, kolobraid.ErrNotAdmin) || errors.Is(err, kolobraid.ErrRemoved)
		if !refused {
			br.RemoveMember(ctx, ev.Joiner) // a write failed after the AddMember entry
		}
		return err
	}
	if err := s.inv.Approve(ev.Request, welcome); err != nil {
		br.RemoveMember(ctx, ev.Joiner) // the invite ended meanwhile
		return err
	}
	b.kick()
	return nil
}

// ApproveRequest lets in the person behind a request that waited for an admin.
func (s *BoardService) ApproveRequest(request string) error {
	p, err := s.takeRequest(request)
	if err != nil {
		return err
	}
	b := s.board()
	if b == nil || b.braid.Load() == nil {
		s.inv.Deny(p.ev.Request)
		return boardErr("no-board", ErrNoBoard, "")
	}
	err = s.admit(b, p.ev)
	s.kickBoard()
	return explainOrNil(err)
}

// DenyRequest refuses the person behind a request.
func (s *BoardService) DenyRequest(request string) error {
	p, err := s.takeRequest(request)
	if err != nil {
		return err
	}
	err = s.inv.Deny(p.ev.Request)
	s.kickBoard()
	if errors.Is(err, invite.ErrNotFound) {
		return nil
	}
	return explainOrNil(err)
}

func explainOrNil(err error) error {
	if err == nil {
		return nil
	}
	return explain(err)
}

func (s *BoardService) takeRequest(request string) (*pendingRequest, error) {
	n, err := strconv.ParseUint(request, 10, 64)
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.requests[invite.RequestID(n)]
	if err != nil || p == nil {
		return nil, boardErr("request-not-found", ErrNoSuchItem, "")
	}
	delete(s.requests, invite.RequestID(n))
	return p, nil
}

// CreateInvite makes an invite for the board and returns it with its code, which the person gives to whoever should join.
// Only the main window may ask, and only an admin can make one that works: it is the admin's app that lets the joiner in.
func (s *BoardService) CreateInvite(ctx context.Context, opts InviteOptions) (InviteInfo, error) {
	if !fromMainWindow(ctx) {
		return InviteInfo{}, boardErr("main-window-only", ErrMainOnly, "")
	}
	b, err := s.activeBoard()
	if err != nil {
		return InviteInfo{}, err
	}
	if !b.isAdmin() {
		return InviteInfo{}, boardErr("not-admin", kolobraid.ErrNotAdmin, "")
	}
	if opts.TTLSeconds < 1 || opts.TTLSeconds > int64(math.MaxInt64/time.Second) {
		return InviteInfo{}, boardErr("invite-ttl-range", ErrLimit, "")
	}
	create := invite.CreateOptions{TTL: time.Duration(opts.TTLSeconds) * time.Second, MaxUses: max(opts.Uses, 1), Manual: true}
	if opts.Uses < 0 || create.MaxUses > maxInviteUses {
		return InviteInfo{}, boardErr("invite-uses-range", ErrLimit, strconv.Itoa(maxInviteUses))
	}
	infos, err := s.inv.List()
	if err != nil {
		return InviteInfo{}, explain(err)
	}
	if n := countActive(infos); n >= maxActiveInvites {
		return InviteInfo{}, boardErr("too-many-invites", ErrLimit, strconv.Itoa(n))
	}
	code, info, err := s.inv.Create(&create)
	if err != nil {
		return InviteInfo{}, explain(err)
	}
	if err := s.invMeta.set(info.ID, opts.RequireApproval); err != nil {
		s.inv.Revoke(info.ID)
		return InviteInfo{}, boardErr("internal", err, "")
	}
	s.emitInvites() // without the code: the window learns of every change of the list from an event
	out := inviteInfo(info, s.invMeta.get(info.ID))
	out.Code = code
	return out, nil
}

func countActive(infos []invite.Info) (n int) {
	for _, i := range infos {
		if i.State == invite.Active {
			n++
		}
	}
	return n
}

func inviteInfo(i invite.Info, approval bool) InviteInfo {
	out := InviteInfo{ID: i.ID.String(), Created: i.Created.UTC().Format(time.RFC3339), Uses: i.MaxUses, Used: i.Used,
		RequireApproval: approval, State: i.State.String()}
	if !i.Expires.IsZero() {
		out.Expires = i.Expires.UTC().Format(time.RFC3339)
	}
	return out
}

// Invites lists the invites, oldest first, ended ones included. It has no codes: the inviter does not give a code back.
func (s *BoardService) Invites() ([]InviteInfo, error) {
	if s.inv == nil {
		return nil, explain(ErrNotReady)
	}
	infos, err := s.inv.List()
	if err != nil {
		return nil, explain(err)
	}
	out := make([]InviteInfo, len(infos))
	for i, info := range infos {
		out[i] = inviteInfo(info, s.invMeta.get(info.ID))
	}
	return out, nil
}

func (s *BoardService) emitInvites() {
	if list, err := s.Invites(); err == nil {
		s.emit("board:invites", list)
	}
}

// RevokeInvite ends an invite: someone who uses its code from now on is refused, and the requests that wait for an answer
// are refused too. People who joined stay.
func (s *BoardService) RevokeInvite(id string) error {
	b, err := hex.DecodeString(id)
	var inviteID invite.ID
	if err != nil || len(b) != len(inviteID) {
		return boardErr("invite-not-found", ErrNoSuchItem, "")
	}
	copy(inviteID[:], b)
	if err := s.inv.Revoke(inviteID); err != nil {
		if errors.Is(err, invite.ErrNotFound) {
			return boardErr("invite-not-found", ErrNoSuchItem, "")
		}
		return explain(err)
	}
	s.emitInvites()
	return nil
}

// revokeAll ends every invite that is active, when the board goes away.
func (s *BoardService) revokeAll() {
	infos, err := s.inv.List()
	if err != nil {
		return
	}
	for _, i := range infos {
		if i.State == invite.Active {
			s.inv.Revoke(i.ID)
		}
	}
	s.mu.Lock()
	clear(s.requests)
	s.mu.Unlock()
}

// isAdmin reports whether this member is an admin in the board's view.
func (b *board) isAdmin() bool {
	br := b.braid.Load()
	if br == nil {
		return false
	}
	v := br.View()
	defer v.Close()
	for _, m := range v.Members() {
		if string(m.Key) == string(b.selfKey()) {
			return m.Roles&kolobraid.RoleAdmin != 0
		}
	}
	return false
}

// activeBoard is the board when it accepts changes from this person.
func (s *BoardService) activeBoard() (*board, error) {
	b := s.board()
	if b == nil {
		return nil, boardErr("no-board", ErrNoBoard, "")
	}
	switch st, _ := b.getState(); st {
	case stateActive:
		return b, nil
	case stateRemoved:
		return nil, boardErr("you-were-removed", kolobraid.ErrRemoved, "")
	}
	return nil, boardErr("board-not-active", ErrBusy, "")
}

// RemoveMember removes a member from the board; only an admin can. What the person wrote before the removal stays; what
// they write after it is void for everyone. They keep what they had received, and the others stop sending them more. The
// last admin cannot be removed.
func (s *BoardService) RemoveMember(key string) error {
	b, err := s.activeBoard()
	if err != nil {
		return err
	}
	k, err := b.memberKey(key)
	if err != nil {
		return err
	}
	if !b.isAdmin() {
		return boardErr("not-admin", kolobraid.ErrNotAdmin, "")
	}
	if b.isLastAdmin(k) {
		return boardErr("last-admin", ErrLastAdmin, "")
	}
	if b.removesLastSettler(k) {
		return boardErr("last-settler", ErrLastSettler, "")
	}
	ctx, cancel := context.WithTimeout(b.ctx, writeWait)
	defer cancel()
	_, err = b.braid.Load().RemoveMember(ctx, k)
	if err != nil {
		return explain(err)
	}
	b.kick()
	return nil
}

// isLastAdmin reports whether key is the only admin of the view.
func (b *board) isLastAdmin(key []byte) bool {
	v := b.braid.Load().View()
	defer v.Close()
	admins := 0
	isAdmin := false
	for _, m := range v.Members() {
		if m.Roles&kolobraid.RoleAdmin != 0 {
			admins++
			isAdmin = isAdmin || string(m.Key) == string(key)
		}
	}
	return isAdmin && admins == 1
}

// ---- the activity list ----

var activityKinds = map[actKind]string{
	actBoard: "board-created", actTitle: "board-renamed", actName: "name-changed", actJoined: "joined", actRemoved: "removed",
	actColAdd: "column-added", actColRename: "column-renamed", actColMove: "column-moved", actColDel: "column-deleted",
	actCardAdd: "card-added", actCardEdit: "card-edited", actCardMove: "card-moved", actCardDel: "card-deleted",
}

var editFields = []struct {
	bit  uint64
	name string
}{{fieldTitle, "title"}, {fieldDesc, "description"}, {fieldAssignee, "assignee"}, {fieldColor, "color"}}

// Activity returns a page of the activity list, newest first: who did what, from the entries of the order. before is the
// Seq of the oldest item the window has, or 0 for the newest page, and limit the items of the page (at least 1, at most
// maxActivityPage; a larger limit gets a page of that size). It lists changes that took effect: an entry the rules
// rejected is not in it.
func (s *BoardService) Activity(before uint64, limit int) (Activity, error) {
	if limit < 1 {
		return Activity{}, boardErr("activity-limit", ErrLimit, "")
	}
	limit = min(limit, maxActivityPage)
	b := s.board()
	if b == nil || b.braid.Load() == nil {
		return Activity{}, boardErr("no-board", ErrNoBoard, "")
	}
	br := b.braid.Load()
	v, st := br.View(), br.Settled()
	defer v.Close()
	defer st.Close()
	items, more := readActivity(v, before, limit)
	settledTo := uint64(0)
	if raw, err := st.Get([]byte(keySeq)); err == nil && len(raw) == 8 {
		settledTo = binary.BigEndian.Uint64(raw)
	}
	out := Activity{Items: []ActivityItem{}, More: more}
	mv := &model{Names: map[oid]string{}}
	need := map[oid]bool{}
	for _, it := range items {
		ai := ActivityItem{Seq: it.Seq, Kind: activityKinds[it.Kind], Who: it.Writer.String(), Subject: it.Subject.String(),
			Text: truncate(cleanLine(it.Text), maxCardTitleRunes), Column: cleanLine(it.Text2), Settled: it.Seq < settledTo, Fields: []string{}}
		switch it.Kind {
		case actCardEdit:
			for _, f := range editFields {
				if it.Detail&f.bit != 0 {
					ai.Fields = append(ai.Fields, f.name)
				}
			}
		case actColDel:
			ai.Cards = int(it.Detail)
		}
		need[it.Writer] = true
		if it.Kind == actJoined || it.Kind == actRemoved {
			need[it.Subject] = true
		}
		out.Items = append(out.Items, ai)
	}
	for id := range need {
		if n, err := v.Get(keyOf(prefixName, id[:])); err == nil {
			mv.Names[id] = string(n)
		}
	}
	snapMembers := map[oid]kolobraid.Member{}
	for _, m := range v.Members() {
		snapMembers[oid(m.Key)] = m
	}
	keys := make([]oid, 0, len(need))
	for id := range need {
		keys = append(keys, id)
	}
	slices.SortFunc(keys, func(a, c oid) int { return slices.Compare(a[:], c[:]) })
	for _, id := range keys {
		p := Person{Key: id.String(), Name: peerName(mv.Names[id], id[:]), Tag: keyTag(id[:]), You: string(id[:]) == string(b.selfKey())}
		if m, ok := snapMembers[id]; ok {
			p.Member, p.Admin, p.Seat = true, m.Roles&kolobraid.RoleAdmin != 0, m.Seat
		}
		p.Online = p.You || b.online(id[:])
		out.People = append(out.People, p)
	}
	return out, nil
}
