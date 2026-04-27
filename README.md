# loqsu

Command-line client for the [loq.su](https://loq.su) URL shortener.

![CI](https://github.com/PeoneEr/loqsu-cli/actions/workflows/ci.yml/badge.svg)
![Release](https://img.shields.io/github/v/release/PeoneEr/loqsu-cli)
![License](https://img.shields.io/github/license/PeoneEr/loqsu-cli)
![Go](https://img.shields.io/github/go-mod/go-version/PeoneEr/loqsu-cli)

Single-file Go binary, no external dependencies. Shorten a URL in one command, get a short link back. That's it.

## Quickstart

```bash
curl -fsSL https://loq.su/install.sh | sh
loqsu https://example.com/very/long/path
```

## Installation

### Install script (recommended)

```bash
curl -fsSL https://loq.su/install.sh | sh
```

Detects your OS and architecture, downloads the binary to `/usr/local/bin/loqsu`.

### Manual download

Download a tarball from [GitHub Releases](https://github.com/PeoneEr/loqsu-cli/releases), verify the checksum, and install:

```bash
VERSION=v0.1.0
OS=linux       # linux | darwin | windows
ARCH=amd64     # amd64 | arm64

curl -fsSL "https://github.com/PeoneEr/loqsu-cli/releases/download/${VERSION}/loqsu_${VERSION}_${OS}_${ARCH}.tar.gz" \
  -o loqsu.tar.gz

# Verify checksum
curl -fsSL "https://github.com/PeoneEr/loqsu-cli/releases/download/${VERSION}/SHA256SUMS.txt" \
  | grep "loqsu_${VERSION}_${OS}_${ARCH}.tar.gz" | sha256sum -c

tar -xzf loqsu.tar.gz
sudo mv loqsu /usr/local/bin/loqsu
```

Windows users: download the `.zip` archive from the same release page.

### go install

```bash
go install github.com/PeoneEr/loqsu-cli@latest
```

Note: the binary will be named `loqsu-cli` (the module name). Rename or symlink it if you want `loqsu`:

```bash
ln -s "$(go env GOPATH)/bin/loqsu-cli" /usr/local/bin/loqsu
```

### From source

```bash
git clone https://github.com/PeoneEr/loqsu-cli.git
cd loqsu-cli
go build -o loqsu .
```

### Docker

Not provided — this is a local CLI tool, install the binary directly.

## Usage

```
loqsu [flags] <url>
loqsu [flags] -          read URL from stdin
```

| Flag | Short | Description |
|------|-------|-------------|
| `--alias <slug>` | `-a` | Custom slug, 3–32 chars, `[a-zA-Z0-9_-]` |
| `--expires <value>` | `-e` | Expiration: RFC3339 timestamp or duration (`30d`, `12h`, `5m`, `60s`) |
| `--max-clicks <N>` | `-m` | Deactivate link after N clicks (0 = unlimited) |
| `--json` | `-j` | Print full JSON response instead of just the short URL |
| `--server <url>` | `-s` | API server base URL (default: `$LOQSU_SERVER` or `https://loq.su`) |
| `--version` | `-V` | Print version and exit |

**Environment:**

| Variable | Description |
|----------|-------------|
| `LOQSU_SERVER` | Override the default API server |

## Examples

```bash
# Basic shorten
loqsu https://example.com/very/long/path
# https://loq.su/abc123

# Custom alias
loqsu -a my-promo https://example.com/promo-page

# Expire after 30 days
loqsu -e 30d https://example.com

# Expire at a specific UTC moment
loqsu -e 2026-12-31T00:00:00Z https://example.com

# Limit to 100 clicks
loqsu -m 100 https://example.com

# Combine: custom alias + expiry + click cap
loqsu -a summer-sale -e 90d -m 1000 https://example.com/summer

# Read URL from stdin
echo https://example.com | loqsu -

# Get full JSON response and extract the slug with jq
loqsu --json https://example.com | jq -r .slug

# Use a self-hosted instance
LOQSU_SERVER=https://your-instance.example.com loqsu https://example.com
```

## Self-hosting

Point `loqsu` at any API-compatible server with `--server` or `LOQSU_SERVER`:

```bash
LOQSU_SERVER=https://your-instance.example.com loqsu https://example.com
# or per-call:
loqsu -s https://your-instance.example.com https://example.com
```

The server must implement the `POST /api/links` endpoint described below.

## API contract

Under the hood, `loqsu` makes a single POST request:

```bash
curl -s -X POST https://loq.su/api/links \
  -H 'content-type: application/json' \
  -d '{"url":"https://example.com"}'
```

Response (`201 Created`):

```json
{
  "slug":       "abc123",
  "short_url":  "https://loq.su/abc123",
  "target_url": "https://example.com",
  "expires_at": "2026-12-31T00:00:00Z",
  "max_clicks": 100,
  "created_at": "2026-04-27T12:00:00Z"
}
```

No authentication required. Rate limits apply per IP. Full API reference: [loq.su/docs](https://loq.su/docs).

## Building

Requires Go 1.26+. No external dependencies.

```bash
go build .
go test ./...
```

Release builds embed the version string via ldflags:

```bash
go build -ldflags "-s -w -X main.version=v0.1.0" -o loqsu .
```

## License

MIT. See [LICENSE](LICENSE).

## Links

- Service: https://loq.su
- API docs: https://loq.su/docs
- Terms of use: https://loq.su/terms
- Releases: https://github.com/PeoneEr/loqsu-cli/releases
- Issues: https://github.com/PeoneEr/loqsu-cli/issues

---

PRs welcome. For substantial changes, please open an issue first.
