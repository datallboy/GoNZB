# GoNZB

A simple, high-performance Usenet downloader written in Go.

This is a work in progress and a personal project to learn more about Usenet clients and the NNTP protocol. Keeping a focus on simplicity.

## Features
- **CLI Based:** Simple CLI based NZB downloader with a low-memory footprint
- **Connection Pooling:** Pools providers to concurrently take advantage of max connections across all providers

## Possible Future Features
- **PAR2 Support**
- **Incomplete/Complete Downloads folders**
- **Web UI**
- **SQLite** - *Provider configs, partial download repairs*


# Configuration
GoNZB uses a `config.yaml` file to manage providers, download path, and log infomation.

```bash
cp config.yaml.example config.yaml
```

## Configuration Reference
| Section | Field | Description |
| :--- | :--- | :--- |
| **servers** | `id` | A unique nickname for the server (e.g., "NewsHosting"). |
| | `host` | The NNTP server address (e.g., `news.example.com`). |
| | `port` | Connection port. Usually `119` (Plain) or `563` (TLS). |
| | `username` | Your Usenet provider username. |
| | `password` | Your Usenet provider password. |
| | `tls` | Enables encrypted communication. Recommended for privacy. |
| | `max_connections`| The maximum threads allowed by your provider. |
| | `priority` | Lower numbers are used first. Use higher numbers for backup/block accounts. |
| **download** | `out_dir` | The directory where finished downloads and `.part` files are stored. |
| **log** | `path` | Path to the log file (e.g., `gonzb.log`). |
| | `level` | Logging verbosity: `debug`, `info`, `warn`, or `error`. |
| | `include_stdout` | If `true`, logs will appear in your terminal in addition to the log file. |

# Installation / Usage
```bash
make build
./bin/gonzb --file my_file.nzb
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
Map config.yaml and your downloads directory inside the container.
```sh
docker run --rm it \ 
    -v $(pwd)/config.yaml:/config/config.yaml
    -v $(pwd)/downloads:/downloads \
    gonzb:latest --file /downloads/test_file.nzb
```