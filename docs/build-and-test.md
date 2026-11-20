# Build and test

Only Wails needs cgo, on macOS and Linux, for the native window. The board and network libraries are pure Go. The repository has no `go.work` and no `replace`: `go.mod` requires the tagged Kolo libraries, so a build with `GOWORK=off` works. For work across Kolo repositories, use a `go.work` outside the repository.

## Checks

```sh
go vet ./...
go test ./...          # every test runs on 127.0.0.1
go test -race ./...
go test -fuzz FuzzDecodeOp -fuzztime 30s
```

What the tests cover:

- The rules of the board: every kind of change, every way a change is rejected, and bodies that are not changes at all. Random changes from three members on three stores end on the same settled board, and kolo-braid's determinism checks pass on the history they agreed on.
- Services in one process, without the window: the first is the first entry node of a private loopback network and the others bootstrap from it, or are started with `-local` on a free loopback port. No test sends a packet to a public address: a peer with no network setting gets an entry node of its own on loopback, and the public network is checked as configuration (the swarm gets no entry nodes, so kolo-swarm uses the public ones; the unreachable-network error names the three options) with a bootstrap that sends nothing. They check creating, joining with and without approval, expired, revoked, used up and denied invites, removing a member, what a removed member keeps, settling with settlers away, a restart, hostile text and bodies from a member, a board of 1500 cards, that the invite stays in the main window, and the notch and the tray.
- Connection relays, with real kolo-relays on 127.0.0.1 and a test hook (`accept`, nil in the example) that refuses every direct connection to a board, so two boards meet only through a relay: an invite, a join, a card each way that settles on both, and a removal that holds, all relayed: a card made after it reaches the member who stays and never the removed one; a relay that stands where its operator stands and sees the members' keys but none of the card text, the board name, the group ID, the invite or a stored key; a stranger that founded a board of its own reaching a member through the relay and getting nothing; a key on the invite's topic that is not the inviter's, which the joiner ignores until the inviter is back; a wrong relay key reported a few seconds after the start, a relay that holds its key and then refuses everything, which is not blamed on the key, and a stop of the network that ends a check stuck on a relay that never answers; quiet renewals of a place, failures logged once and events in order; and the flags, the variable and the file, with hostile entries (IPv6 in every form, 17 relays, clashes, a `/dev/zero` symlink, a directory, a file over 64 KiB, a missing file).
- Fuzz targets: the changes, the stored records, the join data, text cleaning and the positions between cards.
- `smoke_test.go` (build tag `smoke`) makes a board between two services against an entry node of a running example.

## Builds

- `wails3 build` builds `bin/kolo-board` for this computer.
- `wails3 task windows:build ARCH=amd64` cross-builds `bin/kolo-board.exe` for Windows on x86-64.
- A Linux build needs cgo with GTK 4.10 or later and WebKitGTK 6.0, so build it on Linux, for example Ubuntu 24.04 with `libgtk-4-dev` and `libwebkitgtk-6.0-dev`.

## Release files

`scripts/release-local.sh [output dir]` builds the release files of the committed HEAD on a Mac, unsigned and not notarized:

- macOS: a universal `.zip` and `.dmg`;
- Windows amd64 and arm64: `.exe`, `.zip` and installer;
- Linux amd64 and arm64: `.tar.gz` and `.deb`, built in Docker;
- `SHA256SUMS`.

The version of the example is the git tag: the script builds it into the binary (`-ldflags "-X main.version=..."`, shown in Settings and returned by `Info`) and writes it into the packaging files of the release copy with `scripts/stamp-version.sh` (`build/config.yml`, `Info.plist`, the deb, the Windows version info, the manifests, the MSIX files and the installer's defaults). The files in git hold the placeholder `0.0.0`. A build with `wails3 build` says the tag too (`KOLO_VERSION` overrides it); a dev build (`wails3 dev`) says `dev`.

It needs Go, Node.js, the `wails3` CLI and Docker. An unsigned app makes the system warn before it runs.
