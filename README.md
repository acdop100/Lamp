# LAMP: Local Asset Management Program

**LAMP** is an amatuer TUI app designed to automate the downloading & management of software assets such as desktop and android applications, ISO files, and ZIM files across different operating systems and architectures.

![LAMP UI](assets/LAMP.png)

- [LAMP: Local Asset Management Program](#lamp-local-asset-management-program)
  - [Key Features](#key-features)
  - [Documentation](#documentation)
  - [Extensibility](#extensibility)
  - [Getting Started](#getting-started)
    - [Prerequisites](#prerequisites)
    - [Installation](#installation)
      - [From Pre-built Binaries](#from-pre-built-binaries)
      - [From Source](#from-source)
    - [Configuration](#configuration)
  - [Usage](#usage)

## Key Features

- **Interactive TUI**: A high-performance terminal interface built with the Charmbracelet `bubbletea` framework.
- **Multi-Platform Support**: Manages assets for Windows, macOS, and Linux (AMD64/ARM64).
- **Project Gutenberg Integration**: Browse, search, and download thousands of public domain ebooks directly from LAMP.
- **Local Detection**: Automatically detects existing files on disk, even if you didn't download them with LAMP.
- **Selective Updates**: Check for updates and only download files that have a newer version available (`U` key).
- **Flexible Catalogs**: Supports GitHub Releases, RSS Feeds, Web Scraping, and more.

## Documentation

For detailed instructions on configuration, custom catalogs, and advanced usage, please see **[USAGE.md](USAGE.md)**.

## Extensibility

LAMP is built with a "Configuration-as-Code" philosophy. **You can easily expand the ecosystem by adding custom catalogs in the `catalogs/` directory.**

- **YAML Catalogs**: Define your own sources.
- **Dynamic Strategies**: Use built-in strategies like `github_release` or `web_scrape`.
- **Variable Expansion**: Automatically handle multiple OS/Arch targets with a single definition.

See [USAGE.md](USAGE.md#catalogs-system) for the full guide on creating custom catalogs.

## Getting Started

### Prerequisites

- [Go](https://go.dev/) 1.25 or later.

### Installation

You can install LAMP in one of two ways:

#### From Pre-built Binaries

Download the latest pre-built binary from the [releases page](https://github.com/acdop100/lamp/releases).
Releases are available for Windows, macOS, and Linux for AMD64 and ARM64 architectures.

#### From Source

Clone the repository and build the binary:

```bash
git clone https://github.com/acdop100/lamp.git
cd lamp
go build -o lamp
```

or via `go install`:

```bash
go install github.com/acdop100/lamp@latest
```

### Configuration

On the first run, LAMP will automatically create a configuration directory at:

| OS          | Path                                  |
| :---------- | :------------------------------------ |
| **Windows** | `%AppData%\lamp\`                     |
| **macOS**   | `~/Library/Application Support/lamp/` |
| **Linux**   | `~/.config/lamp/`                     |

Press `c` in the app to open this folder.

For full configuration options, including how to set up `config.yaml` and GitHub tokens, see [USAGE.md](USAGE.md#configuration).

## Usage

Launch the TUI:
```bash
./lamp
```

Run a quick status check via the `-check` flag:
```bash
$ ./lamp -check
Checking status of all monitored applications...
--------------------------------------------------
[Applications] BalenaEtcher [macos/amd64]: Local File Not Found [Latest: v2.1.4]
[Applications] BalenaEtcher [macos/arm64]: Local File Not Found [Latest: v2.1.4]
[Applications] BalenaEtcher [windows/amd64]: Local File Not Found [Latest: v2.1.4]
[Applications] Kiwix Desktop [windows/amd64]: Local File Not Found [Latest: 2.4.1]
[Applications] Kiwix Desktop [macos/universal]: Up to Date [3.11.0 -> 3.11.0]
...
```
