package main

import (
	"fmt"
)

// TrayState is what the menu bar or tray icon shows: the active icon while there are changes by other members that were
// not seen, the status line of its menu, and the notch setting for its checkbox.
type TrayState struct {
	Active      bool   `json:"active"`
	Status      string `json:"status"`
	Unseen      int    `json:"unseen"`
	ShowInNotch bool   `json:"showInNotch"`
}

// trayUI is the menu bar or tray icon; the app's comes from Wails, and tests have none.
type trayUI interface {
	set(TrayState)
}

// trayOf maps the board to the tray. It holds no card text: the title of the board only.
func trayOf(b *Board) TrayState {
	st := TrayState{Status: "No board"}
	if b == nil {
		return st
	}
	switch b.State {
	case stateCreating:
		st.Status = "Creating a board"
	case stateJoining:
		st.Status = "Joining a board"
	case stateRemoved:
		st.Status = "Removed from the board"
	case stateActive:
		st.Unseen = b.Unseen
		st.Active = b.Unseen > 0
		switch {
		case b.Unseen == 1:
			st.Status = "1 change since you looked"
		case b.Unseen > 1:
			st.Status = fmt.Sprintf("%d changes since you looked", b.Unseen)
		default:
			st.Status = "Board is up to date"
		}
		if b.Sync.CardsSyncing > 0 {
			st.Status += fmt.Sprintf(", %d syncing", b.Sync.CardsSyncing)
		}
	}
	return st
}

// trayNow is what the tray icon shows now.
func (s *BoardService) trayNow() TrayState {
	var snap *Board
	if b := s.board(); b != nil {
		v := b.snapshot()
		snap = &v
	}
	st := trayOf(snap)
	st.ShowInNotch = s.prefs().showInNotch()
	return st
}

// pushTray tells the tray icon what it shows, when that changed.
func (s *BoardService) pushTray() {
	s.trayMu.Lock()
	defer s.trayMu.Unlock()
	st := s.trayNow()
	s.mu.Lock()
	changed := st != s.lastTray
	s.lastTray = st
	s.mu.Unlock()
	if changed && s.tray != nil {
		s.tray.set(st)
	}
}
