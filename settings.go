package main

import (
	"encoding/json"
	"path/filepath"
)

const settingsFile = "settings.json"

// Settings is what the Settings view shows. The display name and the two notch settings can be changed there; the rest
// comes from flags and is read only, the network included: the network is chosen when the app starts.
type Settings struct {
	// Mode is the network: "public" (the default), "own" (-bootstrap or -first) or "local" (-local, this computer only).
	Mode string `json:"mode"`
	// EntryNodes are the entry nodes the network starts from, a comma list of your own: none in the public network (the window does not list the public ones) and in -local.
	EntryNodes string `json:"entryNodes"`
	// EntryNodesFrom is "public", "flag", "env", "first" or "local".
	EntryNodesFrom string `json:"entryNodesFrom"`
	FirstNode      string `json:"firstNode"`
	// LocalNode is the address of -local, "" in the other networks.
	LocalNode   string `json:"localNode"`
	DataDir     string `json:"dataDir"`
	Listen      string `json:"listen"`
	ShowInNotch bool   `json:"showInNotch"`
	// NotchPreview says whether the notch shows the text of a card; off, it shows who changed something, not what.
	NotchPreview bool   `json:"notchPreview"`
	DisplayName  string `json:"displayName"`
	// The waits of -find-timeout, -join-timeout, -lookup-interval and -notch-linger in milliseconds; 0 is none (no
	// deadline, or the notch stays until the person looks at the board).
	FindTimeoutMs    int64 `json:"findTimeoutMs"`
	JoinTimeoutMs    int64 `json:"joinTimeoutMs"`
	LookupIntervalMs int64 `json:"lookupIntervalMs"`
	NotchLingerMs    int64 `json:"notchLingerMs"`

	// ConnectionRelays are the connection relays as host:port@key, comma separated; ConnectionRelaysFrom is "flag",
	// "file", "flag-and-file", "env" or "none".
	ConnectionRelays     string `json:"connectionRelays"`
	ConnectionRelaysFrom string `json:"connectionRelaysFrom"`
}

// savedSettings is the file kept in the data directory.
type savedSettings struct {
	ShowInNotch  *bool  `json:"showInNotch,omitempty"`  // nil: on
	NotchPreview *bool  `json:"notchPreview,omitempty"` // nil: on
	DisplayName  string `json:"displayName,omitempty"`
}

func (s savedSettings) showInNotch() bool  { return s.ShowInNotch == nil || *s.ShowInNotch }
func (s savedSettings) notchPreview() bool { return s.NotchPreview == nil || *s.NotchPreview }

// loadSaved reads the saved settings of dir; a missing, oversized or unreadable file gives the zero value. A saved display
// name is cleaned again, and dropped if nothing valid is left.
func loadSaved(dir string) (savedSettings, error) {
	var s savedSettings
	b, err := readSmall(filepath.Join(dir, settingsFile))
	if err != nil || b == nil {
		return s, err
	}
	if json.Unmarshal(b, &s) != nil {
		return savedSettings{}, nil
	}
	s.DisplayName, _ = cleanName(s.DisplayName) // "" if nothing valid is left
	return s, nil
}

func saveSettings(dir string, s savedSettings) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(dir, settingsFile), b)
}
