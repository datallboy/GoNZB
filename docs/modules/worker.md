# Posting worker

`gonzb-worker` is a separate Linux process for an operator-controlled posting
VPS. It polls qBittorrent for completed, tagged payloads on a seedbox, exposes
or copies only the selected source, runs the local posting engine, and submits
the completed NZB to GoNZB's uploader.

```text
seedbox qBittorrent API ---- completed item metadata ----+
                                                        |
seedbox files -- read-only SSHFS or rsync-over-SSH --> worker VPS
                                                        |
                                           local posting workspace
                                                        |
                                     completed sanitized NZB over HTTPS
                                                        |
                                                GoNZB uploader
```

The worker is not part of the main GoNZB server or container. GoNZB never
receives the source payload, seedbox login, SSH key, or NNTP credentials. The
GoNZB uploader still owns review, catalog approval, and explicit federation.

## One production process

Run exactly one normal `gonzb-worker` process for a worker data directory. That
single process owns the complete lifecycle:

1. mount or validate the SSHFS source when SSHFS mode is configured;
2. poll qBittorrent for completed items carrying the candidate tag;
3. resolve the selected qBittorrent content path beneath the configured source
   root;
4. run the posting engine against that source;
5. sanitize and submit the completed NZB to GoNZB;
6. persist the job checkpoint and continue polling.

The worker does not discover work by watching every file in the mount. The
qBittorrent completed state and candidate tag are its queue. The mount only
makes the selected source path available.

`-mount-only` is a temporary foreground diagnostic mode. It mounts and
validates SSHFS, waits for Ctrl-C, and exits without polling or posting. Do not
run a mount-only process beside the systemd worker service, and do not create a
second worker service for the mount.

## Requirements

- a Linux VPS with enough local space for the posting workspace;
- qBittorrent Web API access to the seedbox;
- SSH key authentication and either SSHFS/FUSE or rsync;
- the supported posting engine and its NNTP configuration on the worker VPS;
- HTTPS access to a GoNZB node with the uploader enabled;
- a dedicated GoNZB API token for a user with the built-in `uploader` role;
  that role grants only `uploader.submissions.create`.

Release assets currently include a Linux AMD64 worker binary. Other Linux
architectures can build it from source with `make build-worker`. The SSHFS mode
uses the Linux mount table and is not supported on Windows.

## Install the release assets

Download the worker binary, example configuration, service unit, and
`checksums.txt` from the same GitHub release. Verify the checksum before
installing:

```sh
sha256sum --check checksums.txt --ignore-missing
version=v0.10.0 # replace with the release tag you downloaded
sudo install -m 0755 "gonzb-worker_${version}_linux_amd64" /usr/local/bin/gonzb-worker
sudo useradd --system --home-dir /var/lib/gonzb-worker --shell /usr/sbin/nologin gonzb-worker
sudo install -d -o gonzb-worker -g gonzb-worker -m 0700 /var/lib/gonzb-worker
sudo install -d -o root -g gonzb-worker -m 0750 /etc/gonzb-worker
sudo install -o root -g gonzb-worker -m 0640 gonzb-worker-config.yaml.example /etc/gonzb-worker/config.yaml
sudo install -o root -g root -m 0644 gonzb-worker.service /etc/systemd/system/gonzb-worker.service
```

Install `sshfs` and `fuse3` for SSHFS mode, or `rsync` for copy mode, using the
VPS package manager. Confirm the service account can use `/dev/fuse` if the
distribution restricts it.

Install the seedbox's verified SSH host key in
`/var/lib/gonzb-worker/.ssh/known_hosts` before starting the service. Obtain and
verify its fingerprint through a trusted channel; do not solve first-connect
failures by disabling host-key checking. A direct non-interactive SSH test as
the service account should succeed before the mount smoke test.

## Configure the worker

Edit `/etc/gonzb-worker/config.yaml`. The important boundaries are:

- `worker.data_dir` is local durable state and temporary posting workspace;
- `qbittorrent.url` is the qBittorrent Web API base URL, not a torrent URL;
  reverse-proxy path prefixes such as `/qbittorrent/` are preserved;
- `qbittorrent.candidate_tag` limits normal polling to explicitly tagged,
  completed items;
- `transfer.source_root` is the absolute seedbox path that contains every
  qBittorrent content path the worker may accept;
- `gonzb.url` is the GoNZB origin, such as `https://gonzb.example.test`, with no
  uploader endpoint suffix;
- `gonzb.api_token` is the secret value of the least-privilege uploader token.

Keep secrets out of the YAML by placing overrides in
`/etc/gonzb-worker/gonzb-worker.env`:

```sh
GONZB_WORKER_QBITTORRENT_USERNAME=worker-api-user
GONZB_WORKER_QBITTORRENT_PASSWORD=replace-me
GONZB_WORKER_QBITTORRENT_HTTP_BASIC_USERNAME=proxy-user
GONZB_WORKER_QBITTORRENT_HTTP_BASIC_PASSWORD=replace-me
GONZB_WORKER_TRANSFER_SSH_KEY=/etc/gonzb-worker/seedbox_ed25519
GONZB_WORKER_GONZB_API_TOKEN=replace-me
```

Protect both files and the SSH private key with root ownership and group-read
access only where required. The worker logs structured lifecycle data but
deliberately discards posting-engine diagnostics that may contain credentials
or an archive password.

## Read-only SSHFS mode

SSHFS avoids copying the original payload into the worker workspace. The
posting engine reads the mounted source while archive, recovery, and NZB work
remain local to the VPS. Use a mount path outside `worker.data_dir`:

```yaml
transfer:
  type: sshfs
  ssh_host: seedbox.example.test
  ssh_user: seedbox-user
  ssh_port: 22
  ssh_key: /etc/gonzb-worker/seedbox_ed25519
  source_root: /downloads
  mount_path: /mnt/gonzb-worker-seedbox
  manage_mount: true
  unmount_on_exit: true
```

Create the mount point for the service account before starting it:

```sh
sudo install -d -o gonzb-worker -g gonzb-worker -m 0700 /mnt/gonzb-worker-seedbox
```

The worker forces the mount read-only, batch SSH authentication, connection
timeouts, keepalives, and reconnect behavior. It rejects writable options,
`allow_other`, alternate identity/SSH commands, symlink mount points, an
unexpected SSHFS source, and any mount that overlaps its local data directory.

For a mount managed outside the worker, set `manage_mount: false`. The mount
must already exist in the worker service's mount namespace and must still be
read-only SSHFS from the exact configured user, host, and source root.

## Safe mount smoke test

This command mounts and validates SSHFS without querying qBittorrent, invoking
the posting engine, contacting NNTP, or uploading an NZB:

```sh
sudo systemctl stop gonzb-worker.service
sudo systemd-run --unit=gonzb-worker-mount-smoke --collect \
  --property=User=gonzb-worker \
  --property=Group=gonzb-worker \
  --property=EnvironmentFile=/etc/gonzb-worker/gonzb-worker.env \
  /usr/local/bin/gonzb-worker \
  -config /etc/gonzb-worker/config.yaml -mount-only
sudo journalctl -u gonzb-worker-mount-smoke -f
```

In another shell, verify that expected files are visible beneath the mount path
and that writes fail. Then stop the temporary smoke unit:

```sh
sudo systemctl stop gonzb-worker-mount-smoke.service
```

A worker-managed mount is unmounted when `unmount_on_exit` is true. Confirm the
smoke unit has stopped before starting the normal worker service. The transient
unit is only a convenient way to load the same service account and environment;
it is not a second production daemon.

`-once -torrent-hash <info-hash>` is not a dry run. It executes the real
posting and GoNZB submission lifecycle for that completed qBittorrent item.

## Run as a service

After the mount-only smoke test and an operator-controlled posting test:

```sh
sudo systemctl daemon-reload
sudo systemctl enable --now gonzb-worker
sudo systemctl status gonzb-worker
sudo journalctl -u gonzb-worker -f
```

The worker persists job checkpoints in
`/var/lib/gonzb-worker/state/worker.db`. Transfer and GoNZB submission failures
are retried from durable checkpoints. If the worker stops while posting, the
result is intentionally marked for manual reconciliation to prevent an
automatic duplicate NNTP post.

Do not run multiple worker instances against the same `worker.data_dir`. The
durable SQLite job store and mount lifecycle are owned by the single systemd
service.

After a successful submission, open **Uploader** in GoNZB. The item remains
pending until approved. Publishing it to a GoNZBNet pool is a separate,
explicit administrator action. The submitted NZB is rewritten into a
deterministic private form: its head retains only an archive password when one
exists, while title and provenance stay in authenticated GoNZB metadata rather
than the NZB itself.

## Docker deployment

The supported worker deployment is the native Linux binary under systemd. No
`gonzb-worker` Docker image or Compose service is shipped. Putting SSHFS inside
a container requires exposing `/dev/fuse` and additional mount privileges and
would weaken the intended boundary without improving this single-VPS topology.

The main GoNZB server may still run in its normal Docker Compose stack on the
same or a different host. The native worker only needs HTTPS access to that
GoNZB origin. Do not add the worker to the main GoNZB container.
