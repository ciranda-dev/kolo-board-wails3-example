# Your own servers

What Kolo Board needs from the network, and how to run it yourself.

## What the example uses

| Server | Kolo Board | Point the example at it with |
|---|---|---|
| Entry node (`kolo-node run`) | not needed: the public network is the default; your own with `-bootstrap` | `-bootstrap HOST:PORT[,HOST:PORT...]` or `KOLO_BOOTSTRAP` |
| Relay (`kolo-node relay`) | optional | `-relay HOST:PORT@KEY[,...]`, `-relay-file FILE` or `KOLO_BOARD_RELAYS` |
| Pin (`kolo-pin run`) | not used | nothing |

With no flag the example uses the public network: entry nodes run by Ciranda, free, with no uptime guarantee while Kolo is a beta. They are entry nodes only: there is no public relay and no public pin, and the example never picks one for you. `-local` uses no server at all: this computer only.

## When you need one

- **Your own entry node**: for a network that only your team uses, or one that does not depend on the public servers. With `-bootstrap` the example uses your entry nodes alone and never the public ones.
- **A relay**: two computers behind strict (symmetric) NATs, or behind a firewall that drops unknown UDP, cannot connect directly. They meet through a relay both list: the invite, the join and the board's sync go through it, encrypted end to end. The relay sees the members' kolo-ids and network addresses, who connects to whom, when, and how much; it cannot read the board. A direct connection is always tried first.
- **A pin**: keeps encrypted data for people who are online at different times. Kolo Board uses none: members sync straight with each other, so changes travel only between members who are online at the same time.

## Run an entry node

Run it on a host every member reaches: a public IPv4 address, such as a VPS, or a router that forwards UDP port 7447 to it.

Install kolo-node (in `$(go env GOPATH)/bin`, which must be on your `PATH`).

```sh
go install github.com/ciranda-dev/kolo-node/cmd/kolo-node@v0.1.1
```

The first entry node. It creates its key file with mode 0600 on the first run and keeps it for the next runs.

```sh
kolo-node run -listen 0.0.0.0:7447 -identity "$HOME/kolo-node/node.key" -first
```

A second entry node on another host, so the network stays up when one stops. Replace `<node-1>` with the first node's address.

```sh
kolo-node run -listen 0.0.0.0:7447 -identity "$HOME/kolo-node/node.key" -bootstrap <node-1>:7447
```

Each computer: check that the node answers. It prints `ok <node-1>:7447`.

```sh
kolo-node check <node-1>:7447
```

Each computer: start the example with the same list.

```sh
./bin/kolo-board -bootstrap <node-1>:7447,<node-2>:7447
```

`KOLO_BOOTSTRAP=<node-1>:7447,<node-2>:7447` does the same; a flag wins over the variable. The [kolo-node deploy guide](https://github.com/ciranda-dev/kolo-node/blob/main/docs/deploy.md) has a systemd unit and the firewall rules for a server.

## Run a connection relay

Run it on a host every member reaches, on another UDP port than the entry node. It creates its key file with mode 0600 on the first run and prints its key (`identity key=...`) at every start.

```sh
kolo-node relay -listen 0.0.0.0:7448 -identity "$HOME/kolo-node/relay.key"
```

Each computer: start the example with the relay, `host:port@key` with the key as 64 hex characters.

```sh
./bin/kolo-board -bootstrap <node-1>:7447 -relay <relay-host>:7448@<relay-key>
```

Every member lists the same relay: two members meet only through a relay both list, and the invite does not carry it. `-relay` repeats or takes a comma list, and `-relay-file` reads a file with one or more relays per line, `#` starting a comment (at most 16 relays in all, IPv4 or a host name). `KOLO_BOARD_RELAYS` is read only when neither flag is given.

Settings shows each relay, where the list came from, and its state: used to reach others, holding a place (a member that has a board holds one, so others reach it through the relay), or why it cannot be used. At start the example checks each relay's key with one handshake, as a throwaway key that asks the relay for nothing, so a wrong key is shown within seconds, also to someone who is joining; the terminal says each failure once.

`kolo-node relay -allow-file FILE` takes the hex keys that may hold a place there, one per line. It decides only that: any key can still connect through the relay to a member that holds a place. With an allow list, put every member's kolo-id in it (the example prints `kolo-id` in the terminal when it connects); a member that is not in it is told so in Settings. The relay's limits (`-relay-max-session-bytes`, `-relay-max-session-duration`, `-relay-rate-bytes` and the others) are listed by `kolo-node relay -h`.

## Ports

- Entry node: UDP 7447 in, in the host's firewall and in the provider's. With ufw: `sudo ufw allow 7447/udp`.
- Relay: UDP 7448 in, the same way.
- The example: UDP on a port the system picks; `-listen 0.0.0.0:PORT` sets a fixed one to open in a firewall.
- No TCP port is used.
