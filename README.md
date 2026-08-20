# MetroShell

## Delhi Metro, rendered in your terminal.

Imagine the Delhi NCR and its metro network the way Google Maps shows it. Now imagine that on your terminal, rendered entirely in braille characters, interactive, with real metro journey planning between any two stations. That's MetroShell, built solo with my manager of agents, ao. 

![MetroShell v1.0.0 demo](https://github.com/adot-7/metroshell/releases/download/v1.0.0/demo.gif)

## Try it instantly

No install or data download is needed for the public demo:

```sh
ssh metroshell.akashparashar.dev
```

## v1.0.0 downloads

GoReleaser creates the following archives for tag `v1.0.0`. The version segment
is `1.0.0` (the leading `v` is the release tag, not part of the archive name).

### Local terminal application

| Platform | Archive |
| --- | --- |
| Linux x86-64 | [`metroshell_1.0.0_linux_amd64.tar.gz`](https://github.com/adot-7/metroshell/releases/download/v1.0.0/metroshell_1.0.0_linux_amd64.tar.gz) |
| Linux ARM64 | [`metroshell_1.0.0_linux_arm64.tar.gz`](https://github.com/adot-7/metroshell/releases/download/v1.0.0/metroshell_1.0.0_linux_arm64.tar.gz) |
| macOS Intel | [`metroshell_1.0.0_darwin_amd64.tar.gz`](https://github.com/adot-7/metroshell/releases/download/v1.0.0/metroshell_1.0.0_darwin_amd64.tar.gz) |
| macOS Apple Silicon | [`metroshell_1.0.0_darwin_arm64.tar.gz`](https://github.com/adot-7/metroshell/releases/download/v1.0.0/metroshell_1.0.0_darwin_arm64.tar.gz) |
| Windows x86-64 | [`metroshell_1.0.0_windows_amd64.zip`](https://github.com/adot-7/metroshell/releases/download/v1.0.0/metroshell_1.0.0_windows_amd64.zip) |

### Linux SSH server

| Platform | Archive |
| --- | --- |
| Linux x86-64 | [`metroshell-sshserver_1.0.0_linux_amd64.tar.gz`](https://github.com/adot-7/metroshell/releases/download/v1.0.0/metroshell-sshserver_1.0.0_linux_amd64.tar.gz) |
| Linux ARM64 | [`metroshell-sshserver_1.0.0_linux_arm64.tar.gz`](https://github.com/adot-7/metroshell/releases/download/v1.0.0/metroshell-sshserver_1.0.0_linux_arm64.tar.gz) |

All checksums are in [`checksums.txt`](https://github.com/adot-7/metroshell/releases/download/v1.0.0/checksums.txt).
There is no GoReleaser Windows ARM64 archive, and the SSH server is built for
Linux only. The release workflow publishes these binaries and checksums; it
does not fetch or manufacture map, GTFS, or demo media assets.

## Install or build from source

The module pins Go `1.26.2` in [`go.mod`](go.mod). To install the two Go
commands from the release tag:

```sh
go install github.com/adot-7/metroshell@v1.0.0
go install github.com/adot-7/metroshell/cmd/sshserver@v1.0.0
```

Or build from a checkout. These are the same two entry points used by the
release configuration:

```sh
git clone https://github.com/adot-7/metroshell.git
cd metroshell
CGO_ENABLED=0 go build -o metroshell ./
CGO_ENABLED=0 go build -o metroshell-sshserver ./cmd/sshserver
```

Both commands need local map data at runtime. The SSH server additionally needs
an SSH host key. Do not put host keys, credentials, archives, or generated
outputs in the repository.

## Release data setup

The v1.0.0 data files are separate GitHub Release assets, not source files:

- [`delhi-ncr.mbtiles`](https://github.com/adot-7/metroshell/releases/download/v1.0.0/delhi-ncr.mbtiles)
- [`DMRC_GTFS.zip`](https://github.com/adot-7/metroshell/releases/download/v1.0.0/DMRC_GTFS.zip)

Download them into the ignored `mapdata/` directory when preparing a local
demo:

```sh
mkdir -p mapdata
curl -fL -o mapdata/delhi-ncr.mbtiles \
  https://github.com/adot-7/metroshell/releases/download/v1.0.0/delhi-ncr.mbtiles
curl -fL -o mapdata/DMRC_GTFS.zip \
  https://github.com/adot-7/metroshell/releases/download/v1.0.0/DMRC_GTFS.zip
```

The GTFS ZIP is a static snapshot. It must contain `stops.txt`, `routes.txt`,
`trips.txt`, `stop_times.txt`, and `shapes.txt` at its root. The release
workflow cannot access a maintainer's local archives, so the map and GTFS files
must be uploaded manually to the v1.0.0 GitHub Release. The demo GIF is also a
manual release asset; none of these three assets belong in Git or CI.

## Run locally

Map-only mode:

```sh
./metroshell mapdata/delhi-ncr.mbtiles
```

Map plus the local static GTFS snapshot:

```sh
./metroshell mapdata/delhi-ncr.mbtiles mapdata/DMRC_GTFS.zip
```

The equivalent source commands are:

```sh
go run . mapdata/delhi-ncr.mbtiles
go run . mapdata/delhi-ncr.mbtiles mapdata/DMRC_GTFS.zip
```

The feed loads asynchronously. A missing or invalid feed leaves the base map
usable and reports `GTFS: missing` or `GTFS: error`; it never presents a
partially validated feed.

## Controls

MetroShell is keyboard-first. FROM/TO station selection is keyboard-only:
mouse clicks, releases, and pointer motion do not select stations or create a
map cursor. The mouse wheel is reserved for zoom.

| Key | Action |
| --- | --- |
| `Enter` | Dismiss the launch splash; otherwise open the focused FROM/TO picker. With map focus on a ready route, expand or collapse the selected journey leg. |
| `Tab` / `Shift-Tab` | Cycle map, FROM, and TO focus. |
| `↑` / `↓` or `Ctrl-J` / `Ctrl-K` in picker | Move through station results. |
| Type text / `Backspace` in picker | Filter stations / edit the search. |
| `Enter` in picker | Choose the highlighted station. FROM selection advances to the TO picker. |
| `Esc` | Cancel the picker, collapse an expanded leg, or clear the focused endpoint. |
| `Backspace` outside picker | Clear the focused endpoint. |
| `↑` / `↓` or `j` / `k` with map focus and a ready route | Select the previous or next journey leg. |
| `e` | Expand or collapse the selected journey leg (compatibility alias). |
| `w` `a` `s` `d` or `←` `→` `h` `l` | Pan the map. |
| `+` / `=` / `,` and `-` / `_` / `.` | Zoom the map from the keyboard. |
| Mouse wheel | Zoom the map only. |
| `?` | Open or close the bounded help overlay. |
| `r` | Retry a missing or invalid configured GTFS feed. |
| `q` / `Ctrl-C` | Quit. |

## What the visuals mean

- **Map:** a local MBTiles Delhi NCR base map rendered as terminal braille.
- **Metro layer:** line geometry and station markers projected from the local
  static GTFS snapshot. A route uses a deterministic fewest-stop BFS, not a
  travel-time, fare, accessibility, or crowding optimizer.
- **Journey:** the sidebar shows line legs, stops, transfers, and centered
  `JOURNEY` / `SCHEDULED` headings when a route is ready.
- **Clock:** the sidebar clock is Delhi-local wall time and includes seconds;
  it has no `DELHI` prefix and is not the simulator clock.
- **`NEXT SERVICE`:** an **offline timetable** calculation from static GTFS stop
  times and calendar rules in Delhi local time. It is not a live departure
  board, realtime DMRC telemetry, service alert, or network lookup. An expired
  calendar may be carried forward for demo continuity and is marked as
  estimated in the journey view.
- **Moving trains:** deterministic dots moving along prepared GTFS shapes using
  schedule-derived durations. The default internal presentation pace is 15x;
  this is an offline visual simulation, never live vehicle positions or realtime
  service.
- **Identity:** normal terminals open on a large pixel-art `METROSHELL`
  wordmark, metro-train emblem, and tiny network diagram inside the bounded
  magenta AMOLED shell. The launch screen also reports the current static GTFS
  startup state before Enter is pressed. Medium terminals keep the wordmark;
  compact terminals reduce to the exact `METROSHELL`, `DELHI METRO STARTING IN
  YOUR TERMINAL`, and `built by Akash Parashar` copy. Neutral map/sidebar shells
  retain pink MetroShell identity accents after launch.

## Tips

- Set the terminal background to `#000000` for the intended AMOLED-style look.
- Give the app at least 52 columns for the full sidebar. Very small terminals
  keep the UI bounded and pause or reduce train motion for readability.
- Local and SSH sessions share the same controls and rendering behavior, but
  each SSH deployment still needs its own host key and local mounted data.
- Runtime is offline after the local MBTiles and GTFS files are supplied. A
  local snapshot is not a guarantee of current DMRC service information.

<!--
## SSH server: maintainer setup

The server entry point accepts the flags shown below. The map path is required;
GTFS is optional. The server listens on `:2222` by default.

```sh
CGO_ENABLED=0 ./metroshell-sshserver \
  --addr :2222 \
  --host-key /path/to/ssh_host_ed25519_key \
  --tiles mapdata/delhi-ncr.mbtiles \
  --gtfs mapdata/DMRC_GTFS.zip
```

For the checked-in container setup, build from the repository root so the
Dockerfile can copy `go.mod`, `go.sum`, and the source tree:

```sh
docker build -f Dockerfile.sshserver -t metroshell-sshserver .
docker run -d --name metroshell-sshserver --restart unless-stopped \
  -p 22:2222 \
  -v "$PWD/ssh_host_ed25519_key:/app/ssh_host_ed25519_key:ro" \
  -v "$PWD/mapdata:/app/mapdata:ro" \
  metroshell-sshserver \
  --addr :2222 \
  --host-key /app/ssh_host_ed25519_key \
  --tiles /app/mapdata/delhi-ncr.mbtiles \
  --gtfs /app/mapdata/DMRC_GTFS.zip
```

The image exposes and listens on container port `2222`; `-p 22:2222` makes it
available on the host's standard SSH port. The host key is mounted read-only,
and `mapdata/` is mounted read-only. The current image uses the default Alpine
runtime user because the application writes its `trip.log` in `/app`; do not
assume a non-root image without separately testing writable logs and host-key
permissions.

The checked-in deploy workflow connects to the EC2 host on administrative port
`2222`, then runs the container with `-p 22:2222`, mounting
`~/ssh_host_ed25519_key` and `~/mapdata`. That workflow does not download
release assets and invokes the image's default map-only command unless its run
arguments are deliberately extended with `--gtfs mapdata/DMRC_GTFS.zip`.
Oracle/DNS provisioning, host-key fingerprint verification, and firewall setup
are deployment-owner responsibilities; never place their secrets in this
repository. Verify the SSH host-key fingerprint out of band and keep strict
host-key checking enabled.
-->
## Architecture

The local executable in [`main.go`](main.go) opens the required MBTiles path and
passes the optional GTFS path to the shared Bubble Tea v2 model. The SSH entry
point in [`cmd/sshserver`](cmd/sshserver) creates that same model per Wish
session while sharing a tile cache.

The main layers are:

1. `internal/tiles` reads local SQLite MBTiles and gzip-compressed vector tiles.
2. `internal/render` decodes tile geometry and composes the braille terminal
   frame with metro lines, stations, route highlights, and simulated trains.
3. `internal/gtfs` parses and validates the five required static GTFS tables,
   builds deterministic station/line/shape indexes, schedules, and a route
   graph.
4. `internal/app` owns focus, the picker, journey-leg detail, overlays,
   asynchronous feed loading, viewport state, and local/SSH interaction parity.
5. `internal/sim` produces deterministic schedule-shaped train snapshots from
   explicit seed, clock, and route inputs.

All map and feed reads are local. The application does not poll DMRC or a
network service at runtime.

## Roadmap

The v1.0.0 boundary is intentionally narrow. Possible future work includes:

- richer route objectives such as travel time, fares, accessibility, or
  crowding, instead of only fewest stops;
- better data provenance, refresh tooling, and packaging for offline snapshots;
- additional terminal presentation and portability improvements; and
- a separately designed realtime-data integration, if ever pursued. Realtime
  positions, service status, and alerts are not part of v1.0.0.

See the [roadmap](docs/ROADMAP.md), [architecture notes](docs/ARCHITECTURE.md),
and [static data notes](docs/DATA.md) for the maintained boundaries.

## License, attribution, and credit

This repository does not currently include a `LICENSE` file, so v1.0.0 does not
assert an open-source license for the source. Do not assume that the map or GTFS
release assets have the same terms as the code. Preserve the attribution and
redistribution terms supplied with each data asset; the MBTiles provider may
require OpenStreetMap/OpenMapTiles attribution, and the GTFS provider may have
separate terms.

The visible product credit is **Akash Parashar**. MetroShell's static data is
distributed as release assets rather than committed archives; record the actual
provider, retrieval date, version, and license in the maintainer's provenance
notes as described in [`docs/DATA.md`](docs/DATA.md).

For release and CI details, see [`docs/TESTING_AND_CI.md`](docs/TESTING_AND_CI.md).
