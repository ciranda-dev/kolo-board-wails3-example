package main

import (
	"time"
)

const (
	notchShortcut    = "CmdOrCtrl+Alt+Shift+B"
	notchWindowWidth = 660 // the library's default content area
	notchWindowH     = 92
)

// NotchState is what the notch window shows, the payload of the "board:notch" event: the latest change by another member
// and how settled the board is. It never holds an invite. With NotchPreview off it holds no board title and no card or
// column text either: only who changed something.
type NotchState struct {
	Active       bool         `json:"active"` // a board is shown: running, or one that this person was removed from
	State        string       `json:"state"`
	Title        string       `json:"title"`
	Members      int          `json:"members"`
	Online       int          `json:"online"`
	CardsSettled int          `json:"cardsSettled"`
	CardsSyncing int          `json:"cardsSyncing"`
	Pending      int          `json:"pending"`
	Settling     bool         `json:"settling"`
	Unseen       int          `json:"unseen"`
	Change       *NotchChange `json:"change"`
	Error        *BoardError  `json:"error"`
}

// NotchChange is the latest change by another member that the person has not seen.
type NotchChange struct {
	Kind   string `json:"kind"` // as ActivityItem.Kind
	Who    string `json:"who"`
	Card   string `json:"card"`   // "" without the preview
	Column string `json:"column"` // "" without the preview
}

// notchWindow is the part of the library's *application.NotchWindow the service uses.
type notchWindow interface {
	Show()
	Hide()
}

// notchOf is the notch for a board as the window sees it, and the latest change by another member, if there is one.
func notchOf(snap Board, last *Change, preview bool) NotchState {
	n := NotchState{Active: snap.State == stateActive || snap.State == stateRemoved, State: snap.State, Unseen: snap.Unseen,
		CardsSettled: snap.Sync.CardsSettled, CardsSyncing: snap.Sync.CardsSyncing, Pending: snap.Sync.Pending, Settling: snap.Sync.Settling, Error: snap.Error}
	if !n.Active {
		return NotchState{}
	}
	for _, p := range snap.People {
		if p.Member {
			n.Members++
			if p.Online {
				n.Online++
			}
		}
	}
	// Texts go as the board holds them, within the bounds of a title (names.go); the page shows one line of each.
	if preview {
		n.Title = snap.Title
	}
	if last != nil {
		c := &NotchChange{Kind: last.Kind, Who: last.Who}
		if preview {
			c.Card, c.Column = last.Card, last.Column
		}
		n.Change = c
	}
	return n
}

// Notch returns what the notch window shows now: nothing while "Show changes in the notch" is off.
func (s *BoardService) Notch() NotchState {
	if !s.prefs().showInNotch() {
		return NotchState{}
	}
	return s.notchNow(nil)
}

// notchNow reads the notch state; snap is the board if the caller has just read it.
func (s *BoardService) notchNow(snap *Board) NotchState {
	b := s.board()
	if b == nil {
		return NotchState{}
	}
	if snap == nil {
		v := b.snapshot()
		snap = &v
	}
	b.mu.Lock()
	last := b.last
	b.mu.Unlock()
	return notchOf(*snap, last, s.prefs().notchPreview())
}

// sendNotch sends the notch window what it shows now, while the setting is on, and opens it when there is a new change by
// another member. notchMu orders the sends with the setting's changes, and the setting is read under it, so no state
// follows the empty one of "off".
func (s *BoardService) sendNotch(snap *Board, newChange bool) {
	s.notchMu.Lock()
	defer s.notchMu.Unlock()
	on := s.prefs().showInNotch()
	n := s.notchNow(snap)
	if s.notchHook != nil {
		s.notchHook()
	}
	if on && s.emitNotch != nil {
		s.emitNotch("board:notch", n)
	}
	if on && newChange && n.Active {
		s.notchAutoShow()
	}
}

// notchAutoShow opens the notch for a change. It hides when the person looks at the board (MarkSeen) or, with
// -notch-linger, that long after the last change, unless the person showed it themselves.
func (s *BoardService) notchAutoShow() {
	if s.notch != nil {
		s.notch.Show()
	}
	s.mu.Lock()
	s.notchAuto = true
	if s.notchTimer != nil {
		s.notchTimer.Stop()
		s.notchTimer = nil
	}
	if d := s.set.NotchLinger; d > 0 {
		var t *time.Timer
		t = time.AfterFunc(d, func() {
			s.mu.Lock() // t is set under s.mu
			self := t
			s.mu.Unlock()
			s.notchDone(self)
		})
		s.notchTimer = t
	}
	s.mu.Unlock()
}

// notchDone hides a notch that a change opened, unless the person showed it themselves. from is the timer that calls it,
// nil for a look at the board; a timer replaced by a newer change does nothing.
func (s *BoardService) notchDone(from *time.Timer) {
	s.mu.Lock()
	if from != nil && s.notchTimer != from {
		s.mu.Unlock()
		return
	}
	if s.notchTimer != nil {
		s.notchTimer.Stop()
		s.notchTimer = nil
	}
	hide := s.notchAuto && !s.notchHold
	s.notchAuto = false
	s.mu.Unlock()
	if hide && s.notch != nil {
		s.notch.Hide()
	}
}

// NotchShow shows the notch window and keeps it until NotchHide.
func (s *BoardService) NotchShow() {
	s.mu.Lock()
	s.notchHold, s.notchAuto = true, false
	s.mu.Unlock()
	if s.notch != nil {
		s.notch.Show()
	}
}

// NotchHide hides the notch window; its page stays loaded.
func (s *BoardService) NotchHide() {
	s.mu.Lock()
	s.notchHold, s.notchAuto = false, false
	s.mu.Unlock()
	if s.notch != nil {
		s.notch.Hide()
	}
}

// NotchOpenApp brings the main window to the front and marks the changes as seen.
func (s *BoardService) NotchOpenApp() {
	if s.openApp != nil {
		s.openApp()
	}
}

// SetShowInNotch turns the setting "Show changes in the notch" on or off and saves it.
func (s *BoardService) SetShowInNotch(on bool) (Settings, error) {
	s.startMu.Lock()
	defer s.startMu.Unlock()
	if !s.started {
		return Settings{}, explain(ErrNotReady)
	}
	s.notchMu.Lock()
	err := s.saveSaved(func(p *savedSettings) { p.ShowInNotch = &on })
	if err == nil && !on && s.emitNotch != nil {
		s.emitNotch("board:notch", NotchState{}) // the page forgets what it showed
	}
	s.notchMu.Unlock()
	if err != nil {
		return Settings{}, boardErr("internal", err, "")
	}
	if on {
		s.sendNotch(nil, false)
	} else {
		s.NotchHide()
	}
	s.pushTray()
	return s.Settings(), nil
}

// SetNotchPreview turns the preview of card text in the notch on or off and saves it. Off, the notch tells who changed
// something and not what, for a screen that other people can see.
func (s *BoardService) SetNotchPreview(on bool) (Settings, error) {
	s.startMu.Lock()
	defer s.startMu.Unlock()
	if !s.started {
		return Settings{}, explain(ErrNotReady)
	}
	s.notchMu.Lock()
	err := s.saveSaved(func(p *savedSettings) { p.NotchPreview = &on })
	s.notchMu.Unlock()
	if err != nil {
		return Settings{}, boardErr("internal", err, "")
	}
	s.sendNotch(nil, false)
	return s.Settings(), nil
}
