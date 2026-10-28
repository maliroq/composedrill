# Installation

ComposeDrill is distributed as a single static binary. Go is not required to run a release build.

## Release archives

Preview releases are packaged for:

| Archive target | Packaging status | Runtime evidence boundary |
| --- | --- | --- |
| macOS arm64 | Built | Full real-Docker release gate performed on macOS arm64 |
| macOS amd64 | Built | Cross-built; no independent v0.1 Docker platform gate |
| Linux amd64 | Built | Release workflow runs Docker integration on GitHub's Linux amd64 runner before publishing |
| Linux arm64 | Built | Cross-built; no independent v0.1 Docker platform gate |

Windows is not claimed as supported in v0.1.0-preview.

## Verify the archive

Download the target archive and `checksums.txt` from the same GitHub Release.

macOS:

```bash
shasum -a 256 -c checksums.txt
```

Linux:

```bash
sha256sum -c checksums.txt
```

The checksum file covers every published archive.

## Install

Example for macOS arm64:

```bash
tar -xzf composedrill_0.1.0-preview_darwin_arm64.tar.gz
install -m 0755 composedrill "$HOME/.local/bin/composedrill"
composedrill version
```

Example for Linux amd64:

```bash
tar -xzf composedrill_0.1.0-preview_linux_amd64.tar.gz
install -m 0755 composedrill "$HOME/.local/bin/composedrill"
composedrill version
```

If `$HOME/.local/bin` is not on your `PATH`, choose another user-controlled directory that is.

## Build from source

Requires Go 1.23 or newer:

```bash
go build -trimpath -buildvcs=false -o composedrill ./cmd/composedrill
./composedrill version
```

## Runtime prerequisites

`scan`, `plan`, and `manifest` do not require a Docker daemon. `rehearse` requires:

- Docker Engine / Docker Desktop with a running daemon;
- Docker Compose v2 (`docker compose`);
- local access to the images used by the recovery stack and ComposeDrill's pinned utility/probe image, or registry access to pull them.

ComposeDrill never installs Docker or other system software automatically.
