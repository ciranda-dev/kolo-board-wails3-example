# Configuration

Every setting is a flag or an environment variable; a flag wins. A bad setting makes the window show the reason.

## The network

The example is on one of three networks, chosen when it starts:

| Network | How | Entry nodes |
|---|---|---|
| Public (the default) | no flag | the two public entry nodes built into kolo-dht, run by Ciranda: free, with no uptime guarantee while Kolo is a beta |
| Your own | `-bootstrap` (or `-first`) | the ones you give, alone: the public ones are not used |
| This computer only | `-local` | one entry node on `127.0.0.1:7451`, run by the first window on this computer; the others join it |

`-local` with `-bootstrap` or `-first` is an error that names both. If the network cannot be reached at start, the window says so and names the three options; the example never switches to another network by itself. Settings shows the network in use and, for your own network, its entry nodes (the window does not list the public ones).

| Flag | Environment | Default | Meaning |
|---|---|---|---|
| `-data` | `KOLO_BOARD_DATA` | `<user config dir>/kolo-board` | the data directory (see below) |
| `-bootstrap` | `KOLO_BOOTSTRAP` | none: the public network | the entry nodes of your own network, a comma list of `host:port` as for kolo-swarm's `ParseNodes`: IPv4 or a host name. They are used alone. Settings shows the list in use and where it came from |
| `-local` | `KOLO_BOARD_LOCAL` (`1`) | off | this computer only: the first window runs an entry node on `127.0.0.1:7451`, the others join it. Not with `-bootstrap` or `-first` |
| `-first` | `KOLO_BOARD_FIRST` | none | run the first entry node of a test network on this loopback IP:PORT, such as `127.0.0.1:4000`, used with `-bootstrap` by the other copies. For local testing only |
| `-listen` | `KOLO_BOARD_LISTEN` | `127.0.0.1:0` with `-local` or when every entry node is on loopback, else `0.0.0.0:0` | local IPv4 IP:PORT |
| `-find-timeout` | `KOLO_BOARD_FIND_TIMEOUT` | none: until you cancel | how long a join looks for the person who made the invite |
| `-join-timeout` | `KOLO_BOARD_JOIN_TIMEOUT` | none: until you cancel | how long a join waits for the answer to its request (an admin may have to approve it) |
| `-lookup-interval` | `KOLO_BOARD_LOOKUP_INTERVAL` | `5s`, as kolo-invite sets for a joiner | time between lookups of a topic (1s to 1h), and the longest wait before a member is dialed again |
| `-notch-linger` | `KOLO_BOARD_NOTCH_LINGER` | none: until you look at the board or hide it | how long the notch window stays after a change by another member |
| `-relay` | `KOLO_BOARD_RELAYS` | none: no relay, and no public one | connection relays as `host:port@key`, the key as 64 hex characters; repeat it or give a comma list. The variable is read only when neither `-relay` nor `-relay-file` is given |
| `-relay-file` | | none | a file of connection relays, one or more per line, `#` starts a comment; repeatable. A regular file of at most 64 KiB; one that cannot be read is reported as such |

Kolo is IPv4 only: an IPv6 entry node, listen address or relay address is refused. The relays of the flags and files are one list, with no key or address twice.

## Chosen in the window

The display name and the two notch settings are chosen in the window and saved in `settings.json`. Everything about the network is set with the flags above when the example starts; Settings shows, read only, the network (public, your own, or this computer only), the entry nodes of your own network (not the public ones) and the connection relays in use and where they came from (`-relay`, `-relay-file`, both, or `KOLO_BOARD_RELAYS`), each relay's state, `-find-timeout`, `-join-timeout`, `-lookup-interval` and `-notch-linger`.

## Settling

The board uses kolo-braid's defaults for everything that decides settling: the quorum rule (Majority), at least 4 and at most 7 settlers, any number of members, entries of up to 1 MiB and a digest check every 1024 settled positions. They are not settings of the example.

## Where data lives

The data directory holds `identity.key` (mode 0600), `settings.json`, `board.json`, the board's store in `board/` and the invites' store in `invites/`. It is created with mode 0700. An existing data directory open to other users is refused.

**Never copy a data directory or run a copy of it.** Two running copies of one member write two histories of that member's log: the board stops accepting changes from that copy (the window says so), and a settler that does it can stop settling for good.

## Several computers

With no flag, every copy uses the public network and the computers find each other wherever they are. For a network of your own, every copy lists the same entry nodes: kolo-dht servers all of them can reach, such as [kolo-node](https://github.com/ciranda-dev/kolo-node) servers of your own. Give the list with `-bootstrap` or `KOLO_BOOTSTRAP`:

```sh
KOLO_BOOTSTRAP=node1.example.org:7447,node2.example.org:7448 ./bin/kolo-board
```

With the public network or entry nodes that are not on loopback the example listens on `0.0.0.0:0`, so the other computers can connect. UDP must not be blocked between them. Members connect directly to each other, so they see each other's network addresses. When two members cannot, kolo-swarm connects them through a connection relay that both list ([your own servers](own-servers.md#run-a-connection-relay)); the board's sync runs on that connection as on a direct one, with the same key checks.
