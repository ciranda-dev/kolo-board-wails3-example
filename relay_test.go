package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	kolobraid "github.com/ciranda-dev/kolo-braid"
	kolodht "github.com/ciranda-dev/kolo-dht"
	koloid "github.com/ciranda-dev/kolo-id"
	invite "github.com/ciranda-dev/kolo-invite"
	kolorelay "github.com/ciranda-dev/kolo-relay"
	kolosecure "github.com/ciranda-dev/kolo-secure"
	kolostore "github.com/ciranda-dev/kolo-store"
	koloswarm "github.com/ciranda-dev/kolo-swarm"
	kolosync "github.com/ciranda-dev/kolo-sync"
	kolotransport "github.com/ciranda-dev/kolo-transport"
)

// relayEntry is a connection relay in the -relay form at addr, with a fresh key.
func relayEntry(t *testing.T, addr string) (string, ed25519.PublicKey) {
	t.Helper()
	id, err := koloid.Generate()
	if err != nil {
		t.Fatal(err)
	}
	return addr + "@" + id.PublicKeyString(), id.PublicKey()
}

func newID(t *testing.T) *koloid.Identity {
	t.Helper()
	id, err := koloid.Generate()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// wantConfigError checks that err is a ConfigError for setting with the cause want.
func wantConfigError(t *testing.T, name string, err error, want error, setting string) {
	t.Helper()
	var ce *ConfigError
	if !errors.Is(err, ErrConfig) || !errors.Is(err, want) || !errors.As(err, &ce) || ce.Setting != setting {
		t.Errorf("%s: %v, want -%s %v", name, err, setting, want)
	}
}

func TestRelaySettingSources(t *testing.T) {
	a, ka := relayEntry(t, "127.0.0.1:7001")
	b, _ := relayEntry(t, "127.0.0.1:7002")
	c, _ := relayEntry(t, "relay.example.org:7003")
	dir := t.TempDir()
	file := filepath.Join(dir, "relays.txt")
	if err := os.WriteFile(file, []byte("# relays\n"+c+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	resolve := func(args []string, env map[string]string) (settings, error) {
		t.Helper()
		cfg, err := LoadConfig(args, envOf(env), io.Discard)
		if err != nil {
			return settings{}, err
		}
		cfg.DataDir = t.TempDir()
		return cfg.resolve()
	}
	s, err := resolve([]string{"-relay", a, "-relay", b + " , " + a, "-relay-file", file}, map[string]string{"KOLO_BOARD_RELAYS": b})
	if err != nil || s.relaysText != a+","+b+","+c || s.relaysFrom != "flag-and-file" || !s.relays[0].PublicKey.Equal(ka) {
		t.Fatalf("flags: %q %q %v", s.relaysText, s.relaysFrom, err)
	}
	if got := swarmConfig(s).Relays; !slices.EqualFunc(got, s.relays, func(x, y koloswarm.Relay) bool { return x.Addr == y.Addr && x.PublicKey.Equal(y.PublicKey) }) {
		t.Errorf("the swarm gets %v", got)
	}
	for _, tc := range []struct {
		name       string
		args       []string
		env        string
		text, from string
	}{
		{"file alone, over the variable", []string{"-relay-file", file}, a, c, "file"},
		{"flag alone", []string{"-relay", a}, b, a, "flag"},
		{"variable", nil, b + ", ,", b, "env"},
		{"none", nil, "", "", "none"},
		{"empty flag keeps the variable out", []string{"-relay", ""}, b, "", "none"},
		{"only commas", nil, " , ,", "", "none"},
	} {
		s, err := resolve(tc.args, map[string]string{"KOLO_BOARD_RELAYS": tc.env})
		if err != nil || s.relaysText != tc.text || s.relaysFrom != tc.from {
			t.Errorf("%s: %q %q %v", tc.name, s.relaysText, s.relaysFrom, err)
		}
	}
	if s, err = (Config{DataDir: t.TempDir(), Relays: []string{a}}).resolve(); err != nil || s.relaysText != a || s.relaysFrom != "flag" {
		t.Errorf("set in code: %q %q %v", s.relaysText, s.relaysFrom, err)
	}

	var seventeen []string
	for i := range 17 {
		e, _ := relayEntry(t, fmt.Sprintf("127.0.0.1:%d", 8000+i))
		seventeen = append(seventeen, e)
	}
	if s, err = resolve([]string{"-relay", strings.Join(seventeen[:16], ",")}, nil); err != nil || len(s.relays) != 16 {
		t.Errorf("16 relays: %d %v", len(s.relays), err)
	}
	key := hex.EncodeToString(ka)
	other, _ := relayEntry(t, "127.0.0.1:7001")
	zero := filepath.Join(dir, "zero")
	if err := os.Symlink("/dev/zero", zero); err != nil {
		t.Fatal(err)
	}
	big := filepath.Join(dir, "big.txt")
	if err := os.WriteFile(big, bytes.Repeat([]byte("#"), 64<<10+1), 0o600); err != nil {
		t.Fatal(err)
	}
	clash := filepath.Join(dir, "clash.txt")
	if err := os.WriteFile(clash, []byte(strings.Replace(a, "7001", "7009", 1)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, setting string
		args          []string
	}{
		{"17 relays", "relay", []string{"-relay", strings.Join(seventeen, ",")}},
		{"17 with a file", "relay-file", []string{"-relay", strings.Join(seventeen[:16], ","), "-relay-file", file}},
		{"one key, two addresses", "relay", []string{"-relay", a, "-relay", strings.Replace(a, "7001", "7009", 1)}},
		{"one address, two keys", "relay", []string{"-relay", a + "," + other}},
		{"a flag and a file clash", "relay-file", []string{"-relay", a, "-relay-file", clash}},
		{"IPv6", "relay", []string{"-relay", "[::1]:7001@" + key}},
		{"IPv4-mapped IPv6", "relay", []string{"-relay", "[::ffff:127.0.0.1]:7001@" + key}},
		{"bare IPv6", "relay", []string{"-relay", "::1:7001@" + key}},
		{"port 0", "relay", []string{"-relay", "127.0.0.1:0@" + key}},
		{"port 70000", "relay", []string{"-relay", "127.0.0.1:70000@" + key}},
		{"short key", "relay", []string{"-relay", "127.0.0.1:7001@abcd"}},
		{"long key", "relay", []string{"-relay", "127.0.0.1:7001@" + key + "00"}},
		{"key not hex", "relay", []string{"-relay", "127.0.0.1:7001@" + strings.Repeat("zz", 32)}},
		{"no key", "relay", []string{"-relay", "127.0.0.1:7001"}},
		{"symlink to a device", "relay-file", []string{"-relay-file", zero}},
		{"directory", "relay-file", []string{"-relay-file", dir}},
		{"file over 64 KiB", "relay-file", []string{"-relay-file", big}},
	} {
		_, err := resolve(tc.args, nil)
		wantConfigError(t, tc.name, err, ErrMalformed, tc.setting)
	}
	_, err = resolve([]string{"-relay-file", filepath.Join(dir, "none")}, nil)
	wantConfigError(t, "missing file", err, fs.ErrNotExist, "relay-file")
	if !errors.Is(err, ErrUnreadable) || errors.Is(err, ErrMalformed) || !strings.Contains(configError(err).Error(), "The setting -relay-file cannot be read") {
		t.Errorf("a missing file is not a malformed one: %v", configError(err))
	}
	_, err = resolve(nil, map[string]string{"KOLO_BOARD_RELAYS": "nope"})
	wantConfigError(t, "bad variable", err, ErrMalformed, "relay")
	if err == nil || !strings.Contains(err.Error(), "KOLO_BOARD_RELAYS: ") {
		t.Errorf("a bad variable does not say where it came from: %v", err)
	}
}

func TestHelpNamesTheRelayFlags(t *testing.T) {
	var out bytes.Buffer
	LoadConfig([]string{"-h"}, envOf(nil), &out)
	for _, want := range []string{"-relay ", "-relay-file", "KOLO_BOARD_RELAYS", "host:port@key"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the help does not name %q:\n%s", want, out.String())
		}
	}
}

// Without relays the board is what it was: no relay in the swarm, none in the window, and never a null list.
func TestNoRelaysChangesNothing(t *testing.T) {
	p := startPeer(t, "Ana", Config{}, nil)
	if s := p.Settings(); s.ConnectionRelays != "" || s.ConnectionRelaysFrom != "none" {
		t.Errorf("%+v", s)
	}
	if c := swarmConfig(p.settingsCopy()); c.Relays != nil {
		t.Errorf("relays %v", c.Relays)
	}
	b, err := json.Marshal(p.Info().Network)
	if err != nil || !strings.Contains(string(b), `"relays":[]`) {
		t.Errorf("%s %v", b, err)
	}
}

// syncBuf is a log output that the race detector accepts from many goroutines.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

func logTo(t *testing.T) *syncBuf {
	t.Helper()
	buf := &syncBuf{}
	log.SetOutput(buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return buf
}

// relayService is a service with one listed relay that is never started, for the relay state alone.
func relayService(x koloswarm.Relay, emit func(string, any)) *BoardService {
	s := &BoardService{emit: emit}
	s.set.relays = []koloswarm.Relay{x}
	return s
}

func TestRelayChanged(t *testing.T) {
	x := koloswarm.Relay{Addr: "127.0.0.1:1", PublicKey: bytes.Repeat([]byte{7}, 32)}
	s := relayService(x, func(string, any) {})
	logTo(t)
	for _, tc := range []struct {
		err          error
		state, cause string
	}{
		{nil, relayReady, ""},
		{koloswarm.ErrClosed, relayWaiting, ""},
		{kolosecure.ErrUnexpectedPeer, relayFailed, "relay-wrong-key"},
		{koloswarm.ErrInvalidConfig, relayFailed, "relay-bad-key"},
		{kolorelay.ErrNotAllowed, relayFailed, "relay-not-allowed"},
		{kolorelay.ErrLimit, relayFailed, "relay-full"},
		{koloswarm.ErrLost, relayFailed, "relay-unreachable"},
	} {
		s.relayChanged(fromSwarm, koloswarm.Event{Kind: koloswarm.RelayChanged, PublicKey: x.PublicKey, Err: tc.err})
		st := s.relayStatuses()[0]
		reason := ""
		if st.Error != nil {
			reason = st.Error.Reason
			if st.Error.Detail != x.Addr || !strings.Contains(st.Error.Message, x.Addr) {
				t.Errorf("%v: the sentence does not name the relay: %+v", tc.err, st.Error)
			}
		}
		if st.State != tc.state || reason != tc.cause || st.Addr != x.Addr || st.Key != hex.EncodeToString(x.PublicKey) {
			t.Errorf("%v: %+v", tc.err, st)
		}
	}
	s.relayChanged(fromSwarm, koloswarm.Event{Kind: koloswarm.RelayChanged, PublicKey: x.PublicKey})
	s.relayChanged(fromSwarm, koloswarm.Event{Kind: koloswarm.RelayChanged, PublicKey: bytes.Repeat([]byte{8}, 32), Err: koloswarm.ErrLost})
	if st := s.relayStatuses(); len(st) != 1 || st[0].State != relayReady {
		t.Errorf("an unlisted relay changed the list: %+v", st)
	}
	// the check's wrong key wins over a place the swarm holds
	s.relayChanged(fromCheck, koloswarm.Event{Kind: koloswarm.RelayChanged, PublicKey: x.PublicKey, Err: kolosecure.ErrUnexpectedPeer})
	if st := s.relayStatuses()[0]; st.State != relayFailed || st.Error.Reason != "relay-wrong-key" {
		t.Errorf("worst state: %+v", st)
	}
}

// The end of a place is routine: kolo-swarm takes a new one. It shows nothing and logs nothing; a real failure is
// logged once.
func TestRelayPlaceRenewalIsQuiet(t *testing.T) {
	x := koloswarm.Relay{Addr: "127.0.0.1:1", PublicKey: bytes.Repeat([]byte{7}, 32)}
	events := 0
	s := relayService(x, func(name string, _ any) {
		if name == "board:network" {
			events++
		}
	})
	logged := logTo(t)
	held := koloswarm.Event{Kind: koloswarm.RelayChanged, PublicKey: x.PublicKey}
	ended := koloswarm.Event{Kind: koloswarm.RelayChanged, PublicKey: x.PublicKey, Err: kolorelay.ErrReservationEnded}
	for _, ev := range []koloswarm.Event{held, ended, held, ended, held} {
		s.relayChanged(fromSwarm, ev)
	}
	if st := s.relayStatuses(); events != 1 || logged.String() != "" || st[0].State != relayReady {
		t.Fatalf("%d events, log %q, %+v", events, logged, st)
	}
	wrong := koloswarm.Event{Kind: koloswarm.RelayChanged, PublicKey: x.PublicKey, Err: kolosecure.ErrUnexpectedPeer}
	for _, ev := range []koloswarm.Event{wrong, wrong, held, wrong} {
		s.relayChanged(fromSwarm, ev)
	}
	if n := strings.Count(logged.String(), "connection relay 127.0.0.1:1: "); n != 1 || events != 4 {
		t.Errorf("%d events, %d log lines: %q", events, n, logged)
	}
}

// Statuses come from the swarm and from one check per relay at once. Each event is built and sent in one step with a
// growing Seq, so the window, which keeps the highest, ends on the newest states.
func TestRelayEventsArriveInOrder(t *testing.T) {
	var relays []koloswarm.Relay
	for i := range 16 {
		relays = append(relays, koloswarm.Relay{Addr: fmt.Sprintf("127.0.0.1:%d", 1000+i), PublicKey: bytes.Repeat([]byte{byte(i + 1)}, 32)})
	}
	var mu sync.Mutex
	var sent []Network
	s := &BoardService{emit: func(_ string, data any) {
		if n, ok := data.(Network); ok {
			time.Sleep(time.Duration(n.Seq%3) * time.Millisecond) // a slow window: events sent at once would pass each other
			mu.Lock()
			sent = append(sent, n)
			mu.Unlock()
		}
	}}
	s.set.relays = relays
	logTo(t)
	var wg sync.WaitGroup
	for _, r := range relays {
		wg.Go(func() {
			for _, err := range []error{nil, kolosecure.ErrUnexpectedPeer} {
				s.relayChanged(fromCheck, koloswarm.Event{Kind: koloswarm.RelayChanged, PublicKey: r.PublicKey, Err: err})
			}
		})
	}
	wg.Wait()
	for i := 1; i < len(sent); i++ {
		if sent[i].Seq <= sent[i-1].Seq {
			t.Fatalf("event %d has seq %d after %d", i, sent[i].Seq, sent[i-1].Seq)
		}
	}
	for _, st := range sent[len(sent)-1].Relays {
		if st.State != relayFailed {
			t.Fatalf("the newest event hides a wrong key: %+v", st)
		}
	}
}

// loopSecure raises kolo-transport's per-address limits: every client of a test is 127.0.0.1.
func loopSecure() *kolosecure.Config {
	return &kolosecure.Config{Transport: &kolotransport.Config{MaxConns: 256, MaxConnsPerIP: 256, MaxDialed: 256}}
}

// startConnRelay runs a kolo-relay on 127.0.0.1 and returns it as a -relay entry.
func startConnRelay(t *testing.T) (entry, addr string, key ed25519.PublicKey) {
	t.Helper()
	id := newID(t)
	srv, err := kolorelay.Listen(context.Background(), "127.0.0.1:0", id, &kolorelay.ServerConfig{Secure: loopSecure()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })
	addr = srv.Addr().String()
	return addr + "@" + id.PublicKeyString(), addr, id.PublicKey()
}

// downConnRelay is a -relay entry whose address takes packets and never answers.
func downConnRelay(t *testing.T) string {
	t.Helper()
	pc, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 2048)
		for {
			if _, _, err := pc.ReadFrom(buf); err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() {
		pc.Close()
		<-done
	})
	e, _ := relayEntry(t, pc.LocalAddr().String())
	return e
}

// hostileRelay holds the relay's key and closes every connection right after the handshake.
func hostileRelay(t *testing.T) koloswarm.Relay {
	t.Helper()
	id := newID(t)
	ctx, cancel := context.WithCancel(context.Background())
	ln, err := kolosecure.Listen(ctx, "127.0.0.1:0", id, loopSecure())
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Go(func() {
		for {
			c, err := ln.Accept(ctx)
			if err != nil {
				return
			}
			c.CloseWithError(1, "go away")
		}
	})
	t.Cleanup(func() {
		cancel()
		ln.Close()
		wg.Wait()
	})
	return koloswarm.Relay{Addr: ln.Addr().String(), PublicKey: id.PublicKey()}
}

// Only a wrong key is reported: a check that ends for any other reason, such as the network stopping while it runs, or a
// relay that holds its key and then refuses everything, leaves the relay as it was.
func TestRelayCheckReportsOnlyAWrongKey(t *testing.T) {
	logTo(t)
	down := koloswarm.Relay{Addr: "127.0.0.1:1", PublicKey: bytes.Repeat([]byte{7}, 32)}
	s := relayService(down, func(string, any) {})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.checkRelays(ctx)
	s.netWG.Wait()
	if st := s.relayStatuses(); st[0].State != relayWaiting {
		t.Errorf("a stopped check reported %+v", st[0])
	}
	_, addr, key := startConnRelay(t)
	for name, r := range map[string]koloswarm.Relay{"hostile": hostileRelay(t), "real": {Addr: addr, PublicKey: key}} {
		s := relayService(r, func(string, any) {})
		s.checkRelays(context.Background())
		s.netWG.Wait()
		if st := s.relayStatuses(); st[0].State != relayWaiting {
			t.Errorf("a %s relay with the right key reported %+v", name, st[0])
		}
	}
}

// A person without a board holds no place at a relay, so kolo-swarm never tells them of a wrong key: the check does,
// within seconds of the start, in the window and once in the terminal.
func TestWrongRelayKeyIsReportedAtStart(t *testing.T) {
	logged := logTo(t)
	_, addr, _ := startConnRelay(t)
	wrong, _ := relayEntry(t, addr)
	start := time.Now()
	p := startPeer(t, "Bia", Config{First: freeUDP(t), Relays: []string{wrong}}, nil)
	until(t, "the wrong key reported", func() bool {
		n := p.Info().Network.Relays
		return len(n) == 1 && n[0].State == relayFailed && n[0].Error.Reason == "relay-wrong-key" && n[0].Error.Detail == addr
	})
	if d := time.Since(start); d > 15*time.Second {
		t.Errorf("reported after %v", d)
	}
	if n := strings.Count(logged.String(), "connection relay "+addr+": "); n != 1 {
		t.Errorf("%d log lines: %q", n, logged)
	}
}

// A relay that never answers holds its check for up to relayCheckTimeout, but a Reconnect or a stop of the network ends
// it at once: stopNetwork waits for every check.
func TestRelayCheckEndsWithTheNetwork(t *testing.T) {
	p := startPeer(t, "Bia", Config{First: freeUDP(t), Relays: []string{downConnRelay(t)}}, nil)
	p.rec.waitNetwork(t, netReady)
	start := time.Now()
	if err := p.Reconnect(); err != nil {
		t.Fatal(err)
	}
	p.stopNetwork()
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("a Reconnect and a stop waited %v for a relay that never answers", d)
	}
}

// Each network has its own relay states: what the stopped one said is gone after a Reconnect.
func TestReconnectForgetsTheOldRelayStates(t *testing.T) {
	p := startPeer(t, "Bia", Config{Relays: []string{downConnRelay(t)}}, nil)
	key := p.settingsCopy().relays[0].PublicKey
	p.relayChanged(fromSwarm, koloswarm.Event{Kind: koloswarm.RelayChanged, PublicKey: key})
	if st := p.Info().Network.Relays[0]; st.State != relayReady {
		t.Fatalf("%+v", st)
	}
	if err := p.Reconnect(); err != nil {
		t.Fatal(err)
	}
	if st := p.Info().Network.Relays[0]; st.State != relayWaiting {
		t.Errorf("the old network's place is still shown: %+v", st)
	}
}

// A join whose inviter is gone, with relays listed, says that the relays failed too.
func TestJoinThroughRelaysFailsWithItsReason(t *testing.T) {
	entry, _, _ := startConnRelay(t)
	ps := network(t, "Entry", "Ana")
	a := ps[1]
	if _, err := a.CreateBoard("Launch", nil); err != nil {
		t.Fatal(err)
	}
	info, err := a.CreateInvite(mainCtx, InviteOptions{TTLSeconds: 3600})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.ServiceShutdown(); err != nil {
		t.Fatal(err)
	}
	b := startPeer(t, "Bia", Config{Bootstrap: a.cfg.Bootstrap, FindTimeout: 2 * time.Second, Relays: []string{entry}}, nil)
	b.rec.waitNetwork(t, netReady)
	if _, err := b.JoinBoard(mainCtx, info.Code); err != nil {
		t.Fatal(err)
	}
	until(t, "not found through the relays", func() bool { return endedReason(b) == "inviter-not-found-relays" })
	st, _ := b.rec.lastState()
	if !strings.Contains(st.Error.Message, "through the connection relays") {
		t.Errorf("%q", st.Error.Message)
	}
}

// tapRelay stands where a relay operator stands. The boards list it as their relay: it holds the relay key they check,
// and passes every stream on to a real kolo-relay as the same board, so it keeps every byte a relay operator can read. It
// stands in only for the identities it is given; any other key is closed after the handshake.
type tapRelay struct {
	ids   map[string]*koloid.Identity
	key   ed25519.PublicKey
	entry string
	to    string
	toKey ed25519.PublicKey
	wg    sync.WaitGroup
	mu    sync.Mutex
	seen  []byte
}

func startTapRelay(t *testing.T, ids ...*koloid.Identity) *tapRelay {
	t.Helper()
	_, to, toKey := startConnRelay(t)
	id := newID(t)
	ctx, cancel := context.WithCancel(context.Background())
	ln, err := kolosecure.Listen(ctx, "127.0.0.1:0", id, loopSecure())
	if err != nil {
		t.Fatal(err)
	}
	tap := &tapRelay{ids: map[string]*koloid.Identity{}, key: id.PublicKey(), to: to, toKey: toKey,
		entry: ln.Addr().String() + "@" + id.PublicKeyString()}
	for _, x := range ids {
		tap.ids[string(x.PublicKey())] = x
	}
	tap.wg.Go(func() {
		for {
			c, err := ln.Accept(ctx)
			if err != nil {
				return
			}
			tap.wg.Go(func() { tap.serve(ctx, c) })
		}
	})
	t.Cleanup(func() {
		cancel()
		ln.Close()
		tap.wg.Wait()
	})
	return tap
}

func (tap *tapRelay) serve(ctx context.Context, c *kolosecure.Conn) {
	defer c.Close()
	id := tap.ids[string(c.RemotePublicKey())]
	if id == nil {
		return
	}
	rc, err := kolosecure.Dial(ctx, tap.to, id, tap.toKey, &kolosecure.Config{Transport: &kolotransport.Config{KeepAlivePeriod: 5 * time.Second}})
	if err != nil {
		return
	}
	defer rc.Close()
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	context.AfterFunc(c.Context(), cancel)
	context.AfterFunc(rc.Context(), cancel)
	var wg sync.WaitGroup
	wg.Go(func() { tap.join(cctx, &wg, c, rc) })
	wg.Go(func() { tap.join(cctx, &wg, rc, c) })
	<-cctx.Done()
	c.Close()
	rc.Close()
	wg.Wait()
}

// join passes every stream that from opens to a new stream of to, both ways.
func (tap *tapRelay) join(ctx context.Context, wg *sync.WaitGroup, from, to *kolosecure.Conn) {
	for {
		a, err := from.AcceptStream(ctx)
		if err != nil {
			return
		}
		b, err := to.OpenStream(ctx)
		if err != nil {
			a.CancelRead(0)
			a.CancelWrite(0)
			return
		}
		wg.Go(func() { tap.pipe(b, a) })
		wg.Go(func() { tap.pipe(a, b) })
	}
}

func (tap *tapRelay) pipe(dst, src *kolotransport.Stream) {
	buf := make([]byte, 32<<10)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			tap.mu.Lock()
			tap.seen = append(tap.seen, buf[:n]...)
			tap.mu.Unlock()
			if _, werr := dst.Write(buf[:n]); werr != nil {
				src.CancelRead(0)
				return
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				dst.Close()
			} else {
				dst.CancelWrite(0)
			}
			return
		}
	}
}

func (tap *tapRelay) captured() []byte {
	tap.mu.Lock()
	defer tap.mu.Unlock()
	return bytes.Clone(tap.seen)
}

// refuseAll is the accept hook of a board behind a strict NAT: no direct connection comes in. A relayed one still
// works, since the board dials the relay.
func refuseAll(netip.AddrPort) bool { return false }

// startWalled starts a board that accepts no direct connection.
func startWalled(t *testing.T, name string, cfg Config, id *koloid.Identity) *peer {
	t.Helper()
	p := startPeerWith(t, name, cfg, func(s *BoardService) { s.accept = refuseAll }, id)
	p.rec.waitNetwork(t, netReady)
	return p
}

// walledSwarm is a swarm of the test's own behind the same wall, with the relay list given.
func walledSwarm(t *testing.T, ctx context.Context, id *koloid.Identity, entry, relays string) *koloswarm.Swarm {
	t.Helper()
	rs, err := koloswarm.ParseRelays(relays)
	if err != nil {
		t.Fatal(err)
	}
	dht := &kolodht.Config{Secure: &kolosecure.Config{Transport: &kolotransport.Config{AcceptFunc: refuseAll}}}
	sw, err := koloswarm.New(ctx, "127.0.0.1:0", id, &koloswarm.Config{DHT: dht, Relays: rs, LookupInterval: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sw.Close() })
	if err := sw.Bootstrap(ctx, []netip.AddrPort{netip.MustParseAddrPort(entry)}); err != nil {
		t.Fatal(err)
	}
	return sw
}

// connectedTo waits for the swarm's connection to key.
func connectedTo(t *testing.T, ctx context.Context, sw *koloswarm.Swarm, key ed25519.PublicKey) koloswarm.Conn {
	t.Helper()
	for {
		ev, err := sw.Next(ctx)
		if err != nil {
			t.Fatalf("no connection to %x: %v", key[:4], err)
		}
		if ev.Kind == koloswarm.Connected && ev.Peer.PublicKey().Equal(key) {
			return ev.Peer.Conn()
		}
	}
}

// relayOf is the key of the relay that carries p's board connection to other; nil when it is direct or missing.
func relayOf(p, other *peer) ed25519.PublicKey {
	b := p.BoardService.board()
	if b == nil {
		return nil
	}
	b.mu.Lock()
	q := b.peers[string(other.id.PublicKey())]
	b.mu.Unlock()
	if q == nil {
		return nil
	}
	if rc, ok := q.Conn().(*kolorelay.Conn); ok {
		return rc.RelayPublicKey()
	}
	return nil
}

func relayedBy(t *testing.T, p, other *peer, relay ed25519.PublicKey) {
	t.Helper()
	until(t, p.displayName()+"'s board connection through the relay", func() bool { return relayOf(p, other).Equal(relay) })
}

// relayStates is "state/reason" of each relay of p, as the window shows them.
func relayStates(p *peer) []string {
	var out []string
	for _, st := range p.Info().Network.Relays {
		why := ""
		if st.Error != nil {
			why = st.Error.Reason
		}
		out = append(out, st.State+"/"+why)
	}
	return out
}

// storedKeys is every 32-byte value of a board's store but the members' keys: the entry keys among them.
func storedKeys(t *testing.T, p *peer, members ...ed25519.PublicKey) [][]byte {
	t.Helper()
	b := p.BoardService.board()
	if b == nil || b.db == nil {
		t.Fatal("no board store")
	}
	var out [][]byte
	err := b.db.Scan(nil, nil, false, 0, func(_, v []byte) bool {
		if len(v) == 32 && !slices.ContainsFunc(members, func(k ed25519.PublicKey) bool { return bytes.Equal(k, v) }) {
			out = append(out, bytes.Clone(v))
		}
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// inviteSecret is the secret part of an invite code: it never travels.
func inviteSecret(t *testing.T, code string) []byte {
	t.Helper()
	b, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(strings.TrimPrefix(code, invite.Prefix)))
	if err != nil || len(b) != 52 {
		t.Fatalf("invite code: %v", err)
	}
	return b[16:48]
}

func randomMark(t *testing.T) string {
	t.Helper()
	var b [8]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// Boards that cannot connect directly meet through the relay they all list: the invite and the join, a card each way,
// settled on both, and a removal that holds: a card made after it reaches the member who stays, through the relay, and
// never the removed one. The relay operator's view holds the members' keys and none of the card text, the board name,
// the group ID, the invite or an entry key.
func TestBoardThroughRelay(t *testing.T) {
	ida, idb, idc := newID(t), newID(t), newID(t)
	tap := startTapRelay(t, ida, idb, idc)
	entry := startPeer(t, "", Config{First: freeUDP(t)}, nil) // the entry node only, reachable directly
	entry.rec.waitNetwork(t, netReady)
	cfg := Config{Bootstrap: entry.cfg.First, Relays: []string{tap.entry}}
	a := startWalled(t, "Ana", cfg, ida)
	b := startWalled(t, "Bia", cfg, idb)
	c := startWalled(t, "Caio", cfg, idc)
	if st := relayStates(b); !slices.Equal(st, []string{"waiting/"}) {
		t.Fatalf("a person without a board holds no place, and the key is right: %v", st)
	}

	mark := randomMark(t)
	title := "Plan " + mark
	if _, err := a.CreateBoard(title, nil); err != nil {
		t.Fatal(err)
	}
	until(t, "Ana's place at the relay", func() bool { return slices.Equal(relayStates(a), []string{"ready/"}) })
	todo := a.board(t).column("To do").ID
	fromAna := "from Ana " + mark
	if _, err := a.AddCard(todo, CardInput{Title: fromAna, Description: "text " + mark}, 0); err != nil {
		t.Fatal(err)
	}
	info, err := a.CreateInvite(mainCtx, InviteOptions{TTLSeconds: 3600, Uses: 2})
	if err != nil {
		t.Fatal(err)
	}
	b.join(t, info.Code)
	relayedBy(t, a, b, tap.key)
	relayedBy(t, b, a, tap.key)
	c.join(t, info.Code)
	relayedBy(t, a, c, tap.key)

	fromBia := "from Bia " + mark
	if _, err := b.AddCard(todo, CardInput{Title: fromBia}, 0); err != nil {
		t.Fatal(err)
	}
	for _, p := range []*peer{a, b} {
		p.waitBoard(t, p.displayName()+" has both cards settled", func(x Board) bool {
			ka, _ := x.card(fromAna)
			kb, _ := x.card(fromBia)
			return x.Title == title && ka != nil && kb != nil && !ka.Syncing && !kb.Syncing
		})
	}

	c.waitBoard(t, "Caio has both cards", func(x Board) bool { k, _ := x.card(fromBia); return k != nil })
	if err := a.RemoveMember(c.Info().KeyID); err != nil {
		t.Fatal(err)
	}
	// a removed settler is served until a settler config without it is active (kolo-braid docs/guide.md, Membership)
	// Caio wrote nothing, so once removed he is not listed at all
	a.waitBoard(t, "removed Caio", func(x Board) bool { q := x.person("Caio"); return (q == nil || !q.Member) && x.Sync.Seats == 2 })
	removed := "from the removed " + mark
	c.AddCard(todo, CardInput{Title: removed}, 0) // refused when Caio was told, void when not
	after := "after the removal " + mark
	if _, err := a.AddCard(todo, CardInput{Title: after}, 0); err != nil {
		t.Fatal(err)
	}
	// the control: the card travels through the relay to the member who stays, so a quiet removed member is not a dead link
	b.waitBoard(t, "Bia has the card made after the removal, settled", func(x Board) bool { k, _ := x.card(after); return k != nil && !k.Syncing })
	relayedBy(t, b, a, tap.key)
	if k, _ := c.board(t).card(after); k != nil {
		t.Error("a removed member got a card made after the removal")
	}
	for _, p := range []*peer{a, b} {
		x := p.board(t)
		if k, _ := x.card(removed); k != nil {
			t.Errorf("%s shows a card of the removed member", p.displayName())
		}
		if q := x.person("Caio"); q != nil && q.Member {
			t.Errorf("%s sees Caio as a member again: %+v", p.displayName(), q)
		}
	}

	gid := a.BoardService.board().braid.Load().GroupID()
	seen := tap.captured()
	if len(seen) < 4096 {
		t.Fatalf("the relay passed only %d bytes", len(seen))
	}
	if !bytes.Contains(seen, ida.PublicKey()) || !bytes.Contains(seen, idb.PublicKey()) || !bytes.Contains(seen, idc.PublicKey()) { // what the relay legitimately sees
		t.Fatal("the tap did not see the members' keys: it does not read what a relay operator reads")
	}
	code, err := invite.Parse(info.Code)
	if err != nil {
		t.Fatal(err)
	}
	id := code.ID()
	secrets := map[string][]byte{"the card text": []byte(mark), "the board name": []byte(title), "the invite": []byte(info.Code),
		"the invite secret": inviteSecret(t, info.Code), "the invite ID": id[:], "the group ID": gid[:]}
	keys := storedKeys(t, a, ida.PublicKey(), idb.PublicKey(), idc.PublicKey())
	if len(keys) < 5 {
		t.Fatalf("only %d entry keys to look for", len(keys))
	}
	for i, k := range keys {
		secrets[fmt.Sprintf("stored key %d", i)] = k
	}
	for what, s := range secrets {
		if bytes.Contains(seen, s) {
			t.Errorf("the relay saw %s", what)
		}
	}
	t.Logf("the relay saw %d bytes and none of %d secrets", len(seen), len(secrets))
}

// A stranger reaches a member through the relay, as anyone on the board's topic may. It is the founder of a board of its
// own, so its key and its braid are valid; only it is not a member of this board, and it gets nothing. The members go
// on through the relay.
func TestStrangerThroughRelayIsRefused(t *testing.T) {
	relay, _, rkey := startConnRelay(t)
	entry := startPeer(t, "", Config{First: freeUDP(t)}, nil)
	entry.rec.waitNetwork(t, netReady)
	cfg := Config{Bootstrap: entry.cfg.First, Relays: []string{relay}}
	a := startWalled(t, "Ana", cfg, nil)
	b := startWalled(t, "Bia", cfg, nil)
	if _, err := a.CreateBoard("Launch", nil); err != nil {
		t.Fatal(err)
	}
	info, err := a.CreateInvite(mainCtx, InviteOptions{TTLSeconds: 3600})
	if err != nil {
		t.Fatal(err)
	}
	b.join(t, info.Code)
	relayedBy(t, a, b, rkey)

	stranger := newID(t)
	ctx := testContext(t)
	own, err := kolobraid.Create(ctx, stranger, kolobraid.GroupConfig{App: appTag}, testOptions(kolostore.NewMemory()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { own.Close() })
	sw := walledSwarm(t, ctx, stranger, entry.cfg.First, relay)
	if err := sw.Join(ctx, a.BoardService.board().topic, &koloswarm.JoinOptions{Lookup: true}); err != nil {
		t.Fatal(err)
	}
	conn := connectedTo(t, ctx, sw, a.id.PublicKey())
	if rc, ok := conn.(*kolorelay.Conn); !ok || !rc.RelayPublicKey().Equal(rkey) {
		t.Fatalf("the stranger is not relayed: %T", conn)
	}
	var strand kolobraid.Entry
	for o, err := range a.BoardService.board().braid.Load().Entries(0) {
		if err == nil && o.Writer.Equal(a.id.PublicKey()) {
			strand = o.Entry
			break
		}
	}
	fctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if _, err := kolosync.FetchHead(fctx, conn, a.id.PublicKey(), strand.Strand); err == nil {
		t.Fatal("the stranger pulled Ana's strand through the relay")
	}
	if x := a.board(t); len(x.People) != 2 {
		t.Errorf("people %+v", x.People)
	}
	todo := b.board(t).column("To do").ID
	if _, err := b.AddCard(todo, CardInput{Title: "still here"}, 0); err != nil {
		t.Fatal(err)
	}
	a.waitBoard(t, "had Bia's card", func(x Board) bool { k, _ := x.card("still here"); return k != nil })
	if !relayOf(a, b).Equal(rkey) {
		t.Error("Ana and Bia are no longer relayed")
	}
}

// A joiner takes only the inviter's connection for its join. Another key on the invite's topic reaches it through the
// relay with a valid handshake, but the invite names the inviter's key: the joiner sends it nothing and keeps looking,
// and joins once the inviter is back.
func TestJoinTakesOnlyTheInvitersKeyThroughRelay(t *testing.T) {
	relay, _, rkey := startConnRelay(t)
	entry := startPeer(t, "", Config{First: freeUDP(t)}, nil)
	entry.rec.waitNetwork(t, netReady)
	cfg := Config{Bootstrap: entry.cfg.First, Relays: []string{relay}}
	ida := newID(t)
	a := startWalled(t, "Ana", cfg, ida)
	if _, err := a.CreateBoard("Launch", nil); err != nil {
		t.Fatal(err)
	}
	info, err := a.CreateInvite(mainCtx, InviteOptions{TTLSeconds: 3600})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.ServiceShutdown(); err != nil {
		t.Fatal(err)
	}
	code, err := invite.Parse(info.Code)
	if err != nil {
		t.Fatal(err)
	}

	ctx := testContext(t)
	other := newID(t)
	sw := walledSwarm(t, ctx, other, entry.cfg.First, relay)
	if err := sw.Join(ctx, code.Topic(), &koloswarm.JoinOptions{Announce: true, Lookup: true}); err != nil {
		t.Fatal(err)
	}
	jcfg := cfg
	jcfg.FindTimeout = time.Hour
	j := startWalled(t, "Jo", jcfg, nil)
	if _, err := j.JoinBoard(mainCtx, info.Code); err != nil {
		t.Fatal(err)
	}
	conn := connectedTo(t, ctx, sw, j.id.PublicKey())
	if rc, ok := conn.(*kolorelay.Conn); !ok || !rc.RelayPublicKey().Equal(rkey) {
		t.Fatalf("not relayed: %T", conn)
	}
	sctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if _, err := conn.AcceptStream(sctx); err == nil {
		t.Fatal("the joiner sent its request to a key the invite does not name")
	}
	if x := j.board(t); x.State != stateJoining || x.Phase != phaseFinding {
		t.Fatalf("the joiner stopped looking: %s/%s", x.State, x.Phase)
	}

	a = startWalled(t, "Ana", Config{Bootstrap: cfg.Bootstrap, Relays: cfg.Relays, DataDir: a.data}, ida)
	j.waitBoard(t, "joined through the relay", func(x Board) bool { return x.State == stateActive && x.person("Ana") != nil })
	relayedBy(t, j, a, rkey)
}
