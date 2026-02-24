# GoNZB Architecture Overview

GoNZB is a high-performance, unified Usenet engine written in Go, designed to serve as both a CLI downloader and a Newznab-compatible API server. This document outlines its core architectural components and their interactions.

## Core Principles

- **Single Source of Truth (SSOT):** Centralized management of application state and data, primarily through the `app.Context` and the `Store`.
- **Modularity and Decoupling:** Components are designed as interfaces, promoting loose coupling and enabling easier testing, maintenance, and future extensibility.
- **Concurrency:** Leverages Go's concurrency model for efficient, non-blocking operations, especially in downloading and post-processing.
- **Persistence:** All critical data (releases, queue items) are persisted in a SQLite database, ensuring job resilience across application restarts.

## High-Level Architecture

GoNZB operates with a clear separation of concerns, primarily divided into command-line interface (CLI) and API server modes, both powered by a shared internal engine.

```
+------------------+     +-------------------+
|      CLI Mode    |     |    API Server     |
| (executeDownload)|     | (executeServer)   |
+--------+---------+     +---------+---------+
         |                       |
         |                       |
         v                       v
+------------------------------------------------+
|               main.go (setupApp)               |
|  Initializes core application dependencies     |
+------------------------------------------------+
         |
         v
+------------------------------------------------+
|                 app.Context                    |
|  Dependency container for core services        |
|  (NNTP, Indexer, Processor, Downloader, Queue, |
|   NZBParser, Store, Logger, Config)            |
+------------------------------------------------+
         |
         v
+------------------------------------------------+
|                 Engine Package                 |
|  (internal/engine)                             |
|  Orchestrates download and queue management    |
+------------------------------------------------+
   |        |        |         |        |
   v        v        v         v        v
+--------+ +--------+ +---------+ +------+ +-----------+
| NNTP   | | Indexer| |Processor| |Queue | | Downloader|
| Manager| | Manager| | (PAR/RAR)| |Manager| |           |
+--------+ +--------+ +---------+ +------+ +-----------+
   |          |           |           |          |
   v          v           v           v          v
+------------------------------------------------+
|                     Store Package              |
|              (internal/store)                  |
|  Persistence layer: SQLite for metadata,       |
|  File System for NZB blobs                     |
+------------------------------------------------+
```

## Key Components

### `cmd/gonzb/main.go`
- **Entry Point:** Handles CLI command parsing (`cobra`), application startup (`setupApp`), and execution of either the `serve` (API server) or direct download modes.
- **`setupApp()`:** The central function for initializing and wiring up all core application services and dependencies into an `app.Context`.

### `internal/app/context.go`
- **Application Context:** Acts as the primary dependency injection container, holding instances of all major service interfaces.
- **Service Interfaces:** Defines the contracts for `NNTPManager`, `IndexerManager`, `Processor`, `Downloader`, `QueueManager`, `NZBParser`, and `Store`, promoting loose coupling.

### `internal/infra/config`
- **Configuration Management:** Loads and parses `config.yaml` settings, providing application-wide configuration parameters.

### `internal/infra/logger`
- **Logging:** Provides a unified logging interface with support for file logging and console output, configurable verbosity levels.

### `internal/engine`
- **`Downloader`:** Manages the actual downloading of Usenet articles (segments). It interacts with the `NNTPManager` to fetch data and the `FileWriter` to save it to disk.
- **`QueueManager`:** The heart of the unified queue system. It fetches items from the persistent store, orchestrates their hydration, delegates downloading to the `Downloader`, and post-processing to the `Processor`. It also manages the state of active downloads and handles cancellation.
- **`FileWriter`:** Manages writing downloaded segments to temporary files and their eventual consolidation.
- **`Worker`:** (Details in `internal/engine/worker.go`) Responsible for concurrent fetching of individual segments from NNTP providers.

### `internal/nntp`
- **`NNTPManager`:** Manages connections to multiple Usenet providers, handling connection pooling, failover, and article fetching. It abstracts away the complexities of the NNTP protocol.

### `internal/indexer`
- **`IndexerManager`:** Coordinates interactions with various indexers (e.g., Newznab-compatible APIs, local blob store). It's responsible for searching for releases and fetching NZB files.

### `internal/processor`
- **`Processor`:** Handles post-download tasks such as PAR2 verification and repair, and extraction of archives (RAR, ZIP, 7Z).

### `internal/nzb`
- **`NZBParser`:** Parses NZB files, extracting metadata about files and their segments.

### `internal/store`
- **Persistence Layer:** Manages all data persistence.
    - **SQLite:** Used for storing metadata about releases, queue items, and download tasks.
    - **File System:** Used as a blob store for caching raw NZB files.

## Data Flow (Download Process)

1.  **Initiation:** A download is initiated either via CLI (`gonzb --file my.nzb`) or the API server (`/api/v1/search` or manual upload).
2.  **Queueing:** A `domain.QueueItem` is created and added to the `QueueManager`, then persisted in the SQLite store.
3.  **Hydration (`QueueManager.HydrateItem`):** The `QueueManager` retrieves detailed release metadata from the `Store`, fetches the NZB file from an `Indexer` (which might read from the blob store or an external API), and parses it using `NZBParser`. The parsed information (files, segments) is attached to the `QueueItem`.
4.  **Downloading (`Downloader.Download`):** The `QueueManager` hands off the `QueueItem` to the `Downloader`. The `Downloader` uses a worker pool (`runWorkerPool`) to concurrently fetch segments from NNTP providers via the `NNTPManager`. The `FileWriter` handles writing these segments to disk.
5.  **Post-Processing (`Processor.PostProcess`):** Once all segments are downloaded, the `QueueManager` triggers the `Processor` to verify, repair, and extract the downloaded files.
6.  **Finalization:** The `QueueManager` updates the `QueueItem`'s status in the SQLite store, removes it from the active queue, and cleans up any temporary resources.

This modular design allows for independent development and testing of each component, contributing to the robustness and maintainability of GoNZB.