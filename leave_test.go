package main

import (
	"crypto/ed25519"
	"testing"
	"time"

	kolobraid "github.com/ciranda-dev/kolo-braid"
)

// eventPerson is a person in the last "board:state" event, nil when it has none of that name.
func eventPerson(p *peer, name string) *Person {
	st, ok := p.rec.lastState()
	if !ok {
		return nil
	}
	return st.person(name)
}

// Leaving removes the member from the board on every computer, then deletes this computer's copy; the others' windows get
// the change as an event. The only settler cannot leave or be removed: the braid would void the removal.
func TestLeaveRemovesYouForEveryone(t *testing.T) {
	ps := network(t, "Ana", "Bia")
	a, b := ps[0], ps[1]
	if _, err := a.CreateBoard("Launch", nil); err != nil {
		t.Fatal(err)
	}
	invited(t, a, b, InviteOptions{TTLSeconds: 3600})
	b.waitBoard(t, "had both online", func(x Board) bool { return x.online("Ana") && x.person("Bia").Admin })
	a.waitBoard(t, "had both online", func(x Board) bool { return x.online("Bia") })
	if x := a.board(t); x.Leave != leaveLastSettler {
		t.Errorf("Ana, the only settler: leave %q", x.Leave)
	}
	if err := a.LeaveBoard(); explain(err).Reason != "last-settler" || a.Board() == nil {
		t.Fatalf("the only settler left: %v", err)
	}
	if err := b.RemoveMember(a.Info().KeyID); explain(err).Reason != "last-settler" {
		t.Errorf("an admin removed the only settler: %v", err)
	}
	if x := b.board(t); x.Leave != leaveEveryone {
		t.Fatalf("Bia: leave %q", x.Leave)
	}
	if err := b.LeaveBoard(); err != nil || b.Board() != nil {
		t.Fatalf("leave: %v", err)
	}
	until(t, "Ana's window lost Bia", func() bool { p := eventPerson(a, "Bia"); return p == nil || !p.Member })
	if x := a.board(t); len(x.People) != 1 || x.Leave != leaveAlone {
		t.Errorf("Ana after Bia left: %+v leave %q", x.People, x.Leave)
	}
}

// A member that is not an admin cannot remove itself (kolo-braid RemoveMember needs an admin): leaving deletes only this
// computer's copy, and the window said so before. The last admin of a board with other members cannot leave.
func TestLeaveAsMemberAndLastAdmin(t *testing.T) {
	ps := network(t, "Ana", "Bia", "Caio", "Davi")
	a, d := ps[0], ps[3]
	if _, err := a.CreateBoard("Launch", nil); err != nil {
		t.Fatal(err)
	}
	for _, p := range ps[1:] {
		invited(t, a, p, InviteOptions{TTLSeconds: 3600, Uses: 3})
	}
	awaitSeats(t, ps, 4, 4)
	if x := d.board(t); x.person("Davi").Admin || x.Leave != leaveNotAdmin {
		t.Fatalf("Davi: admin %v leave %q", x.person("Davi").Admin, x.Leave)
	}
	if err := d.LeaveBoard(); err != nil || d.Board() != nil {
		t.Fatalf("a member leaves: %v", err)
	}
	time.Sleep(time.Second)
	if p := a.board(t).person("Davi"); p == nil || !p.Member {
		t.Errorf("a member that left without an admin is gone: %+v", p)
	}
	for _, p := range ps[1:3] {
		if err := a.RemoveMember(p.Info().KeyID); err != nil {
			t.Fatal(err)
		}
	}
	x := a.waitBoard(t, "was the last admin", func(x Board) bool { return x.Leave == leaveLastAdmin })
	if err := a.LeaveBoard(); explain(err).Reason != "last-admin" || a.Board() == nil {
		t.Fatalf("the last admin left: %v (%+v)", err, x.People)
	}
}

// With nobody online the removal cannot reach anyone: the copy stays, the window says so, and the removal goes out when
// another member is back. Leaving again deletes the copy.
func TestLeaveOffline(t *testing.T) {
	ps := network(t, "Ana", "Bia", "Caio")
	a, b, c := ps[0], ps[1], ps[2]
	if _, err := a.CreateBoard("Launch", nil); err != nil {
		t.Fatal(err)
	}
	invited(t, a, b, InviteOptions{TTLSeconds: 3600, Uses: 2})
	invited(t, a, c, InviteOptions{TTLSeconds: 3600, Uses: 2})
	awaitSeats(t, ps, 3, 3)
	upA, upC := a.restartDown(t), c.restartDown(t)
	b.waitBoard(t, "was alone", func(x Board) bool { return !x.online("Ana") && !x.online("Caio") })
	if x := b.board(t); x.Leave != leaveEveryone {
		t.Fatalf("leave %q", x.Leave)
	}
	if err := b.LeaveBoard(); explain(err).Reason != "leave-not-sent" || b.Board() == nil {
		t.Fatalf("an offline leave: %v", err)
	}
	if x := b.board(t); x.Leave != leaveLocal {
		t.Errorf("after the offline leave: %q", x.Leave)
	}
	a, _ = upA(), upC()
	a.waitBoard(t, "got Bia's removal", func(x Board) bool { p := x.person("Bia"); return p == nil || !p.Member })
	if err := b.LeaveBoard(); err != nil || b.Board() != nil {
		t.Errorf("leaving again: %v", err)
	}
}

// A member whose own removal is in its view (it was told, or wrote it) shows the board read only at once.
func TestOwnRemovalInTheViewEndsEditing(t *testing.T) {
	ps := network(t, "Ana", "Bia")
	a, b := ps[0], ps[1]
	if _, err := a.CreateBoard("Launch", nil); err != nil {
		t.Fatal(err)
	}
	invited(t, a, b, InviteOptions{TTLSeconds: 3600})
	b.waitBoard(t, "could edit", func(x Board) bool { return x.CanEdit && x.online("Ana") })
	br := b.BoardService.board().braid.Load()
	if _, err := br.RemoveMember(testContext(t), b.id.PublicKey()); err != nil {
		t.Fatal(err)
	}
	until(t, "Bia's window said removed", func() bool { st, ok := b.rec.lastState(); return ok && st.State == stateRemoved })
	if x := b.board(t); x.CanEdit || x.Blocked == nil || x.Blocked.Reason != "you-were-removed" || x.Leave != leaveRemoved {
		t.Errorf("%v %+v %q", x.CanEdit, x.Blocked, x.Leave)
	}
}

// A member removed by an admin is not told: the others stop serving it at once. Its own changes then never settle while
// the settlers are connected, and after notSyncingAfter the window says they are not syncing.
func TestRemovedMemberSeesItsChangesNotSyncing(t *testing.T) {
	ps := network(t, "Ana", "Bia")
	a, b := ps[0], ps[1]
	if _, err := a.CreateBoard("Launch", nil); err != nil {
		t.Fatal(err)
	}
	invited(t, a, b, InviteOptions{TTLSeconds: 3600})
	todo := b.waitBoard(t, "had both online", func(x Board) bool { return x.online("Ana") }).column("To do").ID
	if err := a.RemoveMember(b.Info().KeyID); err != nil {
		t.Fatal(err)
	}
	a.waitBoard(t, "removed Bia", func(x Board) bool { p := x.person("Bia"); return p == nil || !p.Member })
	if _, err := b.AddCard(todo, CardInput{Title: "Dads"}, 0); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	deadline := start.Add(notSyncingAfter + 3*writeWait)
	for {
		if st, ok := b.rec.lastState(); ok && st.Sync.Stuck {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("never said not syncing: %+v", b.board(t).Sync)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if time.Since(start) < notSyncingAfter-writeWait {
		t.Errorf("said not syncing after %s", time.Since(start))
	}
	if k, _ := a.board(t).card("Dads"); k != nil {
		t.Error("a removed member's card reached Ana")
	}
	if k, _ := b.board(t).card("Dads"); k == nil || !k.Syncing {
		t.Errorf("Bia's card: %+v", k)
	}
}

func TestLeavePlan(t *testing.T) {
	key := func(b byte) ed25519.PublicKey {
		k := make(ed25519.PublicKey, ed25519.PublicKeySize)
		k[0] = b
		return k
	}
	me, other, third := key(1), key(2), key(3)
	admin := kolobraid.RoleAdmin
	m := func(k ed25519.PublicKey, roles kolobraid.Role, seat bool) kolobraid.Member {
		return kolobraid.Member{Key: k, Roles: roles, Seat: seat}
	}
	for _, c := range []struct {
		name     string
		members  []kolobraid.Member
		settlers []ed25519.PublicKey
		want     string
	}{
		{"not listed", []kolobraid.Member{m(other, admin, true)}, []ed25519.PublicKey{other}, leaveRemoved},
		{"alone", []kolobraid.Member{m(me, admin, true)}, []ed25519.PublicKey{me}, leaveAlone},
		{"not an admin", []kolobraid.Member{m(me, 0, true), m(other, admin, true)}, []ed25519.PublicKey{other}, leaveNotAdmin},
		{"last admin", []kolobraid.Member{m(me, admin, true), m(other, 0, true)}, []ed25519.PublicKey{me, other}, leaveLastAdmin},
		{"last settler", []kolobraid.Member{m(me, admin, true), m(other, admin, false)}, []ed25519.PublicKey{me}, leaveLastSettler},
		{"no settler", []kolobraid.Member{m(me, admin, false), m(other, admin, false)}, nil, leaveLastSettler},
		{"a settler of two", []kolobraid.Member{m(me, admin, true), m(other, admin, true)}, []ed25519.PublicKey{me, other}, leaveEveryone},
		{"not a settler", []kolobraid.Member{m(me, admin, false), m(other, admin, true)}, []ed25519.PublicKey{other}, leaveEveryone},
		{"three", []kolobraid.Member{m(me, admin, true), m(other, admin, true), m(third, 0, true)}, []ed25519.PublicKey{me, other, third}, leaveEveryone},
	} {
		if got := leavePlan(me, c.members, c.settlers); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
	// the settler keys of a pending config are the ones a removal is checked against
	seats := []kolobraid.Member{m(me, admin, true), m(other, admin, false)}
	if got := settlerKeys(seats, nil); len(got) != 1 || !got[0].Equal(me) {
		t.Errorf("seats: %x", got)
	}
	if got := settlerKeys(seats, []kolobraid.Member{m(me, admin, false), m(other, admin, false)}); len(got) != 2 {
		t.Errorf("pending: %x", got)
	}
}

func TestStall(t *testing.T) {
	var s stall
	steps := int(notSyncingAfter/writeWait) + 1
	for i := range steps {
		if s.step(5, true, true, writeWait) != (i == steps-1) {
			t.Fatalf("step %d: %+v", i, s)
		}
	}
	if s.step(6, true, true, writeWait) {
		t.Error("settling moved on, still stuck")
	}
	for range steps * 2 {
		if s.step(6, true, false, writeWait) {
			t.Fatal("stuck while the settlers were away")
		}
	}
	for range steps - 1 {
		s.step(6, true, true, writeWait)
	}
	if !s.step(6, true, true, writeWait) {
		t.Error("time with the settlers online is added up")
	}
	if s.step(6, false, true, writeWait) {
		t.Error("stuck with nothing of ours waiting")
	}
}
