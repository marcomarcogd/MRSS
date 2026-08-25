# MRSS

<p>
  <strong>English</strong> | <a href="README_zh.md">简体中文</a>
</p>

[![Release](https://img.shields.io/github/v/release/marcomarcogd/MRSS?label=release)](https://github.com/marcomarcogd/MRSS/releases/latest)
[![License](https://img.shields.io/badge/license-GPL--3.0-green.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go)](https://go.dev/)
[![Wails](https://img.shields.io/badge/Wails-v3-blue)](https://wails.io/)

MRSS is a privacy-focused, cross-platform desktop RSS reader with translation, local and AI summaries, feed discovery, automation, and integrations. Application data is stored locally, and this distribution does not include analytics or telemetry.

> [!IMPORTANT]
> **One-time manual upgrade:** releases v1.4.2 and earlier used the old repository identity and cannot automatically install v1.5.0. Download v1.5.0 manually from [MRSS Releases](https://github.com/marcomarcogd/MRSS/releases/latest). After that upgrade, in-app updates use the MRSS repository normally.

## Features

- RSS, Atom, OPML, XPath, script, and newsletter subscriptions
- Article translation and local TF-IDF/TextRank or cloud AI summaries
- Smart discovery, filters, rules, tags, image gallery, and full-text fetching
- FreshRSS, RSSHub, Obsidian, Notion, and Zotero integrations
- Light/dark themes, configurable interface and article typography, and keyboard shortcuts
- Portable desktop packages and an API-only server image

## Download

Download official fork builds from [GitHub Releases](https://github.com/marcomarcogd/MRSS/releases/latest):

- Windows: `MRSS-{version}-windows-{arch}-installer.exe`
- macOS: `MRSS-{version}-darwin-universal.dmg`
- Linux: `MRSS-{version}-linux-{arch}.AppImage`
- Portable packages: `MRSS-{version}-<platform>-<arch>-portable.*`
- Codex skill: `MRSS-{version}-skills.zip`

All distributed packages include the GPL-3.0 license and a source-code notice.

## Data migration

Normal-mode data is stored in:

- Windows: `%APPDATA%\MRSS\`
- macOS: `~/Library/Application Support/MRSS/`
- Linux: `~/.local/share/MRSS/`

On first launch, MRSS automatically migrates an existing `MrRSS` data directory when it is the only directory containing `rss.db`. If both old and new databases exist, MRSS uses the new directory and leaves the old one untouched. If an atomic rename fails, MRSS continues using the old directory and logs a warning without overwriting data.

Portable mode continues to use the adjacent `data/` directory. Server mode continues to use `./data`.

## Build from source

Requirements:

- Go 1.25+
- Node.js 24
- Wails CLI `v3.0.0-alpha2.117`
- Platform dependencies listed in [Build Requirements](docs/BUILD_REQUIREMENTS.md)

```bash
git clone https://github.com/marcomarcogd/MRSS.git
cd MRSS
go mod download
cd frontend && npm ci && cd ..
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-alpha2.117
task build
```

Run quality checks before submitting changes:

```bash
make check
pre-commit run --all-files
```

See [Contributing](CONTRIBUTING.md), [Code of Conduct](CODE_OF_CONDUCT.md), [Testing](docs/TESTING.md), and [Architecture](docs/ARCHITECTURE.md).

## Server and Docker

```bash
docker run -d -p 1234:1234 -v mrss-data:/app/data ghcr.io/marcomarcogd/mrss:latest
```

The local API is documented in [Swagger](docs/SERVER_MODE/swagger.json). The packaged Codex skill is documented in [Skills](docs/SKILLS.md).

## Fork attribution and license

MRSS is an unofficial modified fork based on [DevXDojo/MrRSS](https://github.com/DevXDojo/MrRSS). This distribution was modified on August 17, 2026. It is not endorsed by or affiliated with the upstream maintainers.

MRSS is distributed under the [GNU General Public License v3.0](LICENSE). The original license, copyright notices, and Git history are preserved. Source code for this distribution is available at [marcomarcogd/MRSS](https://github.com/marcomarcogd/MRSS). The software is provided without warranty; see the license for details.

Report fork-specific bugs through [MRSS Issues](https://github.com/marcomarcogd/MRSS/issues). Changes intended for upstream should follow the upstream project's contribution process separately.
