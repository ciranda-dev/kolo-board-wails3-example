package main

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"

	"github.com/wailsapp/wails/v3/pkg/application"
)

const mainWindowName = "main"

// Files of the data directory. The board is one Pebble directory under boardDir; the marker file says it holds a board
// that is complete, so a directory left by a join or a create that did not finish is removed at the next start.
const (
	identityFile = "identity.key"
	boardDir     = "board"
	markerFile   = "board.json"
	invitesDir   = "invites"
	invitesMeta  = "invites.json"
	maxSmallFile = 64 << 10
)

// prepareDataDir creates dir with mode 0700, or checks that an existing one is not open to other users, since it holds
// the identity and the board.
func prepareDataDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return boardErr("data-dir-open", nil, dir)
	}
	return nil
}

// fromMainWindow reports whether a bound call comes from the main window. Wails puts the calling window in the context;
// a call without one (a test) counts as another window.
func fromMainWindow(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	w, ok := ctx.Value(application.WindowKey).(interface{ Name() string })
	return ok && w.Name() == mainWindowName
}

// marker is the content of board.json.
type marker struct {
	Version int    `json:"v"`
	Group   string `json:"group"`
	// Member is set once this member was seen in the roster: a board that does not list it any more was left by a removal.
	Member bool `json:"member"`
}

// readSmall reads a regular file of at most maxSmallFile bytes; a missing one is (nil, nil), and so is one that is not a
// regular file or is too large: it is not ours.
func readSmall(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxSmallFile {
		return nil, nil
	}
	return os.ReadFile(path)
}

func readMarker(dir string) (marker, bool, error) {
	b, err := readSmall(filepath.Join(dir, markerFile))
	if err != nil || b == nil {
		return marker{}, false, err
	}
	var m marker
	if json.Unmarshal(b, &m) != nil || m.Version != 1 {
		return marker{}, false, nil
	}
	return m, true, nil
}

func writeMarker(dir string, m marker) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(dir, markerFile), b)
}
