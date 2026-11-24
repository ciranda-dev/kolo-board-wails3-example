package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// threeOnABoard makes a board at Ana's with Bia and Caio in it, all connected and settling.
func threeOnABoard(t *testing.T) (a, b, c *peer) {
	t.Helper()
	ps := network(t, "Ana", "Bia", "Caio")
	a, b, c = ps[0], ps[1], ps[2]
	if _, err := a.CreateBoard("Launch", nil); err != nil {
		t.Fatal(err)
	}
	invited(t, a, b, InviteOptions{TTLSeconds: 3600, Uses: 2})
	invited(t, a, c, InviteOptions{TTLSeconds: 3600, Uses: 2})
	awaitSeats(t, ps, 3, 3)
	return a, b, c
}

// The board admits with GrowSettlers: the first joiners become admins and settlers until there are three settlers, and a
// plain member of kolo-braid's default would leave the board with one settler however many people join.
func TestAdmittedMembersGrowTheSettlers(t *testing.T) {
	a, b, c := threeOnABoard(t)
	for _, p := range []*peer{a, b, c} {
		x := p.board(t)
		for name, want := range map[string][2]bool{"Ana": {true, true}, "Bia": {true, true}, "Caio": {true, true}} {
			if q := x.person(name); q == nil || q.Admin != want[0] || q.Seat != want[1] {
				t.Errorf("%s sees %s as %+v, want admin %v seat %v", p.displayName(), name, q, want[0], want[1])
			}
		}
	}
}

func TestRemoveMember(t *testing.T) {
	a, b, c := threeOnABoard(t)
	todo := a.board(t).column("To do").ID
	if _, err := c.AddCard(todo, CardInput{Title: "from Caio"}, 0); err != nil {
		t.Fatal(err)
	}
	for _, p := range []*peer{a, b} {
		p.waitBoard(t, "had Caio's card", func(x Board) bool { k, _ := x.card("from Caio"); return k != nil && !k.Syncing })
	}
	if err := a.RemoveMember("zz"); explain(err).Reason != "member-not-found" {
		t.Errorf("a key that is not a key: %v", err)
	}
	if err := a.RemoveMember(strings.Repeat("ab", 32)); explain(err).Reason != "member-not-found" {
		t.Errorf("a stranger: %v", err)
	}
	if err := a.RemoveMember(c.Info().KeyID); err != nil {
		t.Fatal(err)
	}
	// what Caio wrote stays, and Caio is a former member that the card still names
	for _, p := range []*peer{a, b} {
		x := p.waitBoard(t, "removed Caio", func(x Board) bool { q := x.person("Caio"); return q != nil && !q.Member })
		if k, _ := x.card("from Caio"); k == nil || k.CreatedBy != x.person("Caio").Key {
			t.Errorf("%s: Caio's card is gone or has no author: %+v", p.displayName(), x.titles())
		}
	}
	// Caio may or may not be told: the others stop serving him from their next request on. When he is told, his board says
	// so and refuses changes; when he is not, what he writes is void for everyone and nothing new reaches him.
	told := false
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && !told {
		told = c.board(t).State == stateRemoved
		time.Sleep(50 * time.Millisecond)
	}
	cb := c.board(t)
	if k, _ := cb.card("from Caio"); k == nil {
		t.Error("Caio lost what he had received")
	}
	if told {
		if cb.CanEdit || cb.Blocked == nil || cb.Blocked.Reason != "you-were-removed" {
			t.Fatalf("removed: %+v %+v", cb.CanEdit, cb.Blocked)
		}
		if err := c.EditCard(cb.Columns[0].Cards[0].ID, CardEdit{Title: ptr("x")}); explain(err).Reason != "you-were-removed" {
			t.Errorf("a removed member edits: %v", err)
		}
		if _, err := c.CreateInvite(mainCtx, InviteOptions{TTLSeconds: 3600}); explain(err).Reason != "you-were-removed" {
			t.Errorf("a removed member invites: %v", err)
		}
	} else if _, err := c.AddCard(todo, CardInput{Title: "from the removed"}, 0); err != nil {
		t.Logf("a member that was not told cannot write either: %v", explain(err).Reason)
	}
	// what the others write after the removal never reaches Caio, and what he writes never reaches them
	// a removed settler is served until a settler config without it is active (kolo-braid docs/guide.md, Membership)
	a.waitBoard(t, "had the seats without Caio", func(x Board) bool { return x.Sync.Seats == 2 })
	if _, err := a.AddCard(todo, CardInput{Title: "after the removal"}, 0); err != nil {
		t.Fatal(err)
	}
	b.waitBoard(t, "had the card", func(x Board) bool { k, _ := x.card("after the removal"); return k != nil })
	time.Sleep(3 * time.Second)
	if k, _ := c.board(t).card("after the removal"); k != nil {
		t.Error("a removed member got a change made after the removal")
	}
	for _, p := range []*peer{a, b} {
		if k, _ := p.board(t).card("from the removed"); k != nil {
			t.Errorf("%s shows a card a removed member wrote after the removal", p.displayName())
		}
	}
	if told {
		// it stays removed across a restart
		c = c.restart(t, "")
		if x := c.waitBoard(t, "reopened as removed", func(x Board) bool { return x.State == stateRemoved }); x.CanEdit {
			t.Errorf("after a restart: %s", x.State)
		}
	}
	// leaving is what a removed member does next
	if err := c.LeaveBoard(); err != nil || c.Board() != nil {
		t.Errorf("leave: %v", err)
	}
}

func TestOnlyAdminsRemoveAndInvite(t *testing.T) {
	a := startPeer(t, "Ana", Config{}, nil)
	if _, err := a.CreateBoard("Solo", nil); err != nil {
		t.Fatal(err)
	}
	if err := a.RemoveMember(a.Info().KeyID); explain(err).Reason != "last-admin" {
		t.Errorf("the last admin: %v", err)
	}
	// four people: the first three are admins, the fourth is a plain member
	ps := network(t, "Ana", "Bia", "Caio", "Davi")
	a = ps[0]
	if _, err := a.CreateBoard("Launch", nil); err != nil {
		t.Fatal(err)
	}
	for _, p := range ps[1:] {
		invited(t, a, p, InviteOptions{TTLSeconds: 3600, Uses: 3})
	}
	d := ps[3]
	x := d.waitBoard(t, "had everyone", func(x Board) bool { return len(x.People) == 4 && x.person("Ana") != nil })
	if me := x.person("Davi"); me.Admin {
		t.Fatalf("the fourth is an admin: %+v", me)
	}
	if err := d.RemoveMember(a.Info().KeyID); explain(err).Reason != "not-admin" {
		t.Errorf("a member removes: %v", err)
	}
	if _, err := d.CreateInvite(mainCtx, InviteOptions{TTLSeconds: 3600}); explain(err).Reason != "not-admin" {
		t.Errorf("a member invites: %v", err)
	}
	if err := d.RenameBoard("Mine"); explain(err).Reason != "not-admin" {
		t.Errorf("a member renames: %v", err)
	}
	// a member does everything else
	todo := x.column("To do").ID
	if _, err := d.AddCard(todo, CardInput{Title: "by Davi"}, 0); err != nil {
		t.Errorf("a member adds a card: %v", err)
	}
	if _, err := d.AddColumn("Review", 9); err != nil {
		t.Errorf("a member adds a column: %v", err)
	}
}

// endedReason is why the last "board:state" event ended, "" if it did not.
func endedReason(p *peer) string {
	st, ok := p.rec.lastState()
	if !ok || st.State != stateEnded || st.Error == nil {
		return ""
	}
	return st.Error.Reason
}

func TestJoinFailures(t *testing.T) {
	ps := network(t, "Ana", "Bia", "Caio")
	a, b, c := ps[0], ps[1], ps[2]
	if _, err := a.CreateBoard("Launch", nil); err != nil {
		t.Fatal(err)
	}
	dirOf := func(p *peer) string { return filepath.Join(p.data, boardDir) }
	expect := func(p *peer, reason string) {
		t.Helper()
		until(t, "ended with "+reason, func() bool { return endedReason(p) == reason })
		if p.Board() != nil {
			t.Errorf("%s: a board after a failed join", reason)
		}
		if _, err := os.Stat(dirOf(p)); err == nil {
			t.Errorf("%s: the join left its store behind", reason)
		}
		if _, err := os.Stat(filepath.Join(p.data, markerFile)); err == nil {
			t.Errorf("%s: the join left its marker behind", reason)
		}
	}

	// refused by an admin
	info, err := a.CreateInvite(mainCtx, InviteOptions{TTLSeconds: 3600, RequireApproval: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.JoinBoard(mainCtx, info.Code); err != nil {
		t.Fatal(err)
	}
	rq := a.waitBoard(t, "had the request", func(x Board) bool { return len(x.Requests) == 1 }).Requests[0]
	if err := a.DenyRequest(rq.Request); err != nil {
		t.Fatal(err)
	}
	expect(b, "join-denied")
	if err := a.DenyRequest(rq.Request); explain(err).Reason != "request-not-found" {
		t.Errorf("denying twice: %v", err)
	}

	// asking again after a denial gets the same answer
	if _, err := b.JoinBoard(mainCtx, info.Code); err != nil {
		t.Fatal(err)
	}
	until(t, "denied again", func() bool {
		st, _ := b.rec.lastState()
		return st.State == stateEnded && endedReason(b) == "join-denied" && b.Board() == nil
	})

	// revoked
	rev, _ := a.CreateInvite(mainCtx, InviteOptions{TTLSeconds: 3600})
	if err := a.RevokeInvite(rev.ID); err != nil {
		t.Fatal(err)
	}
	if err := a.RevokeInvite(strings.Repeat("0", 32)); explain(err).Reason != "invite-not-found" {
		t.Errorf("revoking what is not there: %v", err)
	}
	if _, err := b.JoinBoard(mainCtx, rev.Code); err != nil {
		t.Fatal(err)
	}
	until(t, "revoked", func() bool { return endedReason(b) == "invite-revoked" && b.Board() == nil })

	// expired
	exp, _ := a.CreateInvite(mainCtx, InviteOptions{TTLSeconds: 1})
	time.Sleep(2 * time.Second)
	if _, err := b.JoinBoard(mainCtx, exp.Code); err != nil {
		t.Fatal(err)
	}
	until(t, "expired", func() bool { return endedReason(b) == "invite-expired" && b.Board() == nil })

	// used up: one use, two people
	one, _ := a.CreateInvite(mainCtx, InviteOptions{TTLSeconds: 3600})
	b.join(t, one.Code)
	if _, err := c.JoinBoard(mainCtx, one.Code); err != nil {
		t.Fatal(err)
	}
	until(t, "used up", func() bool { return endedReason(c) == "invite-used-up" && c.Board() == nil })
	invs, err := a.Invites()
	if err != nil {
		t.Fatal(err)
	}
	var used bool
	for _, i := range invs {
		used = used || i.ID == one.ID && i.State == "used up" && i.Used == 1
		if i.Code != "" {
			t.Errorf("the list of invites has a code: %+v", i)
		}
	}
	if !used {
		t.Errorf("invites %+v", invs)
	}

	// cancelled while it waits for an admin: the request leaves the admin's list and nothing stays on disk
	man, _ := a.CreateInvite(mainCtx, InviteOptions{TTLSeconds: 3600, RequireApproval: true})
	if _, err := c.JoinBoard(mainCtx, man.Code); err != nil {
		t.Fatal(err)
	}
	c.waitBoard(t, "waiting", func(x Board) bool { return x.State == stateJoining && x.Phase == phaseWaiting })
	a.waitBoard(t, "had the request", func(x Board) bool { return len(x.Requests) == 1 })
	if _, err := c.JoinBoard(mainCtx, man.Code); explain(err).Reason != "busy-joining" {
		t.Errorf("a join during a join: %v", err)
	}
	if _, err := c.CreateBoard("X", nil); explain(err).Reason != "busy-joining" {
		t.Errorf("a create during a join: %v", err)
	}
	if err := c.LeaveBoard(); err != nil {
		t.Fatal(err)
	}
	if c.Board() != nil {
		t.Error("a board after cancelling")
	}
	a.waitBoard(t, "lost the request", func(x Board) bool { return len(x.Requests) == 0 })
	if _, err := os.Stat(dirOf(c)); err == nil {
		t.Error("cancelling left the store behind")
	}

	// a person who is already a member asks again
	if _, err := b.JoinBoard(mainCtx, man.Code); explain(err).Reason != "board-exists" {
		t.Errorf("a member joins again: %v", err)
	}

	// not from another window, and not text that is not an invite
	if _, err := c.JoinBoard(otherCtx, man.Code); explain(err).Reason != "main-window-only" {
		t.Errorf("join from the notch: %v", err)
	}
	if _, err := a.CreateInvite(otherCtx, InviteOptions{TTLSeconds: 3600}); explain(err).Reason != "main-window-only" {
		t.Errorf("invite from the notch: %v", err)
	}
	if _, err := a.CreateInvite(mainCtx, InviteOptions{TTLSeconds: 0}); explain(err).Reason != "invite-ttl-range" {
		t.Errorf("a long invite: %v", err)
	}
	if _, err := a.CreateInvite(mainCtx, InviteOptions{TTLSeconds: 3600, Uses: maxInviteUses + 1}); explain(err).Reason != "invite-uses-range" {
		t.Errorf("a wide invite: %v", err)
	}
	if _, err := a.CreateInvite(mainCtx, InviteOptions{TTLSeconds: 1 << 62}); explain(err).Reason != "invite-ttl-range" {
		t.Errorf("a lifetime that overflows: %v", err)
	}
	if _, err := a.CreateInvite(mainCtx, InviteOptions{TTLSeconds: 3600, Uses: -1}); explain(err).Reason != "invite-uses-range" {
		t.Errorf("negative uses: %v", err)
	}
	if _, err := a.CreateInvite(mainCtx, InviteOptions{TTLSeconds: -5}); explain(err).Reason != "invite-ttl-range" {
		t.Errorf("a negative invite: %v", err)
	}
}

func TestInviterNotFound(t *testing.T) {
	ps := network(t, "Entry", "Ana", "Bia") // the first one only runs the entry node
	a, b := ps[1], ps[2]
	if _, err := a.CreateBoard("Launch", nil); err != nil {
		t.Fatal(err)
	}
	info, _ := a.CreateInvite(mainCtx, InviteOptions{TTLSeconds: 3600})
	if err := a.ServiceShutdown(); err != nil { // the inviter goes away
		t.Fatal(err)
	}
	b2 := startPeer(t, "Bia2", Config{Bootstrap: b.cfg.Bootstrap, FindTimeout: 2 * time.Second}, nil)
	b2.rec.waitNetwork(t, netReady)
	if _, err := b2.JoinBoard(mainCtx, info.Code); err != nil {
		t.Fatal(err)
	}
	until(t, "not found", func() bool { return endedReason(b2) == "inviter-not-found" && b2.Board() == nil })

	// without -find-timeout a join looks until the person cancels it
	b3 := startPeerWith(t, "Bia3", Config{Bootstrap: b.cfg.Bootstrap}, func(s *BoardService) { s.cfg.FindTimeout = 0 })
	b3.rec.waitNetwork(t, netReady)
	if _, err := b3.JoinBoard(mainCtx, info.Code); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * b3.cfg.LookupInterval) // past several lookups and past the 2 s that ended Bia2's join
	if x := b3.Board(); x == nil || x.State != stateJoining || x.Phase != phaseFinding {
		t.Fatalf("a join without a deadline stopped looking: %+v", x)
	}
	if err := b3.LeaveBoard(); err != nil {
		t.Fatal(err)
	}
	until(t, "cancelled", func() bool { return b3.Board() == nil })
}

func TestQuorumDecidesWhatIsSettled(t *testing.T) {
	a, b, c := threeOnABoard(t) // three settlers: two must be online
	todo := a.board(t).column("To do").ID
	for _, p := range []*peer{a, b, c} {
		if x := p.board(t); x.Sync.Seats != 3 || x.Sync.Quorum != 2 || x.Sync.SingleSettler {
			t.Fatalf("%s: %+v", p.displayName(), x.Sync)
		}
	}
	// with one settler away the other two settle
	upC := c.restartDown(t)
	if _, err := a.AddCard(todo, CardInput{Title: "one away"}, 0); err != nil {
		t.Fatal(err)
	}
	a.waitBoard(t, "settled with two online", func(x Board) bool {
		k, _ := x.card("one away")
		return k != nil && !k.Syncing && x.Sync.SeatsOnline == 2 && x.Sync.Settling
	})
	// with two away, Ana's card is shown and not settled
	upB := b.restartDown(t)
	if _, err := a.AddCard(todo, CardInput{Title: "two away"}, 0); err != nil {
		t.Fatal(err)
	}
	x := a.waitBoard(t, "showed the card as syncing", func(x Board) bool {
		k, _ := x.card("two away")
		return k != nil && k.Syncing && x.Sync.SeatsOnline == 1
	})
	if x.Sync.Settling || x.Sync.Quorum != 2 || x.Sync.CardsSyncing != 1 || x.Sync.Pending != 1 || x.Sync.CardsSettled != 1 {
		t.Errorf("sync %+v", x.Sync)
	}
	if n := a.Notch(); n.CardsSyncing != 1 || n.Settling {
		t.Errorf("the notch does not say it: %+v", n)
	}
	time.Sleep(2 * time.Second)
	if k, _ := a.board(t).card("two away"); k == nil || !k.Syncing {
		t.Error("the card settled with two settlers away")
	}
	// one comes back: the card settles, and the returning member has it
	b = upB()
	for _, p := range []*peer{a, b} {
		x := p.waitBoard(t, p.displayName()+" saw it settled", func(x Board) bool {
			k, _ := x.card("two away")
			return k != nil && !k.Syncing && x.Sync.Settling && x.Sync.Pending == 0
		})
		if x.Sync.CardsSettled != 2 || x.Sync.CardsSyncing != 0 {
			t.Errorf("%s: %+v", p.displayName(), x.Sync)
		}
	}
	c = upC()
	c.waitBoard(t, "Caio caught up", func(x Board) bool { k, _ := x.card("two away"); return k != nil && !k.Syncing })
}

// With two members the second is not a settler: one computer settles the board, and the window says so.
func TestTwoMembersHaveOneSettler(t *testing.T) {
	ps := network(t, "Ana", "Bia")
	a, b := ps[0], ps[1]
	if _, err := a.CreateBoard("Launch", nil); err != nil {
		t.Fatal(err)
	}
	invited(t, a, b, InviteOptions{TTLSeconds: 3600})
	todo := a.board(t).column("To do").ID
	for _, p := range ps {
		x := p.waitBoard(t, "had both online", func(x Board) bool { return len(x.People) == 2 && x.online("Ana") && x.online("Bia") })
		if !x.Sync.SingleSettler || x.Sync.Seats != 1 || !x.person("Ana").Seat || x.person("Bia").Seat || !x.Sync.Settling {
			t.Errorf("%s: %+v %+v", p.displayName(), x.Sync, x.People)
		}
	}
	// Bia's change shows as syncing while the only settler is away, and settles when it is back
	upA := a.restartDown(t)
	b.waitBoard(t, "saw Ana go", func(x Board) bool { return x.person("Ana") != nil && !x.online("Ana") })
	if _, err := b.AddCard(todo, CardInput{Title: "while Ana is away"}, 0); err != nil {
		t.Fatal(err)
	}
	x := b.waitBoard(t, "showed it as syncing", func(x Board) bool { k, _ := x.card("while Ana is away"); return k != nil && k.Syncing })
	if x.Sync.Settling || x.Sync.SeatsOnline != 0 || !x.Sync.SingleSettler {
		t.Errorf("sync %+v", x.Sync)
	}
	a = upA()
	for _, p := range []*peer{a, b} {
		p.waitBoard(t, p.displayName()+" saw it settled", func(x Board) bool {
			k, _ := x.card("while Ana is away")
			return k != nil && !k.Syncing && x.Sync.Settling
		})
	}
}

// restartDown stops the person and returns the function that starts them again on the same data.
func (p *peer) restartDown(t *testing.T) func() *peer {
	t.Helper()
	if err := p.ServiceShutdown(); err != nil {
		t.Fatal(err)
	}
	return func() *peer {
		cfg := p.cfg
		cfg.DataDir = p.data
		q := startPeer(t, "", cfg, p.id)
		q.rec.waitNetwork(t, netReady)
		return q
	}
}

func TestMemberKeepsTheBoardAcrossARestart(t *testing.T) {
	ps := network(t, "Ana", "Bia")
	a, b := ps[0], ps[1]
	if _, err := a.CreateBoard("Launch", nil); err != nil {
		t.Fatal(err)
	}
	invited(t, a, b, InviteOptions{TTLSeconds: 3600})
	todo := a.board(t).column("To do").ID
	b.waitBoard(t, "all online", func(x Board) bool { return len(x.People) == 2 && x.online("Ana") })
	id := b.board(t).ID
	b = b.restart(t, "")
	b.rec.waitNetwork(t, netReady)
	x := b.waitBoard(t, "reopened as a member", func(x Board) bool { return x.State == stateActive && x.person("Ana") != nil })
	if x.ID != id || x.Title != "Launch" || !x.person("Bia").You {
		t.Fatalf("%+v", x)
	}
	// once an admin is online, the member writes again
	until(t, "could edit", func() bool { return b.board(t).CanEdit })
	if _, err := b.AddCard(todo, CardInput{Title: "after the restart"}, 0); err != nil {
		t.Fatal(err)
	}
	a.waitBoard(t, "had it", func(x Board) bool { k, _ := x.card("after the restart"); return k != nil && !k.Syncing })
}

func TestBoardWaitsForAnotherMemberAfterARestart(t *testing.T) {
	ps := network(t, "Ana", "Bia")
	a, b := ps[0], ps[1]
	if _, err := a.CreateBoard("Launch", nil); err != nil {
		t.Fatal(err)
	}
	invited(t, a, b, InviteOptions{TTLSeconds: 3600})
	b.waitBoard(t, "all online", func(x Board) bool { return len(x.People) == 2 && x.online("Ana") })
	todo := b.board(t).column("To do").ID
	b.restartDown(t)
	if err := a.ServiceShutdown(); err != nil {
		t.Fatal(err)
	}
	// both are away: Bia opens the board alone, and her strand cannot be confirmed by anyone
	b2 := startPeer(t, "", Config{DataDir: b.data}, b.id)
	x := b2.waitBoard(t, "opened", func(x Board) bool { return x.State == stateActive && x.person("Ana") != nil })
	if x.CanEdit || x.Blocked == nil || x.Blocked.Reason != "strand-unconfirmed" {
		t.Errorf("alone after a restart: canEdit %v blocked %+v", x.CanEdit, x.Blocked)
	}
	if k, _ := x.card("nothing"); k != nil || x.Title != "Launch" {
		t.Errorf("the board is not whole: %q", x.Title)
	}
	start := time.Now()
	_, err := b2.AddCard(todo, CardInput{Title: "too early"}, 0)
	if explain(err).Reason != "strand-unconfirmed" || time.Since(start) < writeWait-time.Second {
		t.Errorf("a change that cannot be confirmed: %v after %s", err, time.Since(start))
	}
}

// The active invites have no bound: more than the 32 topics kolo-swarm used to hold are all created and listed.
func TestManyActiveInvites(t *testing.T) {
	p := startPeer(t, "Ana", Config{}, nil)
	if _, err := p.CreateBoard("Launch", nil); err != nil {
		t.Fatal(err)
	}
	const many = 40
	for range many {
		if _, err := p.CreateInvite(mainCtx, InviteOptions{TTLSeconds: 3600}); err != nil {
			t.Fatal(err)
		}
	}
	list, _ := p.Invites()
	if len(list) != many {
		t.Errorf("%d invites, want %d", len(list), many)
	}
}

// Leaving a board ends the invites made for it: a code given out earlier does not let anyone into the next board.
func TestLeavingEndsTheInvites(t *testing.T) {
	ps := network(t, "Ana", "Bia")
	a, b := ps[0], ps[1]
	if _, err := a.CreateBoard("Old", nil); err != nil {
		t.Fatal(err)
	}
	old, _ := a.CreateInvite(mainCtx, InviteOptions{TTLSeconds: 3600, Uses: 5})
	if err := a.LeaveBoard(); err != nil {
		t.Fatal(err)
	}
	list, _ := a.Invites()
	if len(list) != 1 || list[0].State != "revoked" {
		t.Errorf("invites after leaving: %+v", list)
	}
	if _, err := a.CreateBoard("New", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := b.JoinBoard(mainCtx, old.Code); err != nil {
		t.Fatal(err)
	}
	until(t, "refused", func() bool { r := endedReason(b); return r == "invite-revoked" || r == "invite-unknown" })
	if got := a.board(t); len(got.People) != 1 {
		t.Errorf("someone got into the new board with an old code: %+v", got.People)
	}
}

// Five members on one address (here 127.0.0.1) all connect to each other: kolo-transport's default of 4 connections per
// address would refuse the fifth.
func TestFiveMembersOnOneAddress(t *testing.T) {
	names := []string{"Ana", "Bia", "Caio", "Davi", "Eva"}
	ps := network(t, names...)
	a := ps[0]
	if _, err := a.CreateBoard("Launch", nil); err != nil {
		t.Fatal(err)
	}
	for _, p := range ps[1:] {
		invited(t, a, p, InviteOptions{TTLSeconds: 3600, Uses: 4})
	}
	awaitSeats(t, ps, 4, 4)
	for _, p := range ps {
		x := p.waitBoard(t, "had everyone connected", func(x Board) bool {
			for _, n := range names {
				if !x.online(n) {
					return false
				}
			}
			return len(x.People) == 5
		})
		if x.Sync.Seats != 4 || x.Sync.Quorum != 3 {
			t.Errorf("%s: %+v", p.displayName(), x.Sync)
		}
	}
	todo := a.board(t).column("To do").ID
	if _, err := ps[4].AddCard(todo, CardInput{Title: "from Eva"}, 0); err != nil {
		t.Fatal(err)
	}
	for _, p := range ps {
		p.waitBoard(t, p.displayName()+" saw it settled", func(x Board) bool { k, _ := x.card("from Eva"); return k != nil && !k.Syncing })
	}
}

// A board that this member was removed from is shown, read only: every change and every invite is refused with the reason.
// (Whether a removed member is told at all depends on the other peers, so the state is set here directly.)
func TestRemovedBoardIsReadOnly(t *testing.T) {
	p := startPeer(t, "Ana", Config{}, nil)
	if _, err := p.CreateBoard("Launch", nil); err != nil {
		t.Fatal(err)
	}
	b := p.board(t)
	card, err := p.AddCard(b.Columns[0].ID, CardInput{Title: "kept"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	p.BoardService.board().markRemoved()
	x := p.waitBoard(t, "was removed", func(x Board) bool { return x.State == stateRemoved })
	if x.CanEdit || x.Blocked == nil || x.Blocked.Reason != "you-were-removed" || x.Title != "Launch" {
		t.Fatalf("%+v %+v", x.CanEdit, x.Blocked)
	}
	if k, _ := x.card("kept"); k == nil {
		t.Error("a removed member lost what it had")
	}
	for name, err := range map[string]error{
		"edit":   p.EditCard(card, CardEdit{Title: ptr("x")}),
		"move":   p.MoveCard(card, x.Columns[1].ID, 0),
		"delete": p.DeleteCard(card),
		"rename": p.RenameBoard("x"),
		"remove": p.RemoveMember(x.Self),
		"column": p.RenameColumn(x.Columns[0].ID, "x"),
	} {
		if explain(err).Reason != "you-were-removed" {
			t.Errorf("%s of a removed board: %v", name, err)
		}
	}
	if _, err := p.AddCard(x.Columns[0].ID, CardInput{Title: "x"}, 0); explain(err).Reason != "you-were-removed" {
		t.Errorf("add: %v", err)
	}
	if _, err := p.CreateInvite(mainCtx, InviteOptions{TTLSeconds: 3600}); explain(err).Reason != "you-were-removed" {
		t.Errorf("invite: %v", err)
	}
	if n := p.Notch(); !n.Active || n.State != stateRemoved {
		t.Errorf("notch %+v", n)
	}
	if tr := p.trayNow(); tr.Status != "Removed from the board" {
		t.Errorf("tray %+v", tr)
	}
	if err := p.LeaveBoard(); err != nil || p.Board() != nil {
		t.Errorf("leave: %v", err)
	}
}
