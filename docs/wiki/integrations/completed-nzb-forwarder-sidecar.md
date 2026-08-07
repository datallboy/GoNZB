# Completed-NZB forwarder sidecar

The completed-NZB forwarder is a producer-neutral delivery agent for posting
tools that run on a different server from GoNZB. It watches a local completed
output directory and sends stable NZBs to GoNZB's authenticated uploader API.
It does not acquire content, post articles, access NNTP, or publish directly to
a GoNZBNet pool.

## Recommended ownership boundary

Treat the forwarder as a companion service, not as part of either the Postie or
GoNZB server process:

```text
Postie container ──writes──> completed-NZB volume <──read-only── forwarder
                                                               |
                                                   outbound HTTPS only
                                                               |
                                                               v
                                                       GoNZB uploader API
```

This is especially useful when Postie is isolated behind a provider VPN.
GoNZB never needs an inbound route to Postie. The forwarder only needs read
access to Postie's completed-NZB volume, persistent local state, and an outbound
route to GoNZB.

Do not automatically place the forwarder in Postie's NNTP VPN network
namespace. It does not contact Usenet. Give it the narrow route that can reach
GoNZB over HTTPS; this may be an internal container network, LAN route, or
private overlay network while Postie's NNTP traffic remains VPN-isolated.

## Current implementation status

The current reference implementation consists of:

- `scripts/gonzb-submit-nzb-watch.sh`, the long-running directory scanner and
  durable retry loop;
- `scripts/gonzb-submit-nzb.sh`, the bounded HTTP submission helper;
- GoNZB's producer-neutral `POST /api/v1/uploader/submissions` endpoint.

There is not yet a published standalone forwarder container image. Until one is
released, these scripts can be installed into an operator-owned sidecar image.
The image needs Bash, curl, CA certificates, `find`, `stat`, `awk`, and either
`sha256sum` or `shasum`.

## Container runtime contract

The sidecar should receive only the following resources:

| Resource | Access | Purpose |
| --- | --- | --- |
| Completed-NZB volume | read-only | Recursively discover stable `.nzb` files |
| Forwarder state volume | read-write | Delivery receipts and retry deadlines |
| GoNZB URL | configuration | HTTPS uploader endpoint |
| GoNZB token | secret | Authentication with only `uploader.submissions.create` |
| CA trust | read-only | Verify the GoNZB TLS certificate |

The container command is:

```sh
/usr/local/bin/gonzb-submit-nzb-watch.sh /input /state
```

Its relevant environment is:

```ini
GONZB_URL=https://gonzb.internal.example
GONZB_TOKEN=least-privilege-token
GONZB_WATCH_INTERVAL_SECONDS=30
GONZB_WATCH_SETTLE_SECONDS=60
GONZB_WATCH_RETRY_BASE_SECONDS=60
GONZB_WATCH_RETRY_MAX_SECONDS=3600
GONZB_WATCH_MAX_NZB_BYTES=67108864
```

In production, supply the token through the container platform's secret
facility rather than Compose source or an image layer. Run as a non-root user,
use a read-only root filesystem, drop Linux capabilities, set
`no-new-privileges`, and persist `/state` independently of the container.

An illustrative Compose service contract is:

```yaml
services:
  postie:
    volumes:
      - postie_nzb_output:/var/lib/postie/output

  nzb-forwarder:
    image: your-registry/gonzb-nzb-forwarder:your-pinned-version
    restart: unless-stopped
    environment:
      GONZB_URL: https://gonzb.internal.example
      GONZB_TOKEN: ${GONZB_FORWARDER_TOKEN:?set in deployment secrets}
      GONZB_WATCH_INTERVAL_SECONDS: "30"
      GONZB_WATCH_SETTLE_SECONDS: "60"
      GONZB_WATCH_RETRY_BASE_SECONDS: "60"
      GONZB_WATCH_RETRY_MAX_SECONDS: "3600"
    volumes:
      - postie_nzb_output:/input:ro
      - gonzb_forwarder_state:/state
    read_only: true
    cap_drop: [ALL]
    security_opt:
      - no-new-privileges:true
    command: ["/input", "/state"]

volumes:
  postie_nzb_output:
  gonzb_forwarder_state:
```

The image name is deliberately illustrative until a versioned image exists.
Use an immutable digest in a real deployment. A Docker secret-to-environment
entrypoint may also be needed until the forwarder supports a token-file option.

## Delivery behavior

The forwarder:

1. recursively scans only regular `.nzb` files and does not follow symlinks;
2. waits for the configured settle age;
3. bounds the accepted file size;
4. hashes the file and verifies that it did not change during submission;
5. sends it through the normal authenticated uploader endpoint;
6. stores a receipt keyed by exact NZB SHA-256 after success;
7. stores exponential retry state after failure and resumes it after restart.

The sidecar never deletes, renames, or modifies Postie's output. GoNZB performs
its normal bounded NZB parsing and exact-content deduplication. A successfully
delivered item still starts in `pending_review`; the sidecar cannot approve or
publish it.

Postie's `post_upload_script` may remain enabled for low-latency delivery, but
it is optional. If both paths submit the same NZB, GoNZB returns the existing
submission and the forwarder records its receipt. Running only the sidecar
avoids that one duplicate request.

## Separate repository recommendation

A separate project is the cleaner long-term home if the forwarder will be a
published production image. It has an independent deployment lifecycle and is
useful for Postie, pesto, Loon, and any other producer that emits completed
NZBs. It should therefore not be named or coupled specifically to Postie.

The proposed split is:

- **GoNZB repository:** owns the uploader API contract, validation,
  authentication, server-side deduplication, and cross-project conformance
  tests.
- **Forwarder repository:** owns a small static binary, container image,
  filesystem scanning, durable queue state, health checks, metrics, image
  signing/SBOMs, and release documentation.
- **Producer projects:** own only the completed-NZB output volume; no GoNZB
  credentials or API logic is added to their core processing.

The current scripts should remain as the tested reference implementation until
the standalone image passes the same conformance harness. Extraction is worth
doing when the project is ready to publish and maintain versioned multi-arch
images. Merely moving the two scripts to another repository before that point
would add release overhead without improving runtime behavior.

