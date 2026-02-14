# GoNZB

A high-performance, unified Usenet engine written in Go. 

GoNZB combines a powerful NNTP downloader with a Newznab-compatible API server, providing a headless solution for both manual downloads and automated media management (via Prowlarr, Sonarr, Radarr, etc.).

## Key Features
- **Unified Engine Architecture:** A shared core handles downloads whether triggered via CLI, Newznab API, or manual NZB uploads.
- **Persistent Headless Queue:** All downloads are managed by a background queue system and persisted in SQLite, ensuring jobs survive restarts.
- **Newznab Compatible API:** Built-in server that supports searching, capabilities, and NZB fetching.
- **Local Store Indexer:** Search and retrieve NZBs previously added to the local blob store via the Newznab API.
- **Blob Storage:** Automatic caching of NZBs in a local blob store for reliable failover and repeat downloads.
- **Connection Pooling & Failover:** Concurrent segment fetching across multiple providers with priority-based failover logic.
- **Post-Processing:** Built-in PAR2 repair and extraction for RAR, ZIP, and 7Z archives.
- **Visual CLI Progress:** Real-time download speed, progress bar, and ETA for terminal usage.

## Architecture
GoNZB is designed around a "Single Source of Truth" architecture:
1. **Metadata Database (SQLite):** Stores information about all "Releases" (from indexers or manual uploads), the download queue, and individual file tasks.
2. **Blob Store:** Stores the raw NZB files, which are fetched as needed by the download engine.
3. **Unified Engine:** Orchestrates the worker pool, connection management, and segment fetching.

# Configuration
GoNZB uses a `config.yaml` file to manage providers, indexers, and storage paths.

```bash
cp config.yaml.example config.yaml
```

## Configuration Reference
| Section | Field | Description |
| :--- | :--- | :--- |
| **global** | `port` | The port for the HTTP API server (Default: `8080`). |
| **servers** | `id` | A unique nickname for the server (e.g., "NewsHosting"). |
| | `host` | The NNTP server address (e.g., `news.example.com`). |
| | `port` | Connection port. Usually `119` (Plain) or `563` (TLS). |
| | `username` | Your Usenet provider username. |
| | `password` | Your Usenet provider password. |
| | `tls` | Enables encrypted communication (SSL/TLS). |
| | `max_connections`| The maximum threads allowed for this provider. |
| | `priority` | Priority level (Lower = Higher priority). |
| **indexers**| `id` | Identifier for external Newznab indexers. |
| | `base_url` | The API URL for the indexer. |
| | `api_key` | Your API key for the indexer. |
| | `redirect` | If true, redirects download requests directly to indexer URL. |
| **download** | `out_dir` | Directory for active/temporary downloads (`.part` files). |
| | `completed_dir` | Final destination for extracted and verified files. |
| | `cleanup_extensions` | File extensions to delete after success (e.g., `[".par2", ".rar"]`). |
| **log** | `path` | Path to the log file (e.g., `gonzb.log`). |
| | `level` | Verbosity: `debug`, `info`, `warn`, or `error`. |
| | `include_stdout` | If `true`, logs also appear in the terminal. |
| **store** | `sqlite_path` | Path to the SQLite metadata database. |
| | `blob_dir` | Directory where raw NZB files are stored. |

# Usage

## CLI Download
Manually trigger a download of a specific NZB file:
```bash
make build
./bin/gonzb --file my_file.nzb
```

## API Server
Start the Newznab-compatible API server and background queue:
```bash
./bin/gonzb serve
```

## Docker
### Docker Build
```sh
docker build \
  --build-arg VERSION=$(git describe --tags --always) \
  --build-arg BUILD_TIME=$(date -u +'%Y-%m-%dT%H:%M:%SZ') \
  -t gonzb:latest .
```

### Docker Usage
Mount your `config.yaml` and download/store directories. Store config within config a config directory.
```sh
docker run -d \
    --name gonzb \
    -p 8080:8080 \
    -v $(pwd)/config:/config \
    -v $(pwd)/downloads:/downloads \
    -v $(pwd)/store:/store \
    gonzb:latest serve
```

## Troubleshooting

### Config file not found in Docker
If you see `Config error: config file not found: config.yaml`, it's because the application is looking in the container's working directory (`/app`) instead of where you mounted it (`/config`). 

Ensure you are passing `--config /config/config.yaml` **before** or **after** the subcommand:
`gonzb --config /config/config.yaml serve`

