# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is `subs-check` - a subscription detection and conversion tool written in Go that aggregates, tests, and converts proxy/VPN subscriptions. The tool checks node availability, performs speed tests, detects streaming media unlock capabilities, and provides various subscription format outputs.

## Build Commands

```bash
# Build for current platform
make build
# or
go build -o subs-check

# Build for all platforms
make build-all

# Platform-specific builds
make linux-amd64
make linux-arm64  
make darwin-amd64
make darwin-arm64
make windows-amd64

# Clean build artifacts
make clean
```

## Development Commands

```bash
# Run directly from source
go run . -f ./config/config.yaml

# Run tests (only one test file exists)
go test ./save/method/...

# Format and vet code (no explicit commands defined, use standard Go tools)
go fmt ./...
go vet ./...
go mod tidy
```

## High-Level Architecture

### Core Components

1. **Main Entry** (`main.go`) - Initializes the application with version info
2. **App Management** (`app/`) - Handles application lifecycle, configuration watching, HTTP server, and scheduling
3. **Proxy Management** (`proxy/`) - Fetches and parses subscriptions from multiple sources
4. **Checking Engine** (`check/`) - Tests proxy availability, speed, and streaming capabilities
5. **Save System** (`save/`) - Handles output in multiple formats and storage backends
6. **Platform Detection** (`check/platform/`) - Detects streaming service accessibility per region
7. **Configuration** (`config/`) - YAML-based configuration management
8. **Assets** (`assets/`) - Embedded resources and Sub-Store service integration
9. **Utilities** (`utils/`) - Notifications, callbacks, and helper functions

### Data Flow

1. **Subscription Fetching**: Retrieves proxy lists from configured URLs (local + remote sources)
2. **Node Processing**: Deduplicates, validates, and normalizes proxy configurations  
3. **Availability Testing**: Tests each proxy for basic connectivity and response time
4. **Speed Testing**: Downloads test files to measure bandwidth performance
5. **Streaming Detection**: Tests access to Netflix, YouTube, Disney+, OpenAI, etc.
6. **Geolocation**: Determines proxy location and ASN information
7. **Output Generation**: Saves results in multiple formats (YAML, Base64, etc.)
8. **Storage**: Supports local files, R2, Gist, WebDAV, and S3 backends

### Key Features

- **Concurrent Processing**: Configurable thread count for parallel proxy testing
- **Scheduling**: Supports both interval-based and cron expression scheduling
- **Real-time Monitoring**: Web UI for manual triggering and result viewing
- **Multiple Formats**: Outputs for Clash, V2Ray, Sing-Box, and other clients
- **Notification System**: 100+ notification channels via Apprise integration
- **Sub-Store Integration**: Embedded subscription conversion service
- **Memory Management**: Built-in memory monitoring and cleanup

### Configuration

Primary config file: `config/config.yaml` (created from `config.example.yaml`)

- Subscription URLs and remote lists
- Testing parameters (timeout, speed thresholds, concurrent threads)
- Output formats and storage methods
- Notification settings
- Web UI and Sub-Store port configuration

### HTTP Endpoints

- `:8199/admin` - Web control panel
- `:8199/sub/` - File serving for generated subscriptions  
- `:8299/` - Sub-Store service (subscription conversion)
- Various subscription format endpoints with `target` parameter support
