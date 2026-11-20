package main

import (
	"bytes"
	"encoding/binary"
	"math/rand/v2"
	"slices"
	"sync"
	"testing"

	kolobraid "github.com/ciranda-dev/kolo-braid"
	"github.com/ciranda-dev/kolo-braid/kolobraidtest"
)

func TestBetween(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	canon := func() []byte {
		b := make([]byte, 1+r.IntN(5))
		for i := range b {
			b[i] = byte(r.IntN(256))
		}
		if b[len(b)-1] == 0 {
			b[len(b)-1] = 1 + byte(r.IntN(255))
		}
		return b
	}
	for range 20000 {
		a, b := canon(), canon()
		if bytes.Compare(a, b) > 0 {
			a, b = b, a
		}
		got := between(a, b)
		if bytes.Equal(a, b) {
			if got != nil {
				t.Fatalf("between(%x, %x) = %x for equal bounds", a, b, got)
			}
			continue
		}
		if checkPos(got) != nil || bytes.Compare(a, got) >= 0 || bytes.Compare(got, b) >= 0 {
			t.Fatalf("between(%x, %x) = %x", a, b, got)
		}
		if got := between(nil, b); checkPos(got) != nil || bytes.Compare(got, b) >= 0 {
			t.Fatalf("between(nil, %x) = %x", b, got)
		}
		if got := between(a, nil); checkPos(got) != nil || bytes.Compare(got, a) <= 0 {
			t.Fatalf("between(%x, nil) = %x", a, got)
		}
	}
	if got := between(nil, nil); checkPos(got) != nil {
		t.Errorf("between(nil, nil) = %x", got)
	}
	if between([]byte{5}, []byte{5}) != nil || between([]byte{6}, []byte{5}) != nil {
		t.Error("bounds in the wrong order")
	}
}

// Adding at the end or at the start grows a position one byte per 255 additions, and always splitting the same gap
// takes many splits before the bound, which is reported.
func TestPositionsStayShort(t *testing.T) {
	var end, start []byte
	for range 20000 {
		end = between(end, nil)
		if end == nil {
			t.Fatal("no room at the end")
		}
	}
	for range 20000 {
		start = between(nil, start)
		if start == nil {
			t.Fatal("no room at the start")
		}
	}
	if want := 2 + 20000/255; len(end) > want || len(start) > want {
		t.Errorf("20000 additions: %d and %d bytes, want at most %d", len(end), len(start), want)
	}
	lo, hi := []byte{0x80}, []byte{0x81}
	n := 0
	for {
		next := betweenLimit(lo, hi, 128)
		if next == nil {
			break
		}
		hi = next
		n++
	}
	if n < 100 {
		t.Errorf("only %d bisections of one gap before the bound", n)
	}
}

func TestPlan(t *testing.T) {
	s := func(ps ...byte) (out []sibling) {
		for i, p := range ps {
			out = append(out, sibling{oid{byte(i + 1)}, []byte{p}})
		}
		return out
	}
	sl, err := plan(s(0x40, 0x80), 1)
	if err != nil || sl.Fix != nil || bytes.Compare(sl.Pos, []byte{0x40}) <= 0 || bytes.Compare(sl.Pos, []byte{0x80}) >= 0 {
		t.Errorf("between: %+v %v", sl, err)
	}
	if sl, _ = plan(nil, 5); checkPos(sl.Pos) != nil {
		t.Errorf("empty list: %+v", sl)
	}
	if sl, _ = plan(s(0x40, 0x80), 99); bytes.Compare(sl.Pos, []byte{0x80}) <= 0 {
		t.Errorf("past the end: %+v", sl)
	}
	if sl, _ = plan(s(0x40, 0x80), -3); bytes.Compare(sl.Pos, []byte{0x40}) >= 0 {
		t.Errorf("before the start: %+v", sl)
	}
	// two siblings share a position: the one after the slot moves up so that the new one fits between them
	sib := s(0x40, 0x40, 0x40, 0x80)
	sl, err = plan(sib, 2)
	if err != nil || sl.Fix == nil || sl.Fix.ID != sib[2].ID {
		t.Fatalf("tie: %+v %v", sl, err)
	}
	if !(bytes.Compare([]byte{0x40}, sl.Pos) < 0 && bytes.Compare(sl.Pos, sl.Fix.Pos) < 0 && bytes.Compare(sl.Fix.Pos, []byte{0x80}) < 0) {
		t.Errorf("tie: new %x, moved %x", sl.Pos, sl.Fix.Pos)
	}
	// no room: the bound is reached
	full := bytes.Repeat([]byte{255}, maxPosBytes)
	if _, err := plan([]sibling{{oid{1}, full}}, 1); err != ErrNoRoom {
		t.Errorf("at the bound: %v", err)
	}
}

// opGen builds random operations, most of them about things that exist and some about things that do not.
type opGen struct {
	r       *rand.Rand
	members []oid
}

func (g *opGen) pick(ids []oid) oid {
	if len(ids) == 0 || g.r.IntN(12) == 0 {
		var ghost oid
		for i := range ghost {
			ghost[i] = byte(g.r.IntN(256))
		}
		return ghost
	}
	return ids[g.r.IntN(len(ids))]
}

func (g *opGen) text(max int) string {
	words := []string{"alpha", "beta", "gamma", "delta", "launch", "fix", "docs", "review", "Ana", "Bia"}
	s := words[g.r.IntN(len(words))]
	if g.r.IntN(5) == 0 {
		s += " " + words[g.r.IntN(len(words))]
	}
	return s[:min(len(s), max)]
}

func (g *opGen) position() []byte {
	if g.r.IntN(3) == 0 {
		return []byte{byte(1 + g.r.IntN(8)*16)} // few values: many ties
	}
	return between(nil, nil)
}

// next makes an operation from the board as the member sees it; the second result is an operation to write first.
func (g *opGen) next(m *model) (op, *op) {
	var cols, cards []oid
	for _, c := range m.Columns {
		cols = append(cols, c.ID)
		for _, k := range c.Cards {
			cards = append(cards, k.ID)
		}
	}
	insert := func(col *modelColumn, except oid) (slot, *op) {
		sl, err := plan(cardSiblings(col, except), g.r.IntN(8))
		if err != nil {
			return slot{Pos: g.position()}, nil
		}
		if sl.Fix != nil {
			return sl, &op{Kind: opCardMove, ID: sl.Fix.ID, Col: col.ID, Pos: sl.Fix.Pos}
		}
		return sl, nil
	}
	switch w := g.r.IntN(100); {
	case w < 6 || len(cols) == 0:
		sl, err := plan(columnSiblings(m, oid{}), g.r.IntN(5))
		if err != nil {
			sl.Pos = g.position()
		}
		return op{Kind: opColAdd, Text: g.text(maxColumnTitleRunes), Pos: sl.Pos}, nil
	case w < 9:
		return op{Kind: opColRename, ID: g.pick(cols), Text: g.text(maxColumnTitleRunes)}, nil
	case w < 12:
		id := g.pick(cols)
		sl, err := plan(columnSiblings(m, id), g.r.IntN(5))
		if err != nil {
			sl.Pos = g.position()
		}
		return op{Kind: opColMove, ID: id, Pos: sl.Pos}, nil
	case w < 13:
		return op{Kind: opColDel, ID: g.pick(cols)}, nil
	case w < 40:
		col := g.pick(cols)
		sl, fix := insert(m.column(col), oid{})
		o := op{Kind: opCardAdd, Col: col, Text: g.text(maxCardTitleRunes), Desc: g.text(maxCardDescRunes), Color: uint8(g.r.IntN(maxColor + 1)), Pos: sl.Pos}
		if g.r.IntN(4) == 0 {
			o.HasAssignee, o.Assignee = true, g.pick(g.members)
		}
		return o, fix
	case w < 68:
		o := op{Kind: opCardEdit, ID: g.pick(cards), Mask: uint64(1 + g.r.IntN(fieldsAll))}
		o.Text, o.Desc, o.Color = g.text(maxCardTitleRunes), g.text(maxCardDescRunes), uint8(g.r.IntN(maxColor+1))
		if g.r.IntN(3) > 0 {
			o.HasAssignee, o.Assignee = true, g.pick(g.members)
		}
		return o, nil
	case w < 88:
		id, col := g.pick(cards), g.pick(cols)
		sl, fix := insert(m.column(col), id)
		return op{Kind: opCardMove, ID: id, Col: col, Pos: sl.Pos}, fix
	case w < 95:
		return op{Kind: opCardDel, ID: g.pick(cards)}, nil
	case w < 98:
		return op{Kind: opProfile, Text: g.text(maxNameRunes)}, nil
	}
	return op{Kind: opTitle, Text: g.text(maxBoardTitleRunes)}, nil
}

// violations lists what is wrong with a view, whatever order built it: every record decodes and fits its bounds, every card
// is in a column that exists, the activity list has no gap.
func violations(s *kolobraid.Snapshot) (out []string) {
	cols := map[oid]bool{}
	for k, v := range s.Scan([]byte(prefixCol), prefixEnd(prefixCol)) {
		c, err := decodeColumn(v)
		if err != nil || checkPos(c.Pos) != nil || checkText(c.Title, maxColumnTitleRunes, true) != nil || len(k) != len(prefixCol)+idSize {
			out = append(out, "column "+string(k))
			continue
		}
		cols[oid(k[len(prefixCol):])] = true
	}
	for k, v := range s.Scan([]byte(prefixCard), prefixEnd(prefixCard)) {
		c, err := decodeCard(v)
		switch {
		case err != nil || len(k) != len(prefixCard)+idSize:
			out = append(out, "card does not decode")
		case !cols[c.Col]:
			out = append(out, "card in a column that does not exist")
		case checkPos(c.Pos) != nil || checkText(c.Title, maxCardTitleRunes, true) != nil || checkText(c.Desc, maxCardDescRunes, false) != nil || c.Color > maxColor:
			out = append(out, "card out of bounds")
		}
	}
	for k, v := range s.Scan([]byte(prefixName), prefixEnd(prefixName)) {
		if len(k) != len(prefixName)+idSize || checkText(string(v), maxNameRunes, true) != nil {
			out = append(out, "name")
		}
	}
	var n uint64
	for range s.Scan([]byte(prefixAct), prefixEnd(prefixAct)) {
		n++
	}
	if v, err := s.Get([]byte(keySeq)); err != nil {
		if n != 0 {
			out = append(out, "activity records without a counter")
		}
	} else if len(v) != 8 || binary.BigEndian.Uint64(v) != n {
		out = append(out, "activity counter differs from the records")
	}
	return out
}

// historyOf lists the effective data entries of a member's order, as a history for kolobraidtest.
func historyOf(m *tmember) []kolobraid.Entry {
	var es []kolobraid.Entry
	for o, err := range m.b.Entries(0) {
		if err != nil {
			break
		}
		if o.Kind == kolobraid.KindData && !o.Void {
			es = append(es, o.Entry)
		}
	}
	return es
}

// Three members on three stores write random operations at once from what each sees; every store ends with the same
// settled view, which holds no broken record, and apply gives the same view for the order they agreed on on every path
// kolobraidtest tries.
func TestThreeMembersConverge(t *testing.T) {
	seeds := []uint64{1, 2, 3, 4, 5}
	if testing.Short() {
		seeds = seeds[:1]
	}
	for _, seed := range seeds {
		t.Run("", func(t *testing.T) {
			ms := newMesh(t, 3)
			keys := make([]oid, len(ms.members))
			for i, m := range ms.members {
				keys[i] = m.key()
			}
			var wg sync.WaitGroup
			for i, m := range ms.members {
				g := &opGen{r: rand.New(rand.NewPCG(seed, uint64(i))), members: keys}
				wg.Go(func() {
					for range 40 {
						v := m.b.View()
						o, first := g.next(readModel(v))
						v.Close()
						for _, o := range []*op{first, &o} {
							if o == nil {
								continue
							}
							body, err := o.encode()
							if err != nil {
								t.Errorf("%s: %v", m.name, err)
								return
							}
							ref, err := m.b.Append(testContext(t), body)
							if err != nil {
								t.Errorf("%s: %v", m.name, err)
								return
							}
							m.refs = append(m.refs, ref)
						}
					}
				})
			}
			wg.Wait()
			if t.Failed() {
				return
			}
			views := ms.converge()
			for i, m := range ms.members {
				if st := m.b.Status(); st.Halted != nil {
					t.Fatalf("member %d halted: %v", i, st.Halted)
				}
				s := m.b.Settled()
				if bad := violations(s); len(bad) != 0 {
					t.Errorf("member %d: %v", i, bad)
				}
				s.Close()
			}
			if len(views[0]) < 20 {
				t.Errorf("only %d pairs: the operations did not do much", len(views[0]))
			}
			var rejected, total int
			for _, o := range ms.members[0].ordered() {
				if o.Kind == kolobraid.KindData {
					total++
					if o.Rejected {
						rejected++
					}
				}
			}
			t.Logf("seed %d: %d entries, %d rejected, %d pairs in the view", seed, total, rejected, len(views[0]))
			if rejected == total {
				t.Error("everything was rejected")
			}
			kolobraidtest.CheckApply(t, apply, historyOf(ms.members[0]))
		})
	}
}

// The same history, delivered to replicas in their own orders, gives every replica the same running digest.
func TestApplyIsDeterministic(t *testing.T) {
	for seed := range uint64(6) {
		r := rand.New(rand.NewPCG(seed, 99))
		var writers [][]byte
		for i := range 3 {
			writers = append(writers, bytes.Repeat([]byte{byte(i + 1)}, idSize))
		}
		var keys []oid
		for _, w := range writers {
			keys = append(keys, oid(w))
		}
		g := &opGen{r: r, members: keys}
		entries := []kolobraid.Entry{{Writer: writers[0], Kind: kolobraid.KindGenesis}}
		m := &model{}
		for i := range 150 {
			e := kolobraid.Entry{Writer: writers[r.IntN(3)], Kind: kolobraid.KindData}
			for j := range e.ID {
				e.ID[j] = byte(r.IntN(256))
			}
			e.ID[0], e.ID[1] = byte(i), byte(i>>8) // distinct
			switch {
			case r.IntN(15) == 0: // a body that is not an operation
				e.Body = []byte{byte(r.IntN(256)), byte(r.IntN(256))}
			default:
				o, _ := g.next(m)
				body, err := o.encode()
				if err != nil {
					t.Fatal(err)
				}
				e.Body = body
				m = modelAfter(m, o, oid(e.ID))
			}
			entries = append(entries, e)
		}
		kolobraidtest.CheckApply(t, apply, entries)
		d, err := kolobraidtest.FindDivergence(apply, entries, 4, seed)
		if err != nil || d != nil {
			t.Fatalf("seed %d: %v %v", seed, d, err)
		}
	}
}

// modelAfter is a rough model of what an operation creates, so that later operations in a generated history refer to
// things that exist; it need not match apply, whose rules the test then checks.
func modelAfter(m *model, o op, id oid) *model {
	switch o.Kind {
	case opColAdd:
		m.Columns = append(m.Columns, &modelColumn{ID: id, column: column{Title: o.Text, Pos: o.Pos}})
	case opCardAdd:
		if c := m.column(o.Col); c != nil {
			c.Cards = append(c.Cards, &modelCard{ID: id, card: card{Pos: o.Pos}})
		}
	}
	return m
}

// A reorder keeps every id and changes only positions, so a view read after it holds the same items.
func TestReadModelOrdersByPositionThenID(t *testing.T) {
	f := newFounder(t)
	col := f.write(op{Kind: opColAdd, Text: "c", Pos: pos(0x80)})
	var refs []kolobraid.EntryRef
	for range 300 { // more than a small sort would keep in order by itself
		refs = append(refs, f.write(op{Kind: opCardAdd, Col: oid(col.ID), Text: "x", Pos: pos(0x80)}))
	}
	cards := f.board(refs...).Columns[0].Cards
	ids := make([]oid, len(cards))
	for i, c := range cards {
		ids[i] = c.ID
	}
	want := slices.Clone(ids)
	slices.SortFunc(want, func(a, b oid) int { return bytes.Compare(a[:], b[:]) })
	if !slices.Equal(ids, want) {
		t.Errorf("a tie is not ordered by ID: %v", ids)
	}

	// ties among other positions: the sort moves cards across, so only the ID keeps a tie in one order
	col2 := f.write(op{Kind: opColAdd, Text: "d", Pos: pos(0x90)})
	refs = refs[:0]
	for i := range 300 {
		refs = append(refs, f.write(op{Kind: opCardAdd, Col: oid(col2.ID), Text: "y", Pos: pos(byte(0x40 + 0x40*(i%3)))}))
	}
	cards = f.board(refs...).Columns[1].Cards
	got := make([]*modelCard, len(cards))
	copy(got, cards)
	slices.SortFunc(got, func(a, b *modelCard) int {
		if c := bytes.Compare(a.Pos, b.Pos); c != 0 {
			return c
		}
		return bytes.Compare(a.ID[:], b.ID[:])
	})
	for i := range cards {
		if cards[i].ID != got[i].ID {
			t.Fatalf("card %d of mixed positions is out of order", i)
		}
	}
}
