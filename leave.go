package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"log"
	"time"

	kolobraid "github.com/ciranda-dev/kolo-braid"
)

// Leave plans, as Board.Leave names them. A removal must come from an admin and is void when it leaves no admin or no
// settler key (kolo-braid docs/guide.md, Membership; internal/roster/roster.go, KindRemoveMember), so those cases are refused
// or only delete this computer's copy.
const (
	leaveEveryone    = "everyone"
	leaveAlone       = "alone"
	leaveNotAdmin    = "not-admin"
	leaveLastAdmin   = "last-admin"
	leaveLastSettler = "last-settler"
	leaveLocal       = "local"
	leaveRemoved     = "removed"
	leaveCancel      = "cancel"
)

// notSyncingAfter is how long this member's own changes may wait unsettled, with enough settlers connected, before the
// window says they are not syncing. Members that removed this one stop serving it at once and never tell it (kolo-braid
// docs/guide.md, Membership); the braid gives a key it does not know the same time before it closes the connection.
const notSyncingAfter = joinSyncTimeout

// leavePlan is what leaving does for self, from the members of the view and the settler keys a removal is checked against.
func leavePlan(self ed25519.PublicKey, members []kolobraid.Member, settlers []ed25519.PublicKey) string {
	var me *kolobraid.Member
	admins := 0
	for i, m := range members {
		if m.Roles&kolobraid.RoleAdmin != 0 {
			admins++
		}
		if bytes.Equal(m.Key, self) {
			me = &members[i]
		}
	}
	switch {
	case me == nil:
		return leaveRemoved
	case len(members) == 1:
		return leaveAlone
	case me.Roles&kolobraid.RoleAdmin == 0:
		return leaveNotAdmin
	case admins == 1:
		return leaveLastAdmin
	case isLastSettler(self, settlers):
		return leaveLastSettler
	}
	return leaveEveryone
}

// isLastSettler reports whether removing key would leave no settler key.
func isLastSettler(key ed25519.PublicKey, settlers []ed25519.PublicKey) bool {
	return len(settlers) == 0 || len(settlers) == 1 && settlers[0].Equal(key)
}

// removesLastSettler reports whether removing key would leave the board without a settler key.
func (b *board) removesLastSettler(key ed25519.PublicKey) bool {
	br := b.braid.Load()
	v := br.View()
	defer v.Close()
	return isLastSettler(key, settlerKeys(v.Members(), br.Status().PendingConfig))
}

// settlerKeys are the settler keys a removal is checked against: those of the pending config when there is one, else the
// seats of the view.
func settlerKeys(members, pending []kolobraid.Member) []ed25519.PublicKey {
	var out []ed25519.PublicKey
	if pending != nil {
		for _, m := range pending {
			out = append(out, m.Key)
		}
		return out
	}
	for _, m := range members {
		if m.Seat {
			out = append(out, m.Key)
		}
	}
	return out
}

// leavePlan is what LeaveBoard does now.
func (b *board) leavePlan() string {
	st, _ := b.getState()
	b.mu.Lock()
	tried := b.leaveTried
	b.mu.Unlock()
	br := b.braid.Load()
	switch {
	case st == stateJoining || st == stateCreating:
		return leaveCancel
	case tried || br == nil || st == stateEnded:
		return leaveLocal
	case st == stateRemoved:
		return leaveRemoved
	}
	v := br.View()
	defer v.Close()
	members := v.Members()
	return leavePlan(b.selfKey(), members, settlerKeys(members, br.Status().PendingConfig))
}

// removeSelf writes this member's removal and reports whether it left this computer. The braid does not say what other
// members hold, so the removal counts as sent once it is settled here (a settler sees that) or after another member was
// connected for writeWait after it, many announce gaps (eventGap).
func (b *board) removeSelf() bool {
	br := b.braid.Load()
	ctx, cancel := context.WithTimeout(b.ctx, writeWait)
	defer cancel()
	from := br.Status().Settled
	ref, err := br.RemoveMember(ctx, b.selfKey())
	if err != nil {
		log.Print("board: writing the own removal: ", explain(err).Reason)
		return false
	}
	deadline := time.After(writeWait)
	tick := time.NewTicker(eventGap)
	defer tick.Stop()
	for {
		if settledHere(br, ref, from) {
			return true
		}
		if !b.otherOnline() {
			return false
		}
		select {
		case <-tick.C:
		case <-deadline:
			return true
		case <-b.ctx.Done():
			return false
		}
	}
}

// settledHere reports whether the entry is settled in this computer's order.
func settledHere(br *kolobraid.Braid, ref kolobraid.EntryRef, from uint64) bool {
	for o, err := range br.Entries(from) {
		if err != nil {
			return false
		}
		if o.ID == ref.ID {
			return o.Settled
		}
	}
	return false
}

// otherOnline reports whether this computer is connected to another member of the view.
func (b *board) otherOnline() bool {
	v := b.braid.Load().View()
	defer v.Close()
	for _, m := range v.Members() {
		if !bytes.Equal(m.Key, b.selfKey()) && b.online(m.Key) {
			return true
		}
	}
	return false
}

// checkListed marks the board removed once the view, which listed this member, no longer does: a removal this computer
// received or wrote, before it settles here.
func (b *board) checkListed() {
	if st, _ := b.getState(); st != stateActive {
		return
	}
	if b.isMember() {
		return
	}
	b.mu.Lock()
	was := b.listed
	b.mu.Unlock()
	if was {
		b.markRemoved()
	}
}

// stall adds up how long this member's own changes have waited at one settled position while enough settlers were
// connected.
type stall struct {
	at     uint64
	waited time.Duration
}

// step records d more of waiting and reports whether it reached notSyncingAfter. It starts over when the settled position
// moves or nothing of this member waits, and counts no time while the settlers are away.
func (s *stall) step(settled uint64, own, settling bool, d time.Duration) bool {
	switch {
	case settled != s.at || !own:
		s.at, s.waited = settled, 0
	case settling:
		s.waited += d
	}
	return s.waited >= notSyncingAfter
}

// stallLoop sets the board's stuck flag every writeWait.
func (b *board) stallLoop() {
	var s stall
	tick := time.NewTicker(writeWait)
	defer tick.Stop()
	for {
		select {
		case <-b.ctx.Done():
			return
		case <-tick.C:
		}
		br := b.braid.Load()
		st := br.Status()
		stuck := s.step(st.Settled, b.ownPending(br, st.Settled), b.settling(br, st), writeWait)
		b.mu.Lock()
		changed := b.stuck != stuck
		b.stuck = stuck
		b.mu.Unlock()
		if changed {
			b.kick()
		}
	}
}

// ownPending reports whether a data entry of this member after position settled is neither settled nor void.
func (b *board) ownPending(br *kolobraid.Braid, settled uint64) bool {
	for o, err := range br.Entries(settled) {
		if err != nil {
			return false
		}
		if o.Kind == kolobraid.KindData && !o.Settled && !o.Void && bytes.Equal(o.Writer, b.selfKey()) {
			return true
		}
	}
	return false
}

// settling reports whether enough settlers are connected for changes to settle, as SyncInfo.Settling says.
func (b *board) settling(br *kolobraid.Braid, st kolobraid.Status) bool {
	v := br.View()
	defer v.Close()
	n := 0
	for _, m := range v.Members() {
		if m.Seat && (bytes.Equal(m.Key, b.selfKey()) || b.online(m.Key)) {
			n++
		}
	}
	return st.SettlementHalted == nil && n >= st.Quorum
}
