package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode"

	kolobraid "github.com/ciranda-dev/kolo-braid"
)

// shown reports a character a window must never get in text of a person: hidden ones, which cleanLine drops.
func shown(s string) bool {
	for _, r := range s {
		if hidden(r) && r != '\n' {
			return false
		}
	}
	return true
}

// rawWriter is a member that is not this app: it writes any body it likes to its own strand.
func rawWriter(t *testing.T, p *peer) func(body []byte) {
	t.Helper()
	return func(body []byte) {
		t.Helper()
		ctx, cancel := time.AfterFunc(0, func() {}), func() {}
		_ = ctx
		defer cancel()
		br := p.BoardService.board().braid.Load()
		if _, err := br.Append(testContext(t), body); err != nil {
			t.Fatalf("raw append: %v", err)
		}
	}
}

func opBody(t *testing.T, o op) []byte {
	t.Helper()
	b, err := o.encode()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// A member that writes what it likes cannot make another member's window show a character that is not text: names, card
// text, column and board titles are cleaned where they leave Go, in every place a window reads them.
func TestHostileTextIsCleanedEverywhere(t *testing.T) {
	ps := network(t, "Ana", "Bia")
	a, b := ps[0], ps[1]
	if _, err := a.CreateBoard("Launch", nil); err != nil {
		t.Fatal(err)
	}
	invited(t, a, b, InviteOptions{TTLSeconds: 3600})
	b.waitBoard(t, "had the board", func(x Board) bool { return len(x.Columns) == 3 })
	todo := oidOf(t, a.board(t).column("To do").ID)
	raw := rawWriter(t, b)
	evil := "Evil\u202E\u2066\u200B\u0000\u0007 name\u3164"
	raw(opBody(t, op{Kind: opProfile, Text: evil}))
	raw(opBody(t, op{Kind: opCardAdd, Col: todo, Text: "pay\u202Eevil\u200D\ufeff\u0001 now", Desc: "a\u2028b\r\n\u202Ec\u0000\u200Bd", Pos: pos(0x80), Color: 3}))
	raw(opBody(t, op{Kind: opColAdd, Text: "col\u202E\u2069umn", Pos: pos(0xF0)}))
	raw(opBody(t, op{Kind: opTitle, Text: "Ti\u202Etle"}))
	x := a.waitBoard(t, "showed the hostile card", func(x Board) bool {
		k, _ := x.card("payevil now")
		return k != nil && x.Title == "Title" && x.column("column") != nil
	})
	k, _ := x.card("payevil now")
	if k.Description != "a\nb\ncd" || k.Color != "yellow" {
		t.Errorf("description %q", k.Description)
	}
	p := x.person("Evil name")
	if p == nil || p.Name != "Evil name" {
		t.Fatalf("people %+v", x.People)
	}
	act, err := a.Activity(0, 50)
	if err != nil {
		t.Fatal(err)
	}
	buf, _ := json.Marshal([]any{x, act, a.Notch(), a.trayNow()})
	if !shown(string(buf)) {
		t.Errorf("a window gets a hidden character: %q", buf)
	}
	a.rec.mu.Lock()
	events := append(append([]string(nil), a.rec.all...), a.rec.notchRaw...)
	a.rec.mu.Unlock()
	for _, e := range events {
		var v any
		if json.Unmarshal([]byte(e[strings.Index(e, " ")+1:]), &v) == nil {
			if enc, _ := json.Marshal(v); !shown(string(jsonText(enc))) {
				t.Errorf("an event holds a hidden character: %.100q", e)
			}
		}
	}
	// a name made of nothing a person can see becomes "Guest" and the first digits of the key
	raw(opBody(t, op{Kind: opProfile, Text: "\u3164\u200B\u2800"}))
	x = a.waitBoard(t, "showed the empty name as a guest", func(x Board) bool {
		for _, q := range x.People {
			if strings.HasPrefix(q.Name, "Guest ") {
				return true
			}
		}
		return false
	})
	for _, q := range x.People {
		if strings.HasPrefix(q.Name, "Guest ") && q.Name != "Guest "+q.Tag {
			t.Errorf("guest %q for tag %q", q.Name, q.Tag)
		}
	}
	// two members with one name are told apart by their tags
	raw(opBody(t, op{Kind: opProfile, Text: "Ana"}))
	x = a.waitBoard(t, "had two Anas", func(x Board) bool {
		n := 0
		for _, q := range x.People {
			if q.Name == "Ana" {
				n++
			}
		}
		return n == 2
	})
	tags := map[string]bool{}
	for _, q := range x.People {
		tags[q.Tag] = true
	}
	if len(tags) != len(x.People) {
		t.Errorf("tags %+v", x.People)
	}
}

// jsonText undoes the \u escapes of encoding/json so that a check sees the characters themselves.
func jsonText(enc []byte) string {
	var out strings.Builder
	var v any
	_ = json.Unmarshal(enc, &v)
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case string:
			out.WriteString(x)
		case []any:
			for _, e := range x {
				walk(e)
			}
		case map[string]any:
			for k, e := range x {
				out.WriteString(k)
				walk(e)
			}
		}
	}
	walk(v)
	return out.String()
}

func oidOf(t *testing.T, s string) oid {
	t.Helper()
	id, ok := parseOID(s)
	if !ok {
		t.Fatalf("not an ID: %q", s)
	}
	return id
}

// A member that writes bodies that are not board operations changes nothing for the others, halts nothing, and is not in
// the activity list.
func TestHostileEntriesChangeNothing(t *testing.T) {
	ps := network(t, "Ana", "Bia")
	a, b := ps[0], ps[1]
	if _, err := a.CreateBoard("Launch", nil); err != nil {
		t.Fatal(err)
	}
	invited(t, a, b, InviteOptions{TTLSeconds: 3600})
	todo := a.board(t).column("To do").ID
	if _, err := a.AddCard(todo, CardInput{Title: "mine"}, 0); err != nil {
		t.Fatal(err)
	}
	b.waitBoard(t, "had the card", func(x Board) bool { k, _ := x.card("mine"); return k != nil })
	before, _ := a.Activity(0, 50)
	raw := rawWriter(t, b)
	huge := make([]byte, kolobraid.GroupConfig{}.WithDefaults().MaxBody)
	for i := range huge {
		huge[i] = byte(i)
	}
	for _, body := range [][]byte{nil, {}, {0xff}, []byte("hello"), []byte(strings.Repeat("x", 4096)), huge, huge[:len(huge)/2],
		append([]byte{}, opBody(t, op{Kind: opCardDel, ID: oidOf(t, todo)})...), // a delete of a column ID as a card
		opBody(t, op{Kind: opCardEdit, ID: oid{1}, Mask: fieldTitle, Text: "ghost"}),
		opBody(t, op{Kind: opCardMove, ID: oidOf(t, todo), Col: oidOf(t, todo), Pos: pos(1)}),
	} {
		raw(body)
	}
	// then one honest card from Bia, which must still go through
	if _, err := b.AddCard(todo, CardInput{Title: "from Bia"}, 0); err != nil {
		t.Fatal(err)
	}
	x := a.waitBoard(t, "had Bia's card", func(x Board) bool { k, _ := x.card("from Bia"); return k != nil && !k.Syncing })
	if got := x.titles(); got != "To do:from Bia,mine | Doing: | Done:" {
		t.Errorf("the board is %q", got)
	}
	act, _ := a.Activity(0, 50)
	if len(act.Items) != len(before.Items)+1 || act.Items[0].Kind != "card-added" || act.Items[0].Text != "from Bia" {
		t.Errorf("activity %d -> %d: %+v", len(before.Items), len(act.Items), act.Items[0])
	}
	for _, p := range ps {
		if st := p.BoardService.board().braid.Load().Status(); st.Halted != nil || st.StrandLost || st.Unconfirmed {
			t.Errorf("%s: %+v", p.displayName(), st)
		}
		if x := p.board(t); x.Error != nil || !x.CanEdit {
			t.Errorf("%s: error %+v", p.displayName(), x.Error)
		}
	}
}

// Many cards, and the largest ones, are read, shown and settled; a window gets one page of the activity list, not all of it.
func TestManyAndHugeCards(t *testing.T) {
	p := startPeer(t, "Ana", Config{}, nil)
	if _, err := p.CreateBoard("Big", []string{"One", "Two"}); err != nil {
		t.Fatal(err)
	}
	cols := p.board(t).Columns
	const n = 1500
	for i := range n {
		if _, err := p.AddCard(cols[i%2].ID, CardInput{Title: "card " + strings.Repeat("x", i%50)}, 99); err != nil {
			t.Fatal(err)
		}
	}
	huge := CardInput{Title: strings.Repeat("t", maxCardTitleRunes), Description: strings.Repeat("é", maxCardDescRunes)}
	if _, err := p.AddCard(cols[0].ID, huge, 0); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	x := p.waitBoard(t, "had them all", func(x Board) bool { return x.Sync.CardsSettled == n+1 && x.Sync.CardsSyncing == 0 })
	if len(x.Columns[0].Cards) != n/2+1 || len(x.Columns[1].Cards) != n/2 {
		t.Errorf("%d and %d cards", len(x.Columns[0].Cards), len(x.Columns[1].Cards))
	}
	if c := x.Columns[0].Cards[0]; len([]rune(c.Title)) != maxCardTitleRunes || len([]rune(c.Description)) != maxCardDescRunes {
		t.Errorf("the largest card is cut: %d and %d characters", len([]rune(c.Title)), len([]rune(c.Description)))
	}
	t0 := time.Now()
	for range 5 {
		if p.Board() == nil {
			t.Fatal("no board")
		}
	}
	read := time.Since(t0) / 5
	act, err := p.Activity(0, 50)
	if err != nil || len(act.Items) != 50 || !act.More {
		t.Fatalf("activity %d more %v %v", len(act.Items), act.More, err)
	}
	if more, _ := p.Activity(act.Items[len(act.Items)-1].Seq, 50); len(more.Items) != 50 {
		t.Errorf("the second page has %d items", len(more.Items))
	}
	if _, err := p.Activity(0, 0); explain(err).Reason != "activity-limit" {
		t.Errorf("a page of nothing: %v", err)
	}
	if huge, _ := p.Activity(1<<62, 50); len(huge.Items) != 50 {
		t.Errorf("a cursor past the end: %d items", len(huge.Items))
	}
	t.Logf("%d cards settled in %s; reading the board takes %s", n+1, time.Since(start).Round(time.Millisecond), read.Round(time.Millisecond))
	// and they survive a restart
	p = p.restart(t, "")
	x = p.waitBoard(t, "reopened", func(x Board) bool { return x.State == stateActive && x.Sync.CardsSettled == n+1 })
	if len(x.Columns[0].Cards) != n/2+1 {
		t.Error("cards lost in a restart")
	}
}

func TestLimitsAreTheApps(t *testing.T) {
	// the bounds on text are in characters and sum to what an entry and a view value hold, in the worst case of 4 bytes each
	maxBody := kolobraid.GroupConfig{}.WithDefaults().MaxBody
	if textBytes(maxCardTitleRunes)+textBytes(maxCardDescRunes)+maxPosBytes+idSize*3+64 > maxBody {
		t.Error("a card may not fit in an entry")
	}
	for _, r := range "\u3164\u200B\u202E\u0000\ufeff" {
		if !hidden(r) {
			t.Errorf("%U is shown", r)
		}
	}
	for _, r := range "aé中 \n" {
		if r != ' ' && r != '\n' && hidden(r) {
			t.Errorf("%U is hidden", r)
		}
	}
	if unicode.IsControl('\n') != true {
		t.Error("newline is a control character")
	}
}
