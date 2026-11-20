package main

import (
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	kolodht "github.com/ciranda-dev/kolo-dht"
	koloswarm "github.com/ciranda-dev/kolo-swarm"
)

// Defaults of the settings that have one.
const (
	// defaultLookupInterval is the interval kolo-invite configures for a joiner's swarm (README.md:58,
	// examples/basic/main.go:31): kolo-swarm's 10 minutes would keep a joiner that starts before its inviter waiting about
	// 12 minutes (kolo-invite docs/limits.md:47-51).
	defaultLookupInterval = 5 * time.Second
	localListen           = "127.0.0.1:0"
	publicListen          = "0.0.0.0:0"
	// defaultLocalPort is where -local runs its entry node, on 127.0.0.1 only. It is fixed so every window finds it
	// without a flag; next to kolo-node's 7447 and 7448, so it does not clash with a local kolo-node, and below the
	// ephemeral ranges (from 32768 on Linux, 49152 on macOS and Windows), so no random port takes it.
	defaultLocalPort = 7451
)

// The three networks the app can use. Settings.Mode and Network.Mode hold one of these.
const (
	modePublic = "public" // no flag: the public entry nodes built into kolo-dht
	modeOwn    = "own"    // -bootstrap or -first: the entry nodes of a network of your own, alone
	modeLocal  = "local"  // -local: this computer only
)

// ErrConfig matches every ConfigError, whatever setting it names.
var ErrConfig = errors.New("invalid configuration")

// Causes of a ConfigError.
var (
	ErrMissing    = errors.New("is required")
	ErrMalformed  = errors.New("is malformed")
	ErrUnreadable = errors.New("cannot be read")
	ErrConflict   = errors.New("cannot be used with")
)

// ConfigError names the setting that is wrong and why. errors.Is(err, ErrConfig) is true for every one.
type ConfigError struct {
	Setting string // the flag name, such as "bootstrap"
	Err     error
}

func (e *ConfigError) Error() string { return fmt.Sprintf("kolo-board: -%s %v", e.Setting, e.Err) }
func (e *ConfigError) Unwrap() error { return e.Err }
func (e *ConfigError) Is(target error) bool {
	return target == ErrConfig
}

// Config is everything the app needs from outside. A zero field takes its default, if it has one. LoadConfig fills it
// from flags and environment variables.
type Config struct {
	DataDir        string        // -data, KOLO_BOARD_DATA. Default: <user config dir>/kolo-board
	Bootstrap      string        // -bootstrap, KOLO_BOOTSTRAP: the entry nodes of your own network, a comma list of host:port. Default none: the public network
	First          string        // -first, KOLO_BOARD_FIRST: run the first entry node of a test network on this loopback IP:PORT
	Local          bool          // -local, KOLO_BOARD_LOCAL=1: this computer only, on loopback; the first window runs the entry node. Default off
	Listen         string        // -listen, KOLO_BOARD_LISTEN. Default 127.0.0.1:0 on a loopback network, else 0.0.0.0:0
	FindTimeout    time.Duration // -find-timeout, KOLO_BOARD_FIND_TIMEOUT: how long a join looks for the person who invited. Default none: until the person cancels
	JoinTimeout    time.Duration // -join-timeout, KOLO_BOARD_JOIN_TIMEOUT: how long a join waits for the answer (an admin may have to approve). Default none: until the person cancels
	LookupInterval time.Duration // -lookup-interval, KOLO_BOARD_LOOKUP_INTERVAL: time between lookups of a topic, and the longest wait before a dial is retried. Default 5s
	NotchLinger    time.Duration // -notch-linger, KOLO_BOARD_NOTCH_LINGER: how long the notch stays after a change. Default none: until the person looks at the board or hides it
	// Relays and RelayFiles are the connection relays, used when a direct connection fails: -relay (repeatable, a comma
	// list of host:port@key) and -relay-file (repeatable), or KOLO_BOARD_RELAYS when neither flag is given. Default none.
	Relays     []string
	RelayFiles []string

	bootstrapFrom string // "flag", "env" or "" (set in code)
	relaysFrom    string // "flag", "env" or "" (set in code)
	localFrom     string // "flag", "env" or "" (set in code)

	source entrySource // the entry node of -local; nil: loopbackSource(defaultLocalPort). A test hook
}

// LoadConfig reads the flags in args. A setting that is not given as a flag comes from its environment variable (read
// with getenv, such as os.Getenv), and a flag wins over its variable.
func LoadConfig(args []string, getenv func(string) string, stderr io.Writer) (Config, error) {
	var c Config
	fs := flag.NewFlagSet("kolo-board", flag.ContinueOnError)
	fs.SetOutput(stderr)
	text := func(p *string) func(string) error {
		return func(v string) error { *p = v; return nil }
	}
	dur := func(p *time.Duration) func(string) error {
		return func(v string) (err error) { *p, err = time.ParseDuration(v); return err }
	}
	for _, s := range []struct {
		name, env, help string
		set             func(string) error
	}{
		{"data", "KOLO_BOARD_DATA", "data directory (default <user config dir>/kolo-board)", text(&c.DataDir)},
		{"bootstrap", "KOLO_BOOTSTRAP", "entry nodes of your own network, a comma list of host:port (default: the public network)", text(&c.Bootstrap)},
		{"first", "KOLO_BOARD_FIRST", "run the first entry node of a local test network on this loopback IP:PORT, such as 127.0.0.1:4000", text(&c.First)},
		{"listen", "KOLO_BOARD_LISTEN", "local IP:PORT (default 127.0.0.1:0 on a loopback network, else 0.0.0.0:0)", text(&c.Listen)},
		{"find-timeout", "KOLO_BOARD_FIND_TIMEOUT", "how long a join looks for the person who invited (default: until the person cancels)", dur(&c.FindTimeout)},
		{"join-timeout", "KOLO_BOARD_JOIN_TIMEOUT", "how long a join waits for the answer to its request (default: until the person cancels)", dur(&c.JoinTimeout)},
		{"lookup-interval", "KOLO_BOARD_LOOKUP_INTERVAL", "time between lookups of a topic, and the longest wait before a dial is retried (default 5s)", dur(&c.LookupInterval)},
		{"notch-linger", "KOLO_BOARD_NOTCH_LINGER", "how long the notch stays after a change (default: until the person looks at the board or hides it)", dur(&c.NotchLinger)},
	} {
		if v := getenv(s.env); v != "" {
			if err := s.set(v); err != nil {
				return c, &ConfigError{Setting: s.name, Err: fmt.Errorf("%w: %s: %w", ErrMalformed, s.env, err)}
			}
			if s.name == "bootstrap" {
				c.bootstrapFrom = "env"
			}
		}
		set := s.set
		if s.name == "bootstrap" {
			set = func(v string) error { c.bootstrapFrom = "flag"; return s.set(v) }
		}
		fs.Func(s.name, s.help+" ("+s.env+")", set)
	}
	if v := getenv("KOLO_BOARD_LOCAL"); v != "" {
		on, err := strconv.ParseBool(v)
		if err != nil {
			return c, &ConfigError{Setting: "local", Err: fmt.Errorf("%w: KOLO_BOARD_LOCAL: want 1 or 0", ErrMalformed)}
		}
		c.Local = on
		if on {
			c.localFrom = "env"
		}
	}
	fs.BoolFunc("local", "this computer only: the first window runs the entry node on 127.0.0.1, the others join it (KOLO_BOARD_LOCAL=1)",
		func(v string) error {
			on, err := strconv.ParseBool(v)
			if err != nil {
				return err
			}
			c.Local, c.localFrom = on, "flag"
			return nil
		})
	fs.Func("relay", "connection relay, host:port@key; repeat it or give a comma list (KOLO_BOARD_RELAYS when no -relay or -relay-file is given)",
		func(v string) error { c.Relays = append(c.Relays, v); c.relaysFrom = "flag"; return nil })
	fs.Func("relay-file", "file of connection relays, one or more host:port@key per line, \"#\" starts a comment; repeatable",
		func(v string) error { c.RelayFiles = append(c.RelayFiles, v); c.relaysFrom = "flag"; return nil })
	if err := fs.Parse(args); err != nil {
		return c, err
	}
	if v := getenv("KOLO_BOARD_RELAYS"); v != "" && c.relaysFrom == "" {
		c.Relays, c.relaysFrom = []string{v}, "env"
	}
	return c, nil
}

// entrySource gives the entry node of -local, and the address this app may run it on when it is free. The startup
// tries to run host and joins nodes when it cannot.
type entrySource func() (nodes []string, host netip.AddrPort)

// loopbackSource is -local: one entry node on 127.0.0.1:port, run by the first window of this computer that takes the
// port. Windows on other computers cannot reach it.
func loopbackSource(port uint16) entrySource {
	return func() ([]string, netip.AddrPort) {
		host := netip.AddrPortFrom(netip.AddrFrom4([4]byte{127, 0, 0, 1}), port)
		return []string{host.String()}, host
	}
}

// settings is a Config after defaults and checks: values the code can use as they are.
type settings struct {
	Config
	mode       string            // modePublic, modeOwn or modeLocal
	first      netip.AddrPort    // valid when this app runs the first entry node of -first
	local      netip.AddrPort    // valid in modeLocal: the address this app runs the entry node on, if it is free
	nodes      []string          // entry nodes of modeOwn and modeLocal, from ParseNodes; nil in modePublic, where kolo-swarm uses the public ones
	nodesText  string            // nodes as a comma list; "" in modePublic, whose addresses the window does not list
	nodesFrom  string            // "public", "flag", "env", "first" or "local"
	relays     []koloswarm.Relay // for koloswarm.Config.Relays
	relaysText string
	relaysFrom string // "flag", "file", "flag-and-file", "env" or "none"
}

// entryNodeCount is how many entry nodes the app starts from: the public ones in modePublic.
func (s settings) entryNodeCount() int {
	if s.mode == modePublic {
		return len(kolodht.PublicServers())
	}
	return len(s.nodes)
}

// resolve applies the defaults and checks every setting. It returns a *ConfigError for the first bad setting.
func (c Config) resolve() (settings, error) {
	s := settings{Config: c}
	if s.DataDir == "" {
		dir, err := os.UserConfigDir()
		if err != nil {
			return s, &ConfigError{Setting: "data", Err: fmt.Errorf("%w: no user config directory: %w", ErrMissing, err)}
		}
		s.DataDir = filepath.Join(dir, "kolo-board")
	}
	// A wait has no default: kolo-invite leaves it to the caller (docs/guide.md:114-115, "a manual invite waits for a
	// person"), and the window has Cancel (LeaveBoard). Zero waits until then.
	for _, w := range []struct {
		name string
		d    time.Duration
	}{{"find-timeout", s.FindTimeout}, {"join-timeout", s.JoinTimeout}, {"notch-linger", s.NotchLinger}} {
		if w.d < 0 {
			return s, &ConfigError{Setting: w.name, Err: fmt.Errorf("%w: not negative", ErrMalformed)}
		}
	}
	if s.LookupInterval == 0 {
		s.LookupInterval = defaultLookupInterval
	}
	if s.LookupInterval < time.Second || s.LookupInterval > time.Hour {
		return s, &ConfigError{Setting: "lookup-interval", Err: fmt.Errorf("%w: from 1s to 1h", ErrMalformed)}
	}
	if s.First != "" {
		a, err := netip.ParseAddrPort(s.First)
		if err != nil || !a.Addr().Is4() || !a.Addr().IsLoopback() || a.Port() == 0 {
			return s, &ConfigError{Setting: "first", Err: fmt.Errorf("%w: want a loopback IPv4 IP:PORT such as 127.0.0.1:4000, for local testing only", ErrMalformed)}
		}
		s.first = a
	}

	// Three networks, one at a time: your own (-bootstrap, -first), this computer only (-local), else the public one.
	switch {
	case c.Local && (c.Bootstrap != "" || c.First != ""):
		other := "-bootstrap"
		switch {
		case c.Bootstrap == "" && c.First != "":
			other = "-first"
		case c.bootstrapFrom == "env":
			other = "-bootstrap (KOLO_BOOTSTRAP)"
		}
		return s, &ConfigError{Setting: "local", Err: fmt.Errorf("%w %s. Choose this computer only (-local) or a network of your own, not both", ErrConflict, other)}
	case c.Local:
		src := c.source
		if src == nil {
			src = loopbackSource(defaultLocalPort)
		}
		s.mode, s.nodesFrom = modeLocal, "local"
		s.nodes, s.local = src()
	case c.Bootstrap != "" || c.First != "":
		s.mode, s.nodesText, s.nodesFrom = modeOwn, c.Bootstrap, c.bootstrapFrom
		if s.nodesFrom == "" {
			s.nodesFrom = "flag" // set in code, as a flag is
		}
		nodes, err := parseEntryNodes(s.nodesText)
		if err != nil {
			return s, &ConfigError{Setting: "bootstrap", Err: err}
		}
		s.nodes = nodes
		if s.first.IsValid() && !slices.Contains(nodes, s.first.String()) {
			s.nodes = append([]string{s.first.String()}, nodes...)
		}
		if len(nodes) == 0 {
			s.nodesFrom, s.nodesText = "first", s.first.String()
		}
	default:
		s.mode, s.nodesFrom, s.nodesText = modePublic, "public", ""
	}

	if s.Listen == "" {
		s.Listen = publicListen
		if loopbackOnly(s.nodes) {
			s.Listen = localListen
		}
	}
	if a, err := netip.ParseAddrPort(s.Listen); err != nil || !a.Addr().Is4() {
		return s, &ConfigError{Setting: "listen", Err: fmt.Errorf("%w: want an IPv4 IP:PORT such as 0.0.0.0:0", ErrMalformed)}
	}
	return s, s.resolveRelays()
}

// resolveRelays reads the connection relays of the flags and files, else of KOLO_BOARD_RELAYS, as one list that
// kolo-swarm checks: at most 16, IPv4 only, no key or address twice.
func (s *settings) resolveRelays() error {
	s.relaysFrom = "env"
	where := "KOLO_BOARD_RELAYS: "
	if s.Config.relaysFrom != "env" {
		s.relaysFrom, where = "flag", "" // a flag, or set in code
		switch {
		case len(s.Relays) == 0 && len(s.RelayFiles) > 0:
			s.relaysFrom = "file"
		case len(s.Relays) > 0 && len(s.RelayFiles) > 0:
			s.relaysFrom = "flag-and-file"
		}
	}
	relays, err := koloswarm.ParseRelays(strings.Join(s.Relays, ","))
	if err != nil {
		return &ConfigError{Setting: "relay", Err: relayParseError(err, where)}
	}
	for _, path := range s.RelayFiles {
		rs, err := koloswarm.LoadRelays(path)
		var pe *fs.PathError
		switch {
		case errors.As(err, &pe): // missing or unreadable, not malformed
			return &ConfigError{Setting: "relay-file", Err: fmt.Errorf("%w: %w", ErrUnreadable, pe)}
		case err == nil: // checked again with the relays before it
			rs, err = koloswarm.ParseRelays(relaysText(slices.Concat(relays, rs)))
		}
		if err != nil {
			return &ConfigError{Setting: "relay-file", Err: relayParseError(err, path+": ")}
		}
		relays = rs
	}
	s.relays, s.relaysText = relays, relaysText(relays)
	if len(relays) == 0 {
		s.relaysFrom = "none"
	}
	return nil
}

// relaysText writes relays in the form ParseRelays reads.
func relaysText(relays []koloswarm.Relay) string {
	out := make([]string, len(relays))
	for i, r := range relays {
		out[i] = r.Addr + "@" + hex.EncodeToString(r.PublicKey)
	}
	return strings.Join(out, ",")
}

// relayParseError is a kolo-swarm relay parse error as ErrMalformed, without the library's prefixes.
func relayParseError(err error, where string) error {
	msg := strings.TrimPrefix(strings.TrimPrefix(err.Error(), "koloswarm: "), "invalid argument: ")
	return fmt.Errorf("%w: %s%s", ErrMalformed, where, msg)
}

// parseEntryNodes parses a comma list with kolo-swarm's ParseNodes and turns its error into a sentence.
func parseEntryNodes(s string) ([]string, error) {
	nodes, err := koloswarm.ParseNodes(s)
	if err != nil {
		msg := strings.TrimPrefix(err.Error(), "koloswarm: ")
		msg = strings.TrimPrefix(msg, "invalid argument: ")
		return nil, fmt.Errorf("%w: %s", ErrMalformed, msg)
	}
	return nodes, nil
}

// loopbackOnly reports whether every entry node is a loopback IPv4 literal, so the app can stay on 127.0.0.1.
func loopbackOnly(nodes []string) bool {
	if len(nodes) == 0 {
		return false
	}
	for _, n := range nodes {
		a, err := netip.ParseAddrPort(n)
		if err != nil || !a.Addr().IsLoopback() {
			return false
		}
	}
	return true
}
