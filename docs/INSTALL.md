# Installation

ComposeDrill is a single CLI binary. The easiest macOS path is Homebrew; Go and direct release archives remain supported installation paths.

## Homebrew (macOS)

```bash
brew install maliroq/tap/composedrill
```

Then verify the installed binary:

```bash
composedrill version
composedrill help
```

The third-party formula builds from the `v0.1.0-preview` source tag and pins the downloaded source archive with SHA-256.

## Go install

Requires Go 1.23 or newer:

```bash
go install github.com/maliroq/composedrill/cmd/composedrill@v0.1.0-preview
```

The binary is written to `GOBIN`, or to `$(go env GOPATH)/bin` when `GOBIN` is unset.

## Release archives

| Platform | Archive | Validation boundary |
| --- | --- | --- |
| macOS arm64 | `composedrill_0.1.0-preview_darwin_arm64.tar.gz` | Full real-Docker release gate performed on macOS arm64 with Docker Desktop |
| macOS amd64 | `composedrill_0.1.0-preview_darwin_amd64.tar.gz` | Packaged build target; no equivalent v0.1 real-Docker platform gate claimed |
| Linux amd64 | `composedrill_0.1.0-preview_linux_amd64.tar.gz` | Packaged build target; no equivalent v0.1 real-Docker platform gate claimed |
| Linux arm64 | `composedrill_0.1.0-preview_linux_arm64.tar.gz` | Packaged build target; no equivalent v0.1 real-Docker platform gate claimed |

Windows support is not claimed in v0.1.0-preview.

Download the target archive and `checksums.txt` from the same GitHub Release.

### Verify the archive

macOS example:

```bash
ARCHIVE="composedrill_0.1.0-preview_darwin_arm64.tar.gz"
grep "$ARCHIVE\$" checksums.txt | shasum -a 256 -c -
```

Linux example:

```bash
ARCHIVE="composedrill_0.1.0-preview_linux_amd64.tar.gz"
grep "$ARCHIVE\$" checksums.txt | sha256sum -c -
```

### Manual installation

macOS arm64:

```bash
tar -xzf composedrill_0.1.0-preview_darwin_arm64.tar.gz
mkdir -p "$HOME/.local/bin"
install -m 0755 composedrill "$HOME/.local/bin/composedrill"
"$HOME/.local/bin/composedrill" version
```

Linux amd64:

```bash
tar -xzf composedrill_0.1.0-preview_linux_amd64.tar.gz
mkdir -p "$HOME/.local/bin"
install -m 0755 composedrill "$HOME/.local/bin/composedrill"
"$HOME/.local/bin/composedrill" version
```

## Build from source

Requires Go 1.23 or newer:

```bash
go build -trimpath -buildvcs=false -o composedrill ./cmd/composedrill
./composedrill version
```

## Runtime prerequisites

`scan`, `plan`, and `manifest` do not require a Docker daemon.

`rehearse` requires:

- Docker Engine or Docker Desktop with a running daemon;
- Docker Compose v2 (`docker compose`);
- local access to the images used by the recovery stack and ComposeDrill's pinned utility/probe image, or registry access to pull them.

ComposeDrill never installs Docker or other system software automatically.
