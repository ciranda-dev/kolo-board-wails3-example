package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"log"
	"slices"
	"time"

	koloid "github.com/ciranda-dev/kolo-id"
	kolorelay "github.com/ciranda-dev/kolo-relay"
	kolosecure "github.com/ciranda-dev/kolo-secure"
	koloswarm "github.com/ciranda-dev/kolo-swarm"
)

// A connection relay is a kolo-relay of the settings (-relay). When two members cannot connect directly, kolo-swarm
// connects them through a relay both list, and the board gets that connection like a direct one: braid's ServeConn
// takes either. The relay passes on bytes it cannot read.

// States of a connection relay, as the window shows them.
const (
	relayWaiting = "waiting" // the app holds a place there only while it has a board on the network; it still uses it to reach others
	relayReady   = "ready"   // it holds a place: members who cannot reach this computer directly reach it through the relay
	relayFailed  = "failed"  // Error says why
)

// relayCheckTimeout bounds the check of one relay's key when the network starts.
const relayCheckTimeout = 30 * time.Second

// Who reported a relay's state: the swarm, or the check of its key when the network starts.
const (
	fromSwarm = "swarm"
	fromCheck = "check"
)

// RelayStatus is one connection relay in the "board:network" event.
type RelayStatus struct {
	Addr  string      `json:"addr"`
	Key   string      `json:"key"` // the relay's kolo-id in hex
	State string      `json:"state"`
	Error *BoardError `json:"error"` // why it failed; nil otherwise
}

// checkRelays proves each relay's key with one handshake, as a stranger with a throwaway key, and reports a relay that
// answers with another key. kolo-swarm reports that only for a place, which a person without a board does not hold, so
// a joiner would otherwise never learn it. It runs in the network's WaitGroup, so stopping the network ends it.
func (s *BoardService) checkRelays(ctx context.Context) {
	for _, r := range s.settingsCopy().relays {
		s.netWG.Go(func() {
			ctx, cancel := context.WithTimeout(ctx, relayCheckTimeout)
			defer cancel()
			if err := checkRelay(ctx, r); errors.Is(err, kolosecure.ErrUnexpectedPeer) {
				s.relayChanged(fromCheck, koloswarm.Event{Kind: koloswarm.RelayChanged, PublicKey: r.PublicKey, Err: err})
			}
		})
	}
}

// checkRelay proves the relay's key and asks it for nothing: whatever a relay does after the handshake is its own
// doing, not a wrong key in the settings.
func checkRelay(ctx context.Context, r koloswarm.Relay) error {
	me, err := koloid.Generate()
	if err != nil {
		return err
	}
	conn, err := kolosecure.Dial(ctx, r.Addr, me, r.PublicKey, nil)
	if err == nil {
		conn.Close()
	}
	return err
}

// relayChanged records what the swarm or the check said about a relay, logs a failure the first time it happens with
// that reason, and tells the window when a state changed.
func (s *BoardService) relayChanged(from string, ev koloswarm.Event) {
	if errors.Is(ev.Err, kolorelay.ErrReservationEnded) {
		return // a place ends with the relay's lifetime and kolo-swarm takes a new one: not a failure
	}
	addr := s.relayAddr(ev.PublicKey)
	st := RelayStatus{State: relayReady}
	switch {
	case ev.Err == nil:
	case errors.Is(ev.Err, koloswarm.ErrClosed):
		st.State = relayWaiting
	default:
		st.State, st.Error = relayFailed, relayError(ev.Err, addr)
	}
	s.relayMu.Lock()
	if s.relayStates == nil {
		s.relayStates, s.relayLogged = map[string]map[string]RelayStatus{}, map[string]bool{}
	}
	if s.relayStates[from] == nil {
		s.relayStates[from] = map[string]RelayStatus{}
	}
	s.relayStates[from][string(ev.PublicKey)] = st
	logIt := false
	if st.Error != nil {
		seen := string(ev.PublicKey) + st.Error.Reason
		logIt, s.relayLogged[seen] = !s.relayLogged[seen], true
	}
	s.relayMu.Unlock()
	if logIt {
		log.Print("connection relay ", addr, ": ", st.Error.Message)
	}
	s.pushRelays()
}

// forgetRelays drops what the stopped network's swarm and check said.
func (s *BoardService) forgetRelays() {
	s.relayMu.Lock()
	clear(s.relayStates)
	s.relayMu.Unlock()
	s.pushRelays()
}

// relayError is why the app holds no place at the relay at addr.
func relayError(err error, addr string) *BoardError {
	switch {
	case errors.Is(err, kolosecure.ErrUnexpectedPeer):
		return boardErr("relay-wrong-key", err, addr)
	case errors.Is(err, koloswarm.ErrInvalidConfig):
		return boardErr("relay-bad-key", err, addr)
	case errors.Is(err, kolorelay.ErrNotAllowed):
		return boardErr("relay-not-allowed", err, addr)
	case errors.Is(err, kolorelay.ErrLimit):
		return boardErr("relay-full", err, addr)
	}
	return boardErr("relay-unreachable", err, addr)
}

// relayStatuses is every listed relay with the worst state the swarm or the check reported for it. Never nil.
func (s *BoardService) relayStatuses() []RelayStatus {
	relays := s.settingsCopy().relays
	s.relayMu.Lock()
	defer s.relayMu.Unlock()
	out := make([]RelayStatus, 0, len(relays))
	for _, r := range relays {
		st := RelayStatus{Addr: r.Addr, Key: hex.EncodeToString(r.PublicKey), State: relayWaiting}
		for _, from := range []string{fromSwarm, fromCheck} {
			x, ok := s.relayStates[from][string(r.PublicKey)]
			switch {
			case !ok || x.State == relayWaiting:
			case x.State == relayFailed:
				st.State, st.Error = relayFailed, x.Error
			case st.State == relayWaiting:
				st.State = relayReady
			}
		}
		out = append(out, st)
	}
	return out
}

// relayAddr is the address of the listed relay with key, or "" if none is listed with it.
func (s *BoardService) relayAddr(key []byte) string {
	for _, r := range s.settingsCopy().relays {
		if bytes.Equal(r.PublicKey, key) {
			return r.Addr
		}
	}
	return ""
}

// sameRelays reports whether the window already shows these states.
func sameRelays(a, b []RelayStatus) bool {
	return slices.EqualFunc(a, b, func(x, y RelayStatus) bool {
		return x.Addr == y.Addr && x.State == y.State && (x.Error == nil) == (y.Error == nil) && (x.Error == nil || x.Error.Reason == y.Error.Reason)
	})
}
