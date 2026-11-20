# Try it

The README's [Try it](../README.md#try-it) section has the commands, one per block. This page says what each part does, which ports to open and what to check when two copies do not see each other. Every flag is in [configuration](configuration.md).

## What runs where

| Part | Command | Listens on | Needed by |
|---|---|---|---|
| Entry node, public network (the default) | runs on Ciranda's servers, nothing to run | UDP 7447 on each public server | every copy that has no network flag |
| Entry node, your own network | `kolo-node run` | UDP 7447 | every copy, through `-bootstrap` |
| Entry node, this computer only | the first copy started with `-local` | UDP `127.0.0.1:7451` | every copy started with `-local` |
| Example | `./bin/kolo-board` | UDP `0.0.0.0:0` on the public network or with entry nodes off loopback, else `127.0.0.1:0` | |

The three networks are separate: copies meet only on the same one. The public network is the default: run by Ciranda, free, with no uptime guarantee while Kolo is a beta. `-bootstrap` uses your own entry nodes alone. `-local` serves one computer, and its entry node stops when the example that runs it quits. `-first` runs an entry node of a test network inside a copy, on a loopback address of your choice.

The entry nodes of `-bootstrap` are a list: `-bootstrap` and `KOLO_BOOTSTRAP` take a comma list of `host:port` (IPv4 or a host name, at most 16). Settings shows the network and, for your own network, the list in use and where it came from (it does not list the public entry nodes); it is set only with these flags, not in the window. Kolo Board uses no pin: members sync with each other while they are online. Connection relays are set the same way, with `-relay`, `-relay-file` or `KOLO_BOARD_RELAYS` ([your own servers](own-servers.md#run-a-connection-relay)).

To start over, quit the copies, stop kolo-node with Ctrl-C, and delete `/tmp/kolo-board-demo`.

## Same computer

Start every copy with `-local`. Everything stays on loopback: nothing is reachable from another computer, and no firewall rule is needed. One terminal per person, as the README shows; the first copy runs the entry node on `127.0.0.1:7451` and the others join it, so keep it open. If another program holds that port, the window says so. Each person needs a data directory of their own; never run two copies on one, or a copy of one.

## Two computers, anywhere

Start the example on each computer with no network flag. Both use the public network, so they find each other on one network or on two, with nothing to install or open. They connect straight to each other through their routers; some networks do not allow that (two carrier-grade NATs, a firewall that drops unknown UDP), and then a connection relay that you run carries the connection (see below).

If the public network cannot be reached at start, the window says so and names the other two options: `-bootstrap` for your own network, `-local` for this computer only. Check the internet connection and press Try again.

## Your own network, on the same network

Computer A runs the entry node and Ana; computer B runs Bia. Both start with `-bootstrap`.

Find A's LAN address, the `<ip-of-computer-a>` of the README:

- macOS: `ipconfig getifaddr en0` (Wi-Fi; `en1` or another interface for a cable on some Macs).
- Linux: `hostname -I`, the first IPv4 address.
- Windows: `ipconfig`, the "IPv4 Address" line.

kolo-node logs `started` with `addr=0.0.0.0:7447`: that is the socket it listens on, not an address to give to B. Give B `<ip-of-computer-a>:7447`.

Open the port on computer A:

- Linux with ufw: `sudo ufw allow 7447/udp`.
- macOS with the firewall on: allow incoming connections for kolo-node and Kolo Board when macOS asks, or in System Settings, Network, Firewall, Options.
- Windows: allow kolo-node and Kolo Board when Windows asks, or add a rule for UDP 7447 in Windows Defender Firewall.

The copies connect straight to each other, on a port the system picks. If a firewall blocks incoming UDP on A or B, give the example a fixed port, such as `-listen 0.0.0.0:7460`, and open it the same way.

Check from computer B:

```sh
kolo-node check <ip-of-computer-a>:7447
```

It prints `ok <ip-of-computer-a>:7447`, or `failed` with the reason. It proves that the entry node answers over UDP from B, which `ping` or a browser cannot.

## Your own network, on different networks

Only the address changes: `<ip-of-computer-a>` must be a public IPv4 address or a host name that B reaches. Two ways:

- Run `kolo-node run` on a host with a public address, such as a VPS, with UDP 7447 open in its firewall and in the provider's. kolo-node's [deploy guide](https://github.com/ciranda-dev/kolo-node/blob/main/docs/deploy.md) has a systemd unit and the firewall rules. Both computers then use the VPS address in `-bootstrap` or `KOLO_BOOTSTRAP`.
- Forward UDP port 7447 on A's router to A, and use the router's public address, which the router's admin page shows. This does not work when the provider puts A behind a shared address (carrier-grade NAT).

For a network that stays up, run two kolo-node entry nodes on hosts that stay on and list both, such as `-bootstrap <node-1>:7447,<node-2>:7447`.

## A relay behind strict NATs

The entry node helps the two copies open a path through their routers, then they talk straight to each other. Some networks do not allow that, such as two carrier-grade NATs or a firewall that drops unknown UDP. A connection relay (`kolo-node relay`) on a host both reach carries their connection then: start both copies with the same `-relay host:port@key`. There is no public relay and the example never chooses one; you run your own. How to run one, and entry nodes that stay up, is in [your own servers](own-servers.md).

## It does not connect

- Does the entry node answer? `kolo-node check <entry-node-address>` prints `ok` on every computer.
- Is UDP allowed? Port 7447 is open on the computer or host that runs kolo-node, and in the provider's firewall for a VPS.
- Do both copies use the same network? All with no flag, all with the same `-bootstrap` list, or all with `-local`; Settings shows the network and, for your own network, the entry nodes in use.
- The public network: the window names the three options when it cannot be reached. Check the internet connection and that UDP is not blocked. It has no uptime guarantee while Kolo is a beta.
- Is the entry node on loopback? `-local`, `-first` and `127.0.0.1` work on one computer only.
- Your own network on different networks: the entry node needs a public address or a port forward. Behind networks that block a direct path, both copies need the same connection relay; Settings shows each relay's state, such as a key that is not the relay's.
- A join that keeps looking: the person who made the invite must have the example open, on the same network.
