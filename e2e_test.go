package main

import (
	"errors"
	"strings"
	"testing"
	"time"

	kolobraid "github.com/ciranda-dev/kolo-braid"
	invite "github.com/ciranda-dev/kolo-invite"
)

func ptr[T any](v T) *T { return &v }

func TestCreateEditAndRestart(t *testing.T) {
	p := startPeer(t, "Ana", Config{}, nil) // a board needs no one else on the network
	if p.Board() != nil {
		t.Fatal("a board before any was made")
	}
	if _, err := p.CreateBoard("Launch", nil); err != nil {
		t.Fatal(err)
	}
	b := p.board(t)
	if b.State != stateActive || b.Title != "Launch" || b.ID == "" || len(b.Columns) != 3 || b.Columns[0].Title != "To do" || b.Columns[2].Title != "Done" {
		t.Fatalf("%+v", b)
	}
	me := b.person("Ana")
	if me == nil || !me.You || !me.Admin || !me.Seat || !me.Online || !me.Member || me.Key != b.Self {
		t.Fatalf("people %+v", b.People)
	}
	todo, doing := b.column("To do").ID, b.column("Doing").ID

	first, err := p.AddCard(todo, CardInput{Title: "Write docs", Description: "all of them", Color: "orange", Assignee: me.Key}, 0)
	if err != nil {
		t.Fatal(err)
	}
	second, err := p.AddCard(todo, CardInput{Title: "Ship it"}, 99)
	if err != nil {
		t.Fatal(err)
	}
	third, err := p.AddCard(todo, CardInput{Title: "Celebrate"}, 1) // between the two
	if err != nil {
		t.Fatal(err)
	}
	b = p.waitBoard(t, "had three cards", func(b Board) bool { return b.column("To do") != nil && len(b.column("To do").Cards) == 3 })
	if got := b.titles(); got != "To do:Write docs,Celebrate,Ship it | Doing: | Done:" {
		t.Fatalf("order %s", got)
	}
	c, _ := b.card("Write docs")
	if c.Description != "all of them" || c.Color != "orange" || c.Assignee != me.Key || c.CreatedBy != me.Key {
		t.Fatalf("%+v", c)
	}
	if err := p.EditCard(first, CardEdit{Title: ptr("Write the docs"), Color: ptr(""), Assignee: ptr("")}); err != nil {
		t.Fatal(err)
	}
	if err := p.MoveCard(second, doing, 0); err != nil {
		t.Fatal(err)
	}
	if err := p.MoveCard(third, todo, 0); err != nil {
		t.Fatal(err)
	}
	if err := p.RenameColumn(doing, "In progress"); err != nil {
		t.Fatal(err)
	}
	extra, err := p.AddColumn("Review", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.MoveColumn(extra, 99); err != nil {
		t.Fatal(err)
	}
	if err := p.RenameBoard("Launch 2"); err != nil {
		t.Fatal(err)
	}
	want := "To do:Celebrate,Write the docs | In progress:Ship it | Done: | Review:"
	b = p.waitBoard(t, "reached the edits", func(b Board) bool { return b.Title == "Launch 2" && b.titles() == want })
	if c, _ := b.card("Write the docs"); c.Color != "" || c.Assignee != "" || c.Description != "all of them" {
		t.Fatalf("the edit: %+v", c)
	}
	if err := p.DeleteCard(third); err != nil {
		t.Fatal(err)
	}
	if err := p.DeleteColumn(extra); err != nil {
		t.Fatal(err)
	}
	b = p.waitBoard(t, "deleted", func(b Board) bool {
		return b.column("Review") == nil && b.titles() == "To do:Write the docs | In progress:Ship it | Done:"
	})
	// alone, the founder settles everything by itself
	b = p.waitBoard(t, "settled", func(b Board) bool { return b.Sync.CardsSyncing == 0 && b.Sync.Pending == 0 && b.Sync.CardsSettled == 2 })
	if b.Sync.Seats != 1 || b.Sync.Quorum != 1 || !b.Sync.Settling || !b.CanEdit {
		t.Fatalf("sync %+v canEdit %v", b.Sync, b.CanEdit)
	}

	// the board is in the data directory: a new service finds it, and the person is still the same member
	p = p.restart(t, "")
	b2 := p.waitBoard(t, "reopened", func(b Board) bool { return b.State == stateActive && len(b.People) == 1 && b.column("To do") != nil })
	if b2.ID != b.ID || b2.Title != "Launch 2" || b2.titles() != "To do:Write the docs | In progress:Ship it | Done:" {
		t.Fatalf("after the restart: %s %+v", b2.titles(), b2)
	}
	if err := p.EditCard(first, CardEdit{Description: ptr("again")}); err != nil {
		t.Fatalf("a founder alone writes after a restart: %v", err)
	}
	p.waitBoard(t, "edited after the restart", func(b Board) bool { c, _ := b.card("Write the docs"); return c != nil && c.Description == "again" })

	// leaving removes the copy
	if err := p.LeaveBoard(); err != nil {
		t.Fatal(err)
	}
	if p.Board() != nil {
		t.Fatal("the board stays after Leave")
	}
	if err := p.LeaveBoard(); explain(err).Reason != "no-board" {
		t.Errorf("leaving twice: %v", err)
	}
	p = p.restart(t, "")
	if p.Board() != nil {
		t.Fatal("a board came back after Leave")
	}
}

func TestTextIsCheckedAndCleaned(t *testing.T) {
	p := startPeer(t, "Ana", Config{}, nil)
	if _, err := p.CreateBoard("  Launch\u202E  ", []string{"Todo", "Done"}); err != nil {
		t.Fatal(err)
	}
	b := p.board(t)
	if b.Title != "Launch" || len(b.Columns) != 2 {
		t.Fatalf("%+v", b)
	}
	col := b.Columns[0].ID
	for name, in := range map[string]CardInput{
		"empty":       {Title: ""},
		"blank":       {Title: " \u200B\u202E "},
		"long":        {Title: strings.Repeat("x", maxCardTitleRunes+1)},
		"description": {Title: "ok", Description: strings.Repeat("d", maxCardDescRunes+1)},
		"color":       {Title: "ok", Color: "pink"},
		"assignee":    {Title: "ok", Assignee: strings.Repeat("ab", 32)},
	} {
		if _, err := p.AddCard(col, in, 0); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := p.AddCard("nope", CardInput{Title: "x"}, 0); explain(err).Reason != "column-not-found" {
		t.Errorf("unknown column: %v", err)
	}
	if err := p.EditCard("nope", CardEdit{Title: ptr("x")}); explain(err).Reason != "card-not-found" {
		t.Errorf("unknown card: %v", err)
	}
	if err := p.MoveCard("nope", col, 0); explain(err).Reason != "card-not-found" {
		t.Errorf("unknown card: %v", err)
	}
	id, err := p.AddCard(col, CardInput{Title: "A\u202EB\u0000C\nD", Description: "line one\r\n\r\n\r\n\u200Bline two\t x"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	b = p.waitBoard(t, "had the card", func(b Board) bool { c, _ := b.card("ABC D"); return c != nil })
	c, _ := b.card("ABC D")
	if c.ID != id || c.Description != "line one\n\nline two x" {
		t.Errorf("%+v", c)
	}
	if err := p.EditCard(id, CardEdit{}); err != nil {
		t.Errorf("an edit of nothing: %v", err)
	}
	if err := p.AddColumnAt("", 0); err == nil {
		t.Error("a column without a title")
	}
	for _, name := range []string{"", "  ", "\u200B", strings.Repeat("n", maxNameRunes+1)} {
		if _, err := p.SetDisplayName(name); err == nil {
			t.Errorf("name %q accepted", name)
		}
	}
	if s, err := p.SetDisplayName("  Ana\u202E  María "); err != nil || s.DisplayName != "Ana María" {
		t.Errorf("name: %q %v", s.DisplayName, err)
	}
}

// AddColumnAt is AddColumn without its result, for a test that wants the error.
func (s *BoardService) AddColumnAt(title string, index int) error {
	_, err := s.AddColumn(title, index)
	return err
}

func TestOneBoardAtATime(t *testing.T) {
	p := startPeer(t, "Ana", Config{}, nil)
	if _, err := p.CreateBoard("A", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := p.CreateBoard("B", nil); explain(err).Reason != "board-exists" {
		t.Errorf("a second board: %v", err)
	}
	if _, err := p.JoinBoard(mainCtx, "koloinv0abc"); explain(err).Reason != "invalid-invite" {
		t.Errorf("a bad invite: %v", err)
	}
	q := startPeer(t, "", Config{}, nil) // no name yet
	if _, err := q.CreateBoard("A", nil); explain(err).Reason != "name-empty" {
		t.Errorf("without a name: %v", err)
	}
	if _, err := q.CreateBoard("", nil); explain(err).Reason != "board-title-empty" {
		t.Errorf("without a title: %v", err)
	}
	if err := q.EditCard("x", CardEdit{Title: ptr("y")}); explain(err).Reason != "no-board" {
		t.Errorf("an edit without a board: %v", err)
	}
	if _, err := q.CreateInvite(mainCtx, InviteOptions{TTLSeconds: 3600}); explain(err).Reason != "no-board" {
		t.Errorf("an invite without a board: %v", err)
	}
	if _, err := q.Activity(0, 50); explain(err).Reason != "no-board" {
		t.Errorf("the activity without a board: %v", err)
	}
}

func TestInviteJoinAndEditTogether(t *testing.T) {
	ps := network(t, "Ana", "Bia", "Caio")
	a, b, c := ps[0], ps[1], ps[2]
	if _, err := a.CreateBoard("Launch", nil); err != nil {
		t.Fatal(err)
	}
	todo := a.board(t).column("To do").ID
	if _, err := a.AddCard(todo, CardInput{Title: "from Ana"}, 0); err != nil {
		t.Fatal(err)
	}

	// Bia joins with an invite that lets people in at once
	info, err := a.CreateInvite(mainCtx, InviteOptions{TTLSeconds: 3600, Uses: 2})
	if err != nil {
		t.Fatal(err)
	}
	if info.Code == "" || !strings.HasPrefix(info.Code, invite.Prefix) || info.RequireApproval || info.State != "active" || info.Uses != 2 {
		t.Fatalf("%+v", info)
	}
	bb := b.join(t, info.Code)
	if bb.ID != a.board(t).ID || bb.Title != "Launch" {
		t.Fatalf("Bia's board: %+v", bb)
	}
	bb = b.waitBoard(t, "had Ana's card and both names", func(x Board) bool { c, _ := x.card("from Ana"); return c != nil && x.person("Ana") != nil })
	if me := bb.person("Bia"); me == nil || !me.You || !me.Member {
		t.Fatalf("Bia is not herself: %+v", bb.People)
	}
	// Caio joins with a second use of the same invite, but this one needs approval
	info2, err := a.CreateInvite(mainCtx, InviteOptions{TTLSeconds: 3600, RequireApproval: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.JoinBoard(mainCtx, info2.Code); err != nil {
		t.Fatal(err)
	}
	cb := c.waitBoard(t, "waiting for the answer", func(x Board) bool { return x.State == stateJoining && x.Phase == phaseWaiting })
	if cb.ID != "" {
		t.Errorf("a board that is not there yet has an ID: %+v", cb)
	}
	ab := a.waitBoard(t, "had the request", func(x Board) bool { return len(x.Requests) == 1 })
	rq := ab.Requests[0]
	if rq.Name != "Caio" || rq.Invite != info2.ID || rq.Joiner != c.Info().KeyID {
		t.Fatalf("request %+v", rq)
	}
	if err := a.ApproveRequest(rq.Request); err != nil {
		t.Fatal(err)
	}
	if err := a.ApproveRequest(rq.Request); explain(err).Reason != "request-not-found" {
		t.Errorf("approving twice: %v", err)
	}
	c.waitBoard(t, "joined", func(x Board) bool { return x.State == stateActive && x.person("Caio") != nil && x.person("Ana") != nil })

	// everyone edits; all see the same board
	bt, ct := b.board(t).column("To do").ID, c.board(t).column("To do").ID
	if bt != todo || ct != todo {
		t.Fatal("the column IDs differ between members")
	}
	if _, err := b.AddCard(todo, CardInput{Title: "from Bia", Color: "green"}, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddCard(todo, CardInput{Title: "from Caio"}, 1); err != nil {
		t.Fatal(err)
	}
	for _, p := range ps {
		p.waitBoard(t, p.displayName()+" saw three cards", func(x Board) bool { return x.column("To do") != nil && len(x.column("To do").Cards) == 3 })
	}
	// settled: three settlers, two online is enough, here all are
	awaitSeats(t, ps, 3, 3)
	for _, p := range ps {
		x := p.waitBoard(t, p.displayName()+" saw them settled", func(x Board) bool {
			return x.Sync.CardsSyncing == 0 && x.Sync.CardsSettled == 3 && x.Sync.Pending == 0 && x.Sync.SeatsOnline == 3 && x.Sync.Settling
		})
		if x.Sync.Seats != 3 || x.Sync.Quorum != 2 {
			t.Errorf("%s: sync %+v", p.displayName(), x.Sync)
		}
		// the first people to join become admins and settlers: the library's defaults
		for _, name := range []string{"Ana", "Bia", "Caio"} {
			if q := x.person(name); !q.Admin || !q.Seat || !q.Online || !q.Member {
				t.Errorf("%s sees %s as %+v", p.displayName(), name, q)
			}
		}
	}
	titles := a.board(t).titles()
	for _, p := range ps[1:] {
		if got := p.board(t).titles(); got != titles {
			t.Errorf("%s sees %q, Ana sees %q", p.displayName(), got, titles)
		}
	}
	if x := a.board(t); x.person("Bia").Online != true || x.person("Caio").Online != true {
		t.Errorf("online: %+v", x.People)
	}

	// the activity list says who did what
	act, err := a.Activity(0, 50)
	if err != nil || act.More {
		t.Fatal(err, act.More)
	}
	var who []string
	names := map[string]string{}
	for _, p := range act.People {
		names[p.Key] = p.Name
	}
	for _, it := range act.Items {
		if it.Kind == "card-added" {
			who = append(who, names[it.Who]+":"+it.Text)
		}
	}
	for _, w := range []string{"Ana:from Ana", "Bia:from Bia", "Caio:from Caio"} {
		if !contains(who, w) {
			t.Errorf("activity %v lacks %s", who, w)
		}
	}
	if act.Items[0].Seq < act.Items[len(act.Items)-1].Seq {
		t.Error("the activity is not newest first")
	}

}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

var _ = errors.New
var _ = kolobraid.ErrNotAdmin
var _ = time.Second
