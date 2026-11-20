package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	kolobraid "github.com/ciranda-dev/kolo-braid"
	koloid "github.com/ciranda-dev/kolo-id"
	kolostore "github.com/ciranda-dev/kolo-store"
	koloswarm "github.com/ciranda-dev/kolo-swarm"
	kolotransport "github.com/ciranda-dev/kolo-transport"
)

// Every limit of the app is recomputed here from the library constant it comes from. A limit changed without its source
// constant, or a source constant that moves, fails this test.
func TestLimitsComeFromTheirSources(t *testing.T) {
	cfg := kolobraid.GroupConfig{}.WithDefaults()
	short := kolobraid.MaxViewKey / utf8.UTFMax // kolo-braid MaxViewKey: the bound of a view key, in characters
	for name, got := range map[string]int{"display name": maxNameRunes, "board title": maxBoardTitleRunes, "column title": maxColumnTitleRunes, "card title": maxCardTitleRunes} {
		if got != short {
			t.Errorf("%s is %d characters, its source gives %d", name, got, short)
		}
	}
	if maxPosBytes != kolobraid.MaxViewKey {
		t.Errorf("position bound %d, its source gives %d", maxPosBytes, kolobraid.MaxViewKey)
	}
	// a card is one view value: what is left of MaxViewValue after the title, the position and the fixed fields
	fixed := 4*32 + 2 + 6*10
	if want := (kolobraid.MaxViewValue - kolobraid.MaxViewKey - kolobraid.MaxViewKey - fixed) / utf8.UTFMax; maxCardDescRunes != want {
		t.Errorf("description bound %d, its source gives %d", maxCardDescRunes, want)
	}
	if idSize != 32 {
		t.Errorf("ID size %d: entry IDs and ed25519 keys are 32 bytes", idSize)
	}
	// the default entry body bound is the view value bound, so the largest card entry fits it
	if cfg.MaxBody != kolobraid.MaxViewValue {
		t.Errorf("the default MaxBody is %d, not MaxViewValue: the entry bound must be rechecked", cfg.MaxBody)
	}
	if maxInviteUses != cfg.MaxMembers {
		t.Errorf("invite uses %d, MaxMembers default %d", maxInviteUses, cfg.MaxMembers)
	}
	if defaultMaxMembers != cfg.MaxMembers {
		t.Errorf("defaultMaxMembers %d, library default %d", defaultMaxMembers, cfg.MaxMembers)
	}
	if maxActiveInvites != swarmTopics-2 {
		t.Errorf("active invites %d, swarm topics %d less the board's and a join's", maxActiveInvites, swarmTopics)
	}
	// the largest honest card fits an entry and a view value, and the largest name fits a join request
	most := op{Kind: opCardAdd, Col: oid{1}, Text: strings.Repeat("\U0010FFFF", maxCardTitleRunes), Desc: strings.Repeat("\U0010FFFF", maxCardDescRunes)}
	most.Pos = make([]byte, maxPosBytes)
	for i := range most.Pos {
		most.Pos[i] = 255
	}
	body, err := most.encode()
	if err != nil || len(body) > cfg.MaxBody {
		t.Errorf("the largest card entry: %d bytes (limit %d), %v", len(body), cfg.MaxBody, err)
	}
	if rec := (card{Col: oid{1}, Title: most.Text, Desc: most.Desc, Pos: most.Pos}).encode(); len(rec) > kolobraid.MaxViewValue {
		t.Errorf("the largest card record is %d bytes, the view takes %d", len(rec), kolobraid.MaxViewValue)
	}
	if len(body) < cfg.MaxBody-1024 { // the description uses what the title and position leave: nothing is wasted
		t.Errorf("the largest card entry is %d bytes of %d", len(body), cfg.MaxBody)
	}
	// the notch shows the texts within their own bounds: it cuts nothing
	long := strings.Repeat("\U0010FFFF", maxCardTitleRunes)
	if n := notchOf(Board{State: stateActive, Title: long}, &Change{Card: long, Column: long}, true); n.Title != long || n.Change.Card != long || n.Change.Column != long {
		t.Error("the notch cut a text that its bound allows")
	}
	// the waits have no number of their own: zero waits until the person cancels, as kolo-invite leaves it to the caller
	set, err := Config{DataDir: t.TempDir(), Bootstrap: "127.0.0.1:4000"}.resolve()
	if err != nil {
		t.Fatal(err)
	}
	if set.FindTimeout != 0 || set.JoinTimeout != 0 || set.NotchLinger != 0 || timeoutAfter(0) != nil {
		t.Errorf("a wait has a default: find %v, join %v, notch %v", set.FindTimeout, set.JoinTimeout, set.NotchLinger)
	}
	// the lookup interval is kolo-invite's for a joiner, and a dial is retried in step with it (kolo-swarm docs/limits.md:25)
	if c := swarmConfig(set); c.LookupInterval != 5*time.Second || c.RetryMax != c.LookupInterval {
		t.Errorf("lookup %v, retry %v", c.LookupInterval, c.RetryMax)
	}
	id, _ := koloid.Generate()
	join, err := kolobraid.NewJoin(context.Background(), id, kolobraid.Options{Backend: kolostore.NewMemory(), App: appTag, Apply: apply})
	if err != nil {
		t.Fatal(err)
	}
	if req, err := join.Request(joinData(strings.Repeat("\U0010FFFF", maxNameRunes))); err != nil || len(req) > kolobraid.MaxJoinRequest {
		t.Errorf("the largest name makes a join request of %d bytes (limit %d): %v", len(req), kolobraid.MaxJoinRequest, err)
	}
}

// The swarm and transport limits the app passes are inside the ranges of the libraries that take them: accepted at the
// value, refused one past the range.
func TestSwarmLimitsAreInTheirRanges(t *testing.T) {
	id, err := koloid.Generate()
	if err != nil {
		t.Fatal(err)
	}
	try := func(c *koloswarm.Config) error {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		sw, err := koloswarm.New(ctx, "127.0.0.1:0", id, c)
		if err == nil {
			sw.Close()
		}
		return err
	}
	set, err := Config{DataDir: t.TempDir(), Bootstrap: "127.0.0.1:4000"}.resolve()
	if err != nil {
		t.Fatal(err)
	}
	if err := try(swarmConfig(set)); err != nil {
		t.Fatalf("the app's swarm config: %v", err)
	}
	over := swarmConfig(set)
	over.MaxTopics = swarmTopics + 1
	if err := try(over); !errors.Is(err, koloswarm.ErrInvalidConfig) {
		t.Errorf("one topic past the largest MaxTopics: %v", err)
	}
	over = swarmConfig(set)
	over.RetryMax = time.Hour + 1
	if err := try(over); !errors.Is(err, koloswarm.ErrInvalidConfig) {
		t.Errorf("a retry past the largest RetryMax: %v", err)
	}
	// the per-address limit must not pass the transport's MaxConns: the value is accepted at MaxConns, refused above it
	over = swarmConfig(set)
	over.DHT.Secure.Transport = &kolotransport.Config{MaxConns: defaultMaxMembers, MaxConnsPerIP: defaultMaxMembers}
	if err := try(over); err != nil {
		t.Errorf("the per-address limit at MaxConns: %v", err)
	}
	over.DHT.Secure.Transport = &kolotransport.Config{MaxConns: defaultMaxMembers - 1, MaxConnsPerIP: defaultMaxMembers}
	if err := try(over); err == nil {
		t.Error("a per-address limit above MaxConns was accepted")
	}
}
