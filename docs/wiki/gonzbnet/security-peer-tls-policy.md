# Security: Peer TLS Policy

GoNZBNet outbound federation now requires HTTPS/WSS for peer traffic by
default.

## Configuration

```yaml
gonzbnet:
  allow_insecure_peer_http: false
```

When the flag is `false`, outbound peer URLs must use `https://` and websocket
gossip must use `wss://`.

When the flag is `true`, insecure HTTP is still limited to local development
loopback hosts:

- `localhost`
- `127.0.0.1`
- `::1`

Non-local `http://` peers are rejected even with the flag enabled.

## Enforcement Points

The policy is enforced for:

- configured manual peers;
- admin-added peers;
- peer-exchange URLs before storing them;
- pull-sync discovery, profile, caps, and outbox reads;
- push-sync inbox delivery;
- websocket gossip connections;
- on-demand signed manifest fetches.

Admin config validation reports manual peers that violate the policy and warns
when local-development insecure HTTP is enabled.
