# Project proposal: `gonzb-nzb-forwarder`

> Status: proposed and intentionally deferred. No forwarder binary, watcher
> script, container image, or supported deployment currently ships with GoNZB.

`gonzb-nzb-forwarder` is a proposed producer-neutral sidecar for delivering
completed NZBs from a posting host to a separate GoNZB server. This document is
a project note, not an implementation plan or operator runbook.

## Problem statement

Postie and similar tools can produce a completed NZB on a host that should
remain isolated behind a VPN. Postie's current `post_upload_script` can submit
that NZB to GoNZB immediately, but at the tested upstream revision its durable
script retry worker is not started by the service lifecycle. A longer GoNZB or
network outage can therefore outlast the hook's bounded inline retries.

Loon's offline mode is the stronger motivating case: it produces a completed
output directory but does not currently provide the generic HTTP post-upload
hook used by Postie and pesto. Without this proposed sidecar, separate Loon and
GoNZB servers need a shared filesystem mount for GoNZB's read-only inbox.

A shared NFS/SMB mount would let GoNZB use its existing read-only inbox, but it
adds infrastructure and exposes the posting host's output across machines. A
small outbound-only delivery sidecar could provide durable handoff without an
inbound route to the posting server.

## Proposed boundary

The forwarder would be a separate companion service, not part of Postie or the
GoNZB server process:

```text
Postie container ──writes──> completed-NZB volume <──read-only── forwarder
                                                               |
                                                   outbound HTTPS only
                                                               |
                                                               v
                                                       GoNZB uploader API
```

The project should be generic. Postie, pesto, Loon, or any other producer could
use it by exposing a directory containing finalized NZBs. It should not know
how the source material was acquired or posted.

## Repository recommendation

If implemented, this should live in a separate repository and publish its own
versioned container image. The deployment lifecycle, security surface, and
release cadence are independent from GoNZB. Keeping it separate also prevents
the GoNZB server image from acquiring filesystem-watcher or sidecar runtime
concerns.

Suggested ownership split:

- **GoNZB repository:** uploader API contract, bounded validation,
  authentication, exact-content deduplication, and cross-project conformance
  fixtures.
- **`gonzb-nzb-forwarder` repository:** forwarder binary, container image,
  durable delivery state, health checks, metrics, release automation, SBOMs,
  and image signing.
- **Producer projects:** only generate completed NZBs and expose their output
  volume; they do not gain GoNZB-specific core dependencies.

The preferred implementation would be a small static Go binary rather than a
shell-script image. That avoids Bash/curl/coreutils runtime dependencies and
provides a cleaner path for cancellation, health checks, structured logs,
secret-file support, and deterministic multi-architecture images.

## Proposed runtime contract

The sidecar would receive only:

| Resource | Access | Purpose |
| --- | --- | --- |
| Completed-NZB volume | read-only | Discover finalized `.nzb` files recursively |
| Forwarder state volume | read-write | Durable delivery receipts and retry deadlines |
| GoNZB URL | configuration | Locate the HTTPS uploader endpoint |
| GoNZB token | secret | Authenticate with only `uploader.submissions.create` |
| CA trust | read-only | Verify the GoNZB TLS certificate |

The state volume must survive container recreation. The input volume must never
be modified, renamed, or cleaned by the forwarder.

## Proposed behavior

The first implementation should:

1. recursively inspect only regular `.nzb` files without following symlinks;
2. require a configurable settle period and verify the file remains unchanged;
3. enforce a finite input-size limit before submission;
4. deliver through `POST /api/v1/uploader/submissions` using a dedicated token;
5. persist success by exact NZB SHA-256 so restarts do not resend old files;
6. persist bounded exponential retry state for network and server failures;
7. distinguish retryable failures from permanent validation/authentication
   failures and expose both operationally;
8. use bounded concurrency and avoid repeated scans generating network chatter;
9. provide a container health check and concise structured status metrics;
10. avoid logging tokens, NZB bodies, passwords, or response metadata that may
    contain sensitive release information.

GoNZB would retain all authority. Every newly delivered NZB would still enter
`pending_review`; the forwarder could not approve, publish, or administer a
GoNZBNet pool.

## Network and VPN model

The forwarder would not need NNTP or general Internet access. It should not
automatically share Postie's provider-VPN network namespace. Instead, it should
mount the completed output volume and receive the narrow route needed to reach
GoNZB over HTTPS through an internal container network, LAN, or private overlay
network.

This preserves the intended isolation:

- Postie's NNTP traffic remains constrained by its VPN policy;
- GoNZB receives an outbound authenticated request and never connects to
  Postie;
- the forwarder cannot access Postie's source input or NNTP credentials;
- GoNZB's API token grants submission only, not review or publication.

## Proposed container properties

A future production image should:

- run as a fixed non-root user;
- use a read-only root filesystem;
- drop all Linux capabilities and enable `no-new-privileges`;
- support Docker/Kubernetes secret files instead of requiring the token in
  Compose environment text;
- provide signed multi-architecture images pinned by immutable digest;
- include an SBOM and automated vulnerability scanning;
- expose health without opening an externally reachable management service.

## Relationship to Postie's hook

Until this project exists, the supported separate-server integration remains
Postie's `post_upload_script` calling GoNZB's one-shot submission helper. It
provides short bounded retries but is not presented as durable across a long
outage.

Pesto has the same viable HTTP-hook boundary. Loon does not: its current
offline-output integration remains local/shared-filesystem only until a
forwarder or equivalent operator-owned transport exists.

If the sidecar is implemented later, operators could choose one delivery path:

- the Postie hook for immediate best-effort delivery; or
- the sidecar for durable directory-backed delivery.

Using both should not be the recommended default because it creates an
avoidable duplicate request, even though GoNZB safely deduplicates identical
NZB bytes.

## Acceptance criteria before implementation is considered complete

- A clean repository and independently versioned container image exist.
- Unit tests cover settle detection, mutation races, symlink rejection,
  content receipts, retry classification, restart recovery, and secret
  redaction.
- The existing synthetic Postie conformance proves posting, sidecar delivery,
  pending review, approval, pool publication, remote search, and remote grab.
- A multi-hour outage/restart soak proves no lost or duplicate-visible
  submissions and bounded request volume.
- Documentation covers Compose and Kubernetes deployment without requiring an
  inbound connection or cross-server filesystem mount.

No implementation work is currently scheduled by this document.
