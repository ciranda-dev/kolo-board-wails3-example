package main

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	kolobraid "github.com/ciranda-dev/kolo-braid"
	koloid "github.com/ciranda-dev/kolo-id"
	kolosecure "github.com/ciranda-dev/kolo-secure"
	kolostore "github.com/ciranda-dev/kolo-store"
)

const testWait = 60 * time.Second

// tmember is a member of a braid made by hand, without the service: its identity, its store and its braid.
type tmember struct {
	t    *testing.T
	name string
	id   *koloid.Identity
	db   kolostore.Backend
	b    *kolobraid.Braid
	refs []kolobraid.EntryRef
}

func testOptions(db kolostore.Backend) kolobraid.Options {
	return kolobraid.Options{Backend: db, App: appTag, Apply: apply}
}

func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 4*testWait)
	t.Cleanup(cancel)
	return ctx
}

// newFounder creates a braid of one member on an in-memory store.
func newFounder(t *testing.T) *tmember {
	t.Helper()
	id, err := koloid.Generate()
	if err != nil {
		t.Fatal(err)
	}
	db := kolostore.NewMemory()
	b, err := kolobraid.Create(testContext(t), id, kolobraid.GroupConfig{App: appTag}, testOptions(db))
	if err != nil {
		t.Fatal(err)
	}
	m := &tmember{t: t, name: "founder", id: id, db: db, b: b}
	t.Cleanup(func() { b.Close() })
	return m
}

// write appends the encoded operation and returns its reference, whose ID is the ID of what it creates.
func (m *tmember) write(o op) kolobraid.EntryRef {
	m.t.Helper()
	body, err := o.encode()
	if err != nil {
		m.t.Fatalf("encode %+v: %v", o, err)
	}
	return m.raw(body)
}

// raw appends a body as it is: a member that is not this app can write anything.
func (m *tmember) raw(body []byte) kolobraid.EntryRef {
	m.t.Helper()
	ref, err := m.b.Append(testContext(m.t), body)
	if err != nil {
		m.t.Fatalf("append: %v", err)
	}
	m.refs = append(m.refs, ref)
	return ref
}

func (m *tmember) key() oid { return oid(m.id.PublicKey()) }

// ordered returns the order as this member has it, by entry ID.
func (m *tmember) ordered() map[kolobraid.EntryID]kolobraid.Ordered {
	out := map[kolobraid.EntryID]kolobraid.Ordered{}
	for o, err := range m.b.Entries(0) {
		if err != nil {
			break
		}
		out[o.ID] = o
	}
	return out
}

// settledAll reports whether every entry in refs is settled and applied here.
func (m *tmember) settledAll(refs []kolobraid.EntryRef) bool {
	ord := m.ordered()
	snap := m.b.Settled()
	defer snap.Close()
	for _, r := range refs {
		o, ok := ord[r.ID]
		if !ok || !o.Settled || o.Position > snap.Position() {
			return false
		}
	}
	return true
}

// settle waits until the entries (all this member wrote when none is given) are settled and applied here.
func (m *tmember) settle(refs ...kolobraid.EntryRef) {
	m.t.Helper()
	if len(refs) == 0 {
		refs = m.refs
	}
	deadline := time.Now().Add(testWait)
	for !m.settledAll(refs) {
		if time.Now().After(deadline) {
			m.t.Fatalf("%s: entries never settled (status %+v)", m.name, m.b.Status())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// board waits for the refs to settle and returns the settled board.
func (m *tmember) board(refs ...kolobraid.EntryRef) *model {
	m.t.Helper()
	m.settle(refs...)
	s := m.b.Settled()
	defer s.Close()
	return readModel(s)
}

// rejected reports whether the order holds the entry as rejected.
func (m *tmember) rejected(ref kolobraid.EntryRef) bool {
	m.t.Helper()
	m.settle(ref)
	return m.ordered()[ref.ID].Rejected
}

// dump is every pair of the settled view, for a comparison of two views.
func dump(b *kolobraid.Braid) []string {
	s := b.Settled()
	defer s.Close()
	var out []string
	for k, v := range s.Scan(nil, nil) {
		out = append(out, fmt.Sprintf("%x=%x", k, v))
	}
	return out
}

// mesh is members of one braid, each on a store of its own, joined as the library's examples do: the first creates the
// braid and admits the others, every pair of members has one kolo-secure connection on 127.0.0.1.
type mesh struct {
	t       *testing.T
	members []*tmember
}

// newMesh makes n members. With roles given, every joiner is admitted with exactly those roles (none: a plain member that
// is not a settler); without, joiners are admitted as the board admits them, with GrowSettlers.
func newMesh(t *testing.T, n int, roles ...kolobraid.Role) *mesh {
	t.Helper()
	admit := &kolobraid.AdmitOptions{GrowSettlers: true}
	if len(roles) > 0 {
		admit = &kolobraid.AdmitOptions{Roles: &roles[0]}
	}
	ctx := testContext(t)
	ms := &mesh{t: t}
	first := newFounder(t)
	first.name = "member0"
	ms.members = append(ms.members, first)
	for i := 1; i < n; i++ {
		id, err := koloid.Generate()
		if err != nil {
			t.Fatal(err)
		}
		db := kolostore.NewMemory()
		join, err := kolobraid.NewJoin(ctx, id, testOptions(db))
		if err != nil {
			t.Fatal(err)
		}
		req, err := join.Request(nil)
		if err != nil {
			t.Fatal(err)
		}
		welcome, err := first.b.Admit(ctx, id.PublicKey(), req, admit)
		if err != nil {
			t.Fatal(err)
		}
		b, err := join.Complete(ctx, welcome)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { b.Close() })
		ms.members = append(ms.members, &tmember{t: t, name: fmt.Sprintf("member%d", i), id: id, db: db, b: b})
	}
	// every member listens; each connects to the ones before it
	addrs := make([]string, n)
	for i, m := range ms.members {
		l, err := kolosecure.Listen(ctx, "127.0.0.1:0", m.id, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { l.Close() })
		addrs[i] = l.Addr().String()
		go func() {
			for {
				c, err := l.Accept(ctx)
				if err != nil {
					return
				}
				go m.b.ServeConn(ctx, c)
			}
		}()
	}
	for i, m := range ms.members {
		for j := range i {
			c, err := kolosecure.Dial(ctx, addrs[j], m.id, ms.members[j].id.PublicKey(), nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { c.Close() })
			go m.b.ServeConn(ctx, c)
		}
	}
	return ms
}

// converge waits until every member has every entry any member wrote settled and applied, then returns the views.
func (ms *mesh) converge() [][]string {
	ms.t.Helper()
	var all []kolobraid.EntryRef
	for _, m := range ms.members {
		all = append(all, m.refs...)
	}
	var views [][]string
	for _, m := range ms.members {
		m.settle(all...)
	}
	deadline := time.Now().Add(testWait)
	for {
		views = views[:0]
		for _, m := range ms.members {
			views = append(views, dump(m.b))
		}
		same := true
		for _, v := range views[1:] {
			same = same && slices.Equal(v, views[0])
		}
		if same {
			return views
		}
		if time.Now().After(deadline) {
			ms.t.Fatalf("the settled views never became equal")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// pos makes a position from a digit, for tests that need a few distinct ones.
func pos(d byte) []byte { return bytes.Clone([]byte{d}) }
