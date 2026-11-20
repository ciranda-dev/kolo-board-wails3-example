package main

import (
	"bytes"
	"strings"
	"testing"

	kolobraid "github.com/ciranda-dev/kolo-braid"
	kolocodec "github.com/ciranda-dev/kolo-codec"
)

// board is a founder with three columns and a card in the first, all settled.
type tboard struct {
	*tmember
	todo, doing, done kolobraid.EntryRef
	card              kolobraid.EntryRef
}

func newTestBoard(t *testing.T) *tboard {
	t.Helper()
	b := &tboard{tmember: newFounder(t)}
	b.todo = b.write(op{Kind: opColAdd, Text: "To do", Pos: pos(0x40)})
	b.doing = b.write(op{Kind: opColAdd, Text: "Doing", Pos: pos(0x80)})
	b.done = b.write(op{Kind: opColAdd, Text: "Done", Pos: pos(0xC0)})
	b.card = b.write(op{Kind: opCardAdd, Col: oid(b.todo.ID), Text: "Write docs", Desc: "all of them", Color: 2, Pos: pos(0x80)})
	return b
}

func (b *tboard) snapshot() *model { return b.board() }

func TestBoardBuilds(t *testing.T) {
	b := newTestBoard(t)
	m := b.snapshot()
	if len(m.Columns) != 3 || m.Columns[0].Title != "To do" || m.Columns[1].Title != "Doing" || m.Columns[2].Title != "Done" {
		t.Fatalf("columns %+v", m.Columns)
	}
	c := m.Columns[0].Cards
	if len(c) != 1 || c[0].ID != oid(b.card.ID) || c[0].Title != "Write docs" || c[0].Desc != "all of them" || c[0].Color != 2 ||
		c[0].CreatedBy != b.key() || c[0].LastBy != b.key() || c[0].LastOp != opCardAdd {
		t.Fatalf("card %+v", c)
	}
}

func TestEditChangesOnlyTheFieldsInTheMask(t *testing.T) {
	b := newTestBoard(t)
	e1 := b.write(op{Kind: opCardEdit, ID: oid(b.card.ID), Mask: fieldTitle, Text: "Write the docs"})
	e2 := b.write(op{Kind: opCardEdit, ID: oid(b.card.ID), Mask: fieldColor, Color: 5})
	e3 := b.write(op{Kind: opCardEdit, ID: oid(b.card.ID), Mask: fieldAssignee, HasAssignee: true, Assignee: b.key()})
	c := b.board(e1, e2, e3).Columns[0].Cards[0]
	if c.Title != "Write the docs" || c.Desc != "all of them" || c.Color != 5 || !c.HasAssignee || c.Assignee != b.key() {
		t.Fatalf("%+v", c)
	}
	e4 := b.write(op{Kind: opCardEdit, ID: oid(b.card.ID), Mask: fieldAssignee | fieldDesc, Desc: ""})
	c = b.board(e4).Columns[0].Cards[0]
	if c.HasAssignee || c.Desc != "" || c.Title != "Write the docs" || c.LastOp != opCardEdit {
		t.Fatalf("assignee and description cleared: %+v", c)
	}
}

func TestMoveAndReorder(t *testing.T) {
	b := newTestBoard(t)
	second := b.write(op{Kind: opCardAdd, Col: oid(b.todo.ID), Text: "Second", Pos: pos(0xC0)})
	mv := b.write(op{Kind: opCardMove, ID: oid(b.card.ID), Col: oid(b.doing.ID), Pos: pos(0x80)})
	m := b.board(second, mv)
	if len(m.Columns[0].Cards) != 1 || m.Columns[0].Cards[0].Title != "Second" || len(m.Columns[1].Cards) != 1 || m.Columns[1].Cards[0].Title != "Write docs" {
		t.Fatalf("after the move: %+v", m)
	}
	// the same position in one column: the ID decides, the same on every peer
	a := b.write(op{Kind: opCardAdd, Col: oid(b.done.ID), Text: "A", Pos: pos(0x80)})
	c := b.write(op{Kind: opCardAdd, Col: oid(b.done.ID), Text: "B", Pos: pos(0x80)})
	cards := b.board(a, c).Columns[2].Cards
	want := []string{"A", "B"}
	if bytes.Compare(a.ID[:], c.ID[:]) > 0 {
		want = []string{"B", "A"}
	}
	if len(cards) != 2 || cards[0].Title != want[0] || cards[1].Title != want[1] {
		t.Fatalf("tie: %+v want %v", cards, want)
	}
	// columns move too
	cm := b.write(op{Kind: opColMove, ID: oid(b.done.ID), Pos: pos(0x10)})
	if m = b.board(cm); m.Columns[0].Title != "Done" || m.Columns[3-1].Title != "Doing" {
		t.Fatalf("columns %+v", m.Columns)
	}
}

func TestRulesOfDeletion(t *testing.T) {
	b := newTestBoard(t)
	del := b.write(op{Kind: opCardDel, ID: oid(b.card.ID)})
	b.settle(del)
	// everything that comes after the delete in the order is rejected: no edit brings the card back
	edit := b.write(op{Kind: opCardEdit, ID: oid(b.card.ID), Mask: fieldTitle, Text: "again"})
	move := b.write(op{Kind: opCardMove, ID: oid(b.card.ID), Col: oid(b.done.ID), Pos: pos(0x80)})
	del2 := b.write(op{Kind: opCardDel, ID: oid(b.card.ID)})
	for name, ref := range map[string]kolobraid.EntryRef{"edit": edit, "move": move, "second delete": del2} {
		if !b.rejected(ref) {
			t.Errorf("%s of a deleted card was not rejected", name)
		}
	}
	if n := len(b.board().Columns[0].Cards); n != 0 {
		t.Errorf("%d cards after the delete", n)
	}
	// a column delete takes its cards with it, and nothing goes into it afterwards
	keep := b.write(op{Kind: opCardAdd, Col: oid(b.doing.ID), Text: "stays", Pos: pos(0x80)})
	lost := b.write(op{Kind: opCardAdd, Col: oid(b.todo.ID), Text: "goes", Pos: pos(0x80)})
	b.settle(keep, lost)
	cd := b.write(op{Kind: opColDel, ID: oid(b.todo.ID)})
	b.settle(cd)
	m := b.board()
	if len(m.Columns) != 2 || m.Columns[0].Title != "Doing" || len(m.Columns[0].Cards) != 1 {
		t.Fatalf("after deleting the column: %+v", m.Columns)
	}
	if _, c := m.find(oid(lost.ID)); c != nil {
		t.Error("a card of the deleted column is still there")
	}
	snap := b.b.Settled()
	if bad := violations(snap); len(bad) != 0 { // no card is left behind in the view, shown or not
		t.Errorf("the view after the column delete: %v", bad)
	}
	snap.Close()
	late := b.write(op{Kind: opCardAdd, Col: oid(b.todo.ID), Text: "late", Pos: pos(0x80)})
	into := b.write(op{Kind: opCardMove, ID: oid(keep.ID), Col: oid(b.todo.ID), Pos: pos(0x80)})
	renamed := b.write(op{Kind: opColRename, ID: oid(b.todo.ID), Text: "back"})
	moved := b.write(op{Kind: opColMove, ID: oid(b.todo.ID), Pos: pos(0x01)})
	again := b.write(op{Kind: opColDel, ID: oid(b.todo.ID)})
	for name, ref := range map[string]kolobraid.EntryRef{"add into it": late, "move into it": into, "rename": renamed, "move": moved, "delete again": again} {
		if !b.rejected(ref) {
			t.Errorf("%s: a deleted column was not refused", name)
		}
	}
	if m = b.board(); len(m.Columns) != 2 || len(m.Columns[0].Cards) != 1 || m.Columns[0].Cards[0].ID != oid(keep.ID) {
		t.Errorf("rejected entries changed the board: %+v", m.Columns)
	}
}

func TestEntriesAboutNothing(t *testing.T) {
	b := newTestBoard(t)
	var ghost oid
	ghost[0] = 9
	for name, o := range map[string]op{
		"rename":   {Kind: opColRename, ID: ghost, Text: "x"},
		"edit":     {Kind: opCardEdit, ID: ghost, Mask: fieldColor, Color: 1},
		"move":     {Kind: opCardMove, ID: ghost, Col: oid(b.todo.ID), Pos: pos(0x80)},
		"to ghost": {Kind: opCardMove, ID: oid(b.card.ID), Col: ghost, Pos: pos(0x80)},
		"add":      {Kind: opCardAdd, Col: ghost, Text: "x", Pos: pos(0x80)},
		"delete":   {Kind: opCardDel, ID: ghost},
	} {
		if !b.rejected(b.write(o)) {
			t.Errorf("%s: an entry about something that is not there was not rejected", name)
		}
	}
	// a card is not a column and a column is not a card
	if !b.rejected(b.write(op{Kind: opCardMove, ID: oid(b.card.ID), Col: oid(b.card.ID), Pos: pos(0x80)})) {
		t.Error("a card used as a column")
	}
	if !b.rejected(b.write(op{Kind: opCardDel, ID: oid(b.todo.ID)})) {
		t.Error("a column used as a card")
	}
}

func TestAssigneeMustBeAMember(t *testing.T) {
	b := newTestBoard(t)
	var stranger oid
	stranger[5] = 1
	if !b.rejected(b.write(op{Kind: opCardAdd, Col: oid(b.todo.ID), Text: "x", HasAssignee: true, Assignee: stranger, Pos: pos(0x80)})) {
		t.Error("a card added for a stranger")
	}
	if !b.rejected(b.write(op{Kind: opCardEdit, ID: oid(b.card.ID), Mask: fieldAssignee, HasAssignee: true, Assignee: stranger})) {
		t.Error("a card assigned to a stranger")
	}
	ok := b.write(op{Kind: opCardAdd, Col: oid(b.todo.ID), Text: "mine", HasAssignee: true, Assignee: b.key(), Pos: pos(0xA0)})
	if b.rejected(ok) {
		t.Error("a card assigned to a member was rejected")
	}
}

func TestOnlyAnAdminRenamesTheBoard(t *testing.T) {
	m := newMesh(t, 2, 0) // the second member is a plain member
	admin, plain := m.members[0], m.members[1]
	a := admin.write(op{Kind: opTitle, Text: "Launch"})
	plain.settle() // nothing yet: its own writes come after the join
	b := plain.write(op{Kind: opTitle, Text: "Hijacked"})
	admin.settle(a)
	admin.settle(b)
	if admin.rejected(a) || !admin.rejected(b) {
		t.Fatalf("rejected: admin's rename %v, plain member's %v", admin.rejected(a), admin.rejected(b))
	}
	if got := admin.board().Title; got != "Launch" {
		t.Errorf("title %q", got)
	}
}

func TestPerFieldMerge(t *testing.T) {
	m := newMesh(t, 2)
	a, b := m.members[0], m.members[1]
	col := a.write(op{Kind: opColAdd, Text: "To do", Pos: pos(0x80)})
	card := a.write(op{Kind: opCardAdd, Col: oid(col.ID), Text: "t", Pos: pos(0x80)})
	m.converge()
	// concurrent edits of different fields both take effect; of one field, the later in the order wins
	e1 := a.write(op{Kind: opCardEdit, ID: oid(card.ID), Mask: fieldTitle, Text: "from a"})
	e2 := b.write(op{Kind: opCardEdit, ID: oid(card.ID), Mask: fieldColor, Color: 3})
	e3 := b.write(op{Kind: opCardEdit, ID: oid(card.ID), Mask: fieldTitle, Text: "from b"})
	views := m.converge()
	c := a.board().Columns[0].Cards[0]
	if c.Color != 3 {
		t.Errorf("the color edit was lost: %+v", c)
	}
	order := a.ordered()
	later := "from a"
	if order[e3.ID].Position > order[e1.ID].Position {
		later = "from b"
	}
	if c.Title != later {
		t.Errorf("title %q, the later entry wrote %q (positions a %d, b %d)", c.Title, later, order[e1.ID].Position, order[e3.ID].Position)
	}
	if order[e2.ID].Rejected {
		t.Error("a concurrent edit was rejected")
	}
	if len(views) != 2 {
		t.Fatal("views")
	}
}

func TestActivityList(t *testing.T) {
	m := newMesh(t, 2)
	a, b := m.members[0], m.members[1]
	n1 := a.write(op{Kind: opProfile, Text: "Ana"})
	n2 := b.write(op{Kind: opProfile, Text: "Bia"})
	col := a.write(op{Kind: opColAdd, Text: "To do", Pos: pos(0x80)})
	m.converge()
	c1 := b.write(op{Kind: opCardAdd, Col: oid(col.ID), Text: "Buy milk", Pos: pos(0x80)})
	m.converge()
	e1 := a.write(op{Kind: opCardEdit, ID: oid(c1.ID), Mask: fieldColor | fieldDesc, Color: 1})
	col2 := a.write(op{Kind: opColAdd, Text: "Done", Pos: pos(0xC0)})
	m.converge()
	mv := b.write(op{Kind: opCardMove, ID: oid(c1.ID), Col: oid(col2.ID), Pos: pos(0x80)})
	rn := b.write(op{Kind: opProfile, Text: "Bianca"})
	m.converge()
	_ = []kolobraid.EntryRef{n1, n2, e1, rn, mv}
	s := a.b.Settled()
	defer s.Close()
	items, more := readActivity(s, 0, 50)
	if more {
		t.Error("more records than written")
	}
	var kinds []actKind
	for _, it := range items {
		kinds = append(kinds, it.Kind)
	}
	count := func(k actKind) (n int) {
		for _, x := range kinds {
			if x == k {
				n++
			}
		}
		return n
	}
	if count(actBoard) != 1 || count(actJoined) != 1 || count(actColAdd) != 2 || count(actCardAdd) != 1 || count(actCardEdit) != 1 || count(actCardMove) != 1 || count(actName) != 1 {
		t.Errorf("activity kinds %v", kinds)
	}
	// newest first, and each record has the entry it came from
	for i := 1; i < len(items); i++ {
		if items[i].Seq != items[i-1].Seq-1 {
			t.Errorf("sequence %d then %d", items[i-1].Seq, items[i].Seq)
		}
	}
	for _, it := range items {
		if it.Kind == actCardAdd && (it.Writer != b.key() || it.Text != "Buy milk" || it.Text2 != "To do" || it.Entry != oid(c1.ID)) {
			t.Errorf("card activity %+v", it)
		}
		if it.Kind == actCardMove && (it.Writer != b.key() || it.Text != "Buy milk" || it.Text2 != "Done" || it.Subject != oid(c1.ID)) {
			t.Errorf("move activity %+v", it)
		}
		if it.Kind == actJoined && it.Subject != b.key() {
			t.Errorf("joined %+v", it)
		}
	}
	// paging: one record per page walks the whole list
	var seen int
	for before := uint64(0); ; {
		page, more := readActivity(s, before, 1)
		seen += len(page)
		if len(page) == 1 {
			before = page[0].Seq
		}
		if !more {
			break
		}
	}
	if seen != len(items) {
		t.Errorf("paged %d records of %d", seen, len(items))
	}
}

func TestRemovedMemberIsInTheActivity(t *testing.T) {
	m := newMesh(t, 2)
	a, b := m.members[0], m.members[1]
	ref, err := a.b.RemoveMember(testContext(t), b.id.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	a.refs = append(a.refs, ref)
	a.settle()
	s := a.b.Settled()
	defer s.Close()
	items, _ := readActivity(s, 0, 50)
	if len(items) == 0 || items[0].Kind != actRemoved || items[0].Subject != b.key() || items[0].Writer != a.key() {
		t.Fatalf("%+v", items)
	}
}

// bodies that are not what this app writes: each is rejected, writes nothing, and halts nothing.
func TestMalformedEntriesAreRejected(t *testing.T) {
	b := newTestBoard(t)
	var e kolocodec.Encoder
	enc := func(f func(*kolocodec.Encoder)) []byte {
		e = kolocodec.Encoder{}
		e.PutString(opTag)
		f(&e)
		return e.Bytes()
	}
	good, _ := op{Kind: opCardEdit, ID: oid(b.card.ID), Mask: fieldTitle, Text: "ok"}.encode()
	long := strings.Repeat("a", maxCardTitleRunes+1)
	cases := map[string][]byte{
		"empty body": {},
		"one byte":   {0},
		"another tag": func() []byte {
			var x kolocodec.Encoder
			x.PutString("kolo-board/op/v1")
			x.PutUvarint(1)
			x.PutString("x")
			return x.Bytes()
		}(),
		"no operation":   enc(func(*kolocodec.Encoder) {}),
		"operation 0":    enc(func(e *kolocodec.Encoder) { e.PutUvarint(0) }),
		"unknown":        enc(func(e *kolocodec.Encoder) { e.PutUvarint(99) }),
		"huge operation": enc(func(e *kolocodec.Encoder) { e.PutUvarint(1 << 62) }),
		"cut short":      good[:len(good)-3],
		"trailing bytes": append(bytes.Clone(good), 7),
		"profile blank":  enc(func(e *kolocodec.Encoder) { e.PutUvarint(uint64(opProfile)); e.PutString("   ") }),
		"profile long": enc(func(e *kolocodec.Encoder) {
			e.PutUvarint(uint64(opProfile))
			e.PutString(strings.Repeat("n", maxNameRunes+1))
		}),
		"title blank": enc(func(e *kolocodec.Encoder) {
			e.PutUvarint(uint64(opCardEdit))
			e.PutFixed(b.card.ID[:])
			e.PutUvarint(fieldTitle)
			e.PutString("")
		}),
		"title long": enc(func(e *kolocodec.Encoder) {
			e.PutUvarint(uint64(opCardEdit))
			e.PutFixed(b.card.ID[:])
			e.PutUvarint(fieldTitle)
			e.PutString(long)
		}),
		"bad utf8": enc(func(e *kolocodec.Encoder) {
			e.PutUvarint(uint64(opCardEdit))
			e.PutFixed(b.card.ID[:])
			e.PutUvarint(fieldTitle)
			e.PutBytes([]byte{0xff, 0xfe})
		}),
		"mask 0": enc(func(e *kolocodec.Encoder) {
			e.PutUvarint(uint64(opCardEdit))
			e.PutFixed(b.card.ID[:])
			e.PutUvarint(0)
		}),
		"mask 16": enc(func(e *kolocodec.Encoder) {
			e.PutUvarint(uint64(opCardEdit))
			e.PutFixed(b.card.ID[:])
			e.PutUvarint(16)
			e.PutUvarint(0)
		}),
		"color 7": enc(func(e *kolocodec.Encoder) {
			e.PutUvarint(uint64(opCardEdit))
			e.PutFixed(b.card.ID[:])
			e.PutUvarint(fieldColor)
			e.PutUvarint(7)
		}),
		"short id":      enc(func(e *kolocodec.Encoder) { e.PutUvarint(uint64(opCardDel)); e.PutFixed(b.card.ID[:5]) }),
		"non-canonical": append(bytes.Clone(good[:len(opTag)+1]), 0x80+byte(opCardEdit), 0x00),
		"pos zero tail": enc(func(e *kolocodec.Encoder) { e.PutUvarint(uint64(opColAdd)); e.PutString("x"); e.PutBytes([]byte{1, 0}) }),
		"pos empty":     enc(func(e *kolocodec.Encoder) { e.PutUvarint(uint64(opColAdd)); e.PutString("x"); e.PutBytes(nil) }),
		"pos long": enc(func(e *kolocodec.Encoder) {
			e.PutUvarint(uint64(opColAdd))
			e.PutString("x")
			e.PutBytes(bytes.Repeat([]byte{1}, maxPosBytes+1))
		}),
		"bool 2": enc(func(e *kolocodec.Encoder) {
			e.PutUvarint(uint64(opCardEdit))
			e.PutFixed(b.card.ID[:])
			e.PutUvarint(fieldAssignee)
			e.PutFixed([]byte{2})
		}),
	}
	b.settle()
	before := dump(b.b)
	for name, body := range cases {
		ref := b.raw(body)
		if !b.rejected(ref) {
			t.Errorf("%s: not rejected", name)
		}
	}
	// the only change is the number of activity records that the rejected entries did not add: none
	if after := dump(b.b); strings.Join(after, "\n") != strings.Join(before, "\n") {
		t.Error("rejected entries changed the view")
	}
	if st := b.b.Status(); st.Halted != nil {
		t.Fatalf("the view halted: %v", st.Halted)
	}
}

// the largest honest card fits the entry and the view; one character more, or a body of the entry's largest size, does not.
func TestLimitsFit(t *testing.T) {
	cfg := kolobraid.GroupConfig{}.WithDefaults()
	most := op{Kind: opCardAdd, Col: oid{1}, Text: strings.Repeat("\U0010FFFF", maxCardTitleRunes), Desc: strings.Repeat("\U0010FFFF", maxCardDescRunes), Pos: bytes.Repeat([]byte{255}, maxPosBytes)}
	body, err := most.encode()
	if err != nil {
		t.Fatal(err)
	}
	if len(body) > cfg.MaxBody {
		t.Errorf("the largest card entry is %d bytes, the braid takes %d", len(body), cfg.MaxBody)
	}
	rec := card{Col: oid{1}, Title: most.Text, Desc: most.Desc, Pos: most.Pos}.encode()
	if len(rec) > kolobraid.MaxViewValue {
		t.Errorf("the largest card record is %d bytes, the view takes %d", len(rec), kolobraid.MaxViewValue)
	}
	if _, err := (op{Kind: opCardAdd, Col: oid{1}, Text: "x", Desc: strings.Repeat("d", maxCardDescRunes+1), Pos: pos(1)}).encode(); err == nil {
		t.Error("a description over the bound encodes")
	}
	if len(appTag) < 1 || len(appTag) > kolobraid.MaxAppTag {
		t.Error("app tag")
	}
}

func TestHugeCardsAndManyCards(t *testing.T) {
	b := newTestBoard(t)
	huge := b.write(op{Kind: opCardAdd, Col: oid(b.todo.ID), Text: strings.Repeat("t", maxCardTitleRunes), Desc: strings.Repeat("é", maxCardDescRunes), Pos: pos(0xA0)})
	// the entry's largest body: not a card the app can write, so the app side of every peer rejects it
	var e kolocodec.Encoder
	e.PutString(opTag)
	e.PutUvarint(uint64(opCardAdd))
	e.PutFixed(b.todo.ID[:])
	e.PutString("big")
	e.PutBytes(bytes.Repeat([]byte("x"), kolobraid.MaxViewValue-1024)) // a description of 1 MiB less a little: over the character bound
	body := e.Bytes()
	if len(body) > kolobraid.MaxViewValue {
		t.Fatal("the test body is larger than the braid takes")
	}
	hostile := b.raw(body)
	if !b.rejected(hostile) {
		t.Error("a description over the character bound was accepted")
	}
	var refs []kolobraid.EntryRef
	for i := range 300 {
		refs = append(refs, b.write(op{Kind: opCardAdd, Col: oid(b.doing.ID), Text: strings.Repeat("c", 1+i%200), Pos: between(nil, nil)}))
	}
	m := b.board(append(refs, huge)...)
	if n := len(m.Columns[1].Cards); n != 300 {
		t.Errorf("%d cards of 300", n)
	}
	if _, c := m.find(oid(huge.ID)); c == nil || len([]rune(c.Desc)) != maxCardDescRunes {
		t.Error("the largest card is not whole")
	}
	if b.b.Status().Halted != nil {
		t.Fatal("halted")
	}
}
