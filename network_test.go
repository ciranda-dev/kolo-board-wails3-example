package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/netip"
	"path/filepath"
	"strings"
	"testing"

	kolodht "github.com/ciranda-dev/kolo-dht"
	koloid "github.com/ciranda-dev/kolo-id"
	koloswarm "github.com/ciranda-dev/kolo-swarm"
)

// held is a bootstrap that sends nothing and waits for the network to stop: the service stays at "starting".
func held(ctx context.Context, _ *koloswarm.Swarm) error {
	<-ctx.Done()
	return ctx.Err()
}

// publicService starts a service in the public network with a bootstrap that sends nothing, so no test sends a
// packet to a public address.
func publicService(t *testing.T, boot func(context.Context, *koloswarm.Swarm) error) (*BoardService, *recorder) {
	t.Helper()
	id, err := koloid.Generate()
	if err != nil {
		t.Fatal(err)
	}
	rec := &recorder{}
	s := NewBoardService(Config{DataDir: filepath.Join(t.TempDir(), "data"), Listen: "127.0.0.1:0"}, rec.emit)
	s.identity, s.bootstrap = id, boot
	if _, err := s.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.ServiceShutdown() })
	return s, rec
}

// With nothing configured the app is on the public network: no entry node of its own, so kolo-swarm uses the public ones.
func TestDefaultIsThePublicNetwork(t *testing.T) {
	s, err := Config{DataDir: t.TempDir()}.resolve()
	if err != nil {
		t.Fatal(err)
	}
	public := kolodht.PublicServers()
	if s.mode != modePublic || s.nodesFrom != "public" || s.nodes != nil || s.first.IsValid() || s.local.IsValid() || s.Listen != publicListen {
		t.Fatalf("%+v", s)
	}
	if s.entryNodeCount() != len(public) || len(public) == 0 || s.nodesText != "" {
		t.Errorf("%d entry nodes, public %v, text %q", s.entryNodeCount(), public, s.nodesText)
	}
	// nil, not an empty list: kolo-swarm bootstraps from the public servers on nil and refuses an empty list
	if c := swarmConfig(s); c.BootstrapNodes != nil {
		t.Errorf("BootstrapNodes %#v", c.BootstrapNodes)
	}
}

func TestPublicNetworkIsWhatTheWindowSees(t *testing.T) {
	s, _ := publicService(t, held)
	n, st := s.Info().Network, s.Settings()
	if n.Mode != modePublic || n.State != netStarting || n.EntryNodes != len(kolodht.PublicServers()) || n.LocalNode != "" || n.FirstNode != "" {
		t.Errorf("network %+v", n)
	}
	if st.Mode != modePublic || st.EntryNodesFrom != "public" || st.EntryNodes != "" || st.LocalNode != "" || st.FirstNode != "" {
		t.Errorf("settings %+v", st)
	}
	payload, err := json.Marshal(struct {
		Network  Network
		Settings Settings
	}{n, st})
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range kolodht.PublicServers() {
		if bytes.Contains(payload, []byte(a.Addr().String())) {
			t.Errorf("the payload lists the public entry node %s: %s", a, payload)
		}
	}
}

// When nothing answers in the public network the error names the three networks, and the app does not try another one.
func TestPublicNetworkUnreachable(t *testing.T) {
	s, rec := publicService(t, func(context.Context, *koloswarm.Swarm) error { return kolodht.ErrNoNodes })
	rec.waitNetwork(t, netFailed)
	n := s.Info().Network
	if n.Mode != modePublic || n.Error == nil || n.Error.Reason != "public-unreachable" || n.Error.Kind != kindNetwork || !errors.Is(n.Error, kolodht.ErrNoNodes) {
		t.Fatalf("%+v", n)
	}
	for _, want := range []string{"public network", "internet", "-bootstrap", "-local"} {
		if !strings.Contains(n.Message, want) {
			t.Errorf("the message does not name %q: %s", want, n.Message)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sw != nil {
		t.Error("a swarm after the failure: another network was used")
	}
}

// -bootstrap is used alone: the public list is not in the swarm's configuration, and its failure names the other networks.
func TestOwnNetworkIsUsedAlone(t *testing.T) {
	s, err := Config{DataDir: t.TempDir(), Bootstrap: "127.0.0.1:4000,127.0.0.1:4001"}.resolve()
	if err != nil {
		t.Fatal(err)
	}
	if s.mode != modeOwn || s.nodesFrom != "flag" || s.local.IsValid() || s.entryNodeCount() != 2 || s.Listen != localListen {
		t.Fatalf("%+v", s)
	}
	if c := swarmConfig(s); len(c.BootstrapNodes) != 2 || c.BootstrapNodes[0] != "127.0.0.1:4000" || c.BootstrapNodes[1] != "127.0.0.1:4001" {
		t.Errorf("BootstrapNodes %v", c.BootstrapNodes)
	}
	e := networkError(s, nil, kolodht.ErrNoNodes)
	if e.Reason != "bootstrap-failed" || !strings.Contains(e.Message, "127.0.0.1:4000,127.0.0.1:4001") || !strings.Contains(e.Message, "public network") || !strings.Contains(e.Message, "-local") {
		t.Errorf("%+v", e)
	}
	if s, err = (Config{DataDir: t.TempDir(), Bootstrap: "127.0.0.1:5", First: "127.0.0.1:6"}).resolve(); err != nil || s.mode != modeOwn || len(s.nodes) != 2 || s.nodes[0] != "127.0.0.1:6" {
		t.Errorf("-first and -bootstrap: %+v %v", s, err)
	}
}

// -local is this computer only: the entry node is on 127.0.0.1, with nothing from the public list.
func TestLocalNetworkAlone(t *testing.T) {
	s, err := Config{DataDir: t.TempDir(), Local: true}.resolve()
	if err != nil {
		t.Fatal(err)
	}
	want := netip.MustParseAddrPort("127.0.0.1:7451")
	if s.mode != modeLocal || s.nodesFrom != "local" || s.local != want || len(s.nodes) != 1 || s.nodes[0] != want.String() || s.Listen != localListen || s.nodesText != "" {
		t.Fatalf("%+v", s)
	}
	if s.entryNodeCount() != 1 || swarmConfig(s).BootstrapNodes[0] != want.String() {
		t.Errorf("%d entry nodes", s.entryNodeCount())
	}
	if e := networkError(s, nil, kolodht.ErrNoNodes); e.Reason != "local-port-taken" || e.Detail != want.String() ||
		!strings.Contains(e.Message, "-bootstrap") || !strings.Contains(e.Message, "without -local") {
		t.Errorf("%+v", e)
	}
	if e := networkError(s, nil, errors.New("boom")); e.Reason != "bootstrap-failed" {
		t.Errorf("another failure: %+v", e)
	}
}

func TestLocalWithBootstrapOrFirstIsAnError(t *testing.T) {
	for _, tc := range []struct {
		c    Config
		want string
	}{
		{Config{Local: true, Bootstrap: "127.0.0.1:4000"}, "-local cannot be used with -bootstrap"},
		{Config{Local: true, Bootstrap: "127.0.0.1:4000", bootstrapFrom: "env"}, "-bootstrap (KOLO_BOOTSTRAP)"},
		{Config{Local: true, First: "127.0.0.1:4000"}, "-local cannot be used with -first"},
	} {
		tc.c.DataDir = t.TempDir()
		_, err := tc.c.resolve()
		var ce *ConfigError
		if !errors.As(err, &ce) || ce.Setting != "local" || !errors.Is(err, ErrConfig) || !errors.Is(err, ErrConflict) || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%+v: %v", tc.c, err)
		}
	}
	// the same through Start: a window says which setting is wrong
	s := NewBoardService(Config{DataDir: t.TempDir(), Local: true, Bootstrap: "127.0.0.1:4000"}, func(string, any) {})
	_, err := s.Start()
	var be *BoardError
	if !errors.As(err, &be) || be.Reason != "config-setting" || be.Detail != "local" || !strings.Contains(be.Message, "-local") {
		t.Errorf("Start: %v", err)
	}
}

func TestLoadConfigLocal(t *testing.T) {
	for _, tc := range []struct {
		args []string
		env  map[string]string
		want bool
		from string
	}{
		{nil, nil, false, ""},
		{[]string{"-local"}, nil, true, "flag"},
		{[]string{"-local=true"}, nil, true, "flag"},
		{nil, map[string]string{"KOLO_BOARD_LOCAL": "1"}, true, "env"},
		{nil, map[string]string{"KOLO_BOARD_LOCAL": "0"}, false, ""},
		{[]string{"-local=false"}, map[string]string{"KOLO_BOARD_LOCAL": "1"}, false, "flag"},
		{[]string{"-local"}, map[string]string{"KOLO_BOARD_LOCAL": "0"}, true, "flag"},
	} {
		c, err := LoadConfig(tc.args, envOf(tc.env), &strings.Builder{})
		if err != nil || c.Local != tc.want || c.localFrom != tc.from {
			t.Errorf("%v %v: %+v %v", tc.args, tc.env, c, err)
		}
	}
	if _, err := LoadConfig(nil, envOf(map[string]string{"KOLO_BOARD_LOCAL": "yes please"}), &strings.Builder{}); !errors.Is(err, ErrConfig) || !errors.Is(err, ErrMalformed) {
		t.Errorf("a bad KOLO_BOARD_LOCAL: %v", err)
	}
	// KOLO_BOOTSTRAP in the environment and -local on the command line are two networks
	c, err := LoadConfig([]string{"-local"}, envOf(map[string]string{"KOLO_BOOTSTRAP": "127.0.0.1:4000"}), &strings.Builder{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.resolve(); !errors.Is(err, ErrConflict) {
		t.Errorf("%v", err)
	}
	var out strings.Builder
	LoadConfig([]string{"-h"}, envOf(nil), &out)
	for _, want := range []string{"-local", "KOLO_BOARD_LOCAL"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the help does not name %q:\n%s", want, out.String())
		}
	}
}

// The first window of -local runs the entry node, the others join it, and the entry node is not the public one.
func TestLocalModeFirstThenSecond(t *testing.T) {
	port := freePort(t)
	addr := netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), port).String()
	a := startPeer(t, "Ana", Config{Local: true, source: loopbackSource(port)}, nil)
	a.rec.waitNetwork(t, netReady)
	b := startPeer(t, "Bia", Config{Local: true, source: loopbackSource(port)}, nil)
	b.rec.waitNetwork(t, netReady)
	if x, y := a.Info().Network, b.Info().Network; x.Mode != modeLocal || y.Mode != modeLocal || x.LocalNode != addr || y.LocalNode != addr || x.FirstNode != addr || y.FirstNode != "" || y.EntryNodes != 1 {
		t.Fatalf("Ana %+v, Bia %+v", x, y)
	}
	if st := b.Settings(); st.Mode != modeLocal || st.EntryNodesFrom != "local" || st.EntryNodes != "" || st.LocalNode != addr || st.FirstNode != "" {
		t.Fatalf("settings %+v", st)
	}
	if _, err := a.CreateBoard("Local", nil); err != nil {
		t.Fatal(err)
	}
	invited(t, a, b, InviteOptions{TTLSeconds: 3600})
}

// A program that is not this app holds the port: the network fails with the way out, and nothing else is tried.
func TestLocalPortTakenByAnotherProgram(t *testing.T) {
	pc, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	port := netip.MustParseAddrPort(pc.LocalAddr().String()).Port()
	p := startPeer(t, "Ana", Config{Local: true, source: loopbackSource(port)}, nil)
	p.rec.waitNetwork(t, netFailed)
	if n := p.Info().Network; n.Error == nil || n.Error.Reason != "local-port-taken" || n.Error.Detail != pc.LocalAddr().String() || !strings.Contains(n.Message, "-bootstrap") {
		t.Fatalf("%+v", n)
	}
}

// -bootstrap to a loopback node runs a whole network without the public list.
func TestOwnNetworkOnLoopback(t *testing.T) {
	entry := startPeer(t, "Ana", Config{First: freeUDP(t)}, nil)
	entry.rec.waitNetwork(t, netReady)
	b := startPeer(t, "Bia", Config{Bootstrap: entry.cfg.First}, nil)
	b.rec.waitNetwork(t, netReady)
	if n := b.Info().Network; n.Mode != modeOwn || n.EntryNodes != 1 || n.FirstNode != "" || n.LocalNode != "" {
		t.Errorf("%+v", n)
	}
	if n := entry.Info().Network; n.Mode != modeOwn || n.FirstNode != entry.cfg.First {
		t.Errorf("%+v", n)
	}
}
