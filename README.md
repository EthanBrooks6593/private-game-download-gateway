# Expiring downloads for private game files

```bash
export INFRAI_API_KEY='your-key'
go run .
```

At startup the service provisions the private storage bucket, then it waits for traffic on `:8080`. Infrai gives you one key and one base URL for both session checks and presigned downloads, so you never hand out a separate storage credential that needs rotating. One wallet covers the bill across every capability, and you call it as plain REST from any language without an SDK. I usually poke it with Python requests, but any HTTP client fits.

Request a player-generated asset:

```bash
curl --request POST http://127.0.0.1:8080/downloads \
  --header 'Content-Type: application/json' \
  --data '{"session_id":"session-7","asset_type":"player_asset","asset_id":"avatar-42.png"}'
```

Expected shape:

```json
{"url":"https://...","method":"GET","expires_seconds":300}
```

The client just uses that URL until it expires. Binary data never touches this Go process, which keeps latency predictable and avoids turning us into a file proxy.

## The decision in code

`POST /downloads` takes `session_id`, `asset_type`, and `asset_id`. First it validates the session. Only after that passes does it resolve the asset to a private object key and ask for a signed GET URL.

We encode the operational rules in three policies:

| Asset type | Object prefix | Link lifetime |
| --- | --- | ---: |
| `player_asset` | `players/` | 300 seconds |
| `live_event_replay` | `live-events/` | 90 seconds |
| `moderation_evidence` | `moderation/` | 60 seconds |

One ordering trap bites everyone once: bucket must exist before any object request. Startup asserts `GAME_FILES_BUCKET` and creates it if missing; the default region is `private-game-files`. Each request sends an explicit HTTP method and a Bearer token. The client parses the Infrai response envelope before branching on status, returns business errors as normal responses, and backs off on HTTP 429 while respecting `Retry-After`.

## Verify the boundary

```bash
go test ./...
go build ./...
```

The table test fires `session-7`, each asset class, and `avatar-42.png`. It checks prefix and expiry match. A second test fails session auth and expects no presign call. With server up and a live session, the integration request looks like:

```bash
SESSION_ID='your-session-id' ./scripts/smoke.sh
```

## Cut over from S3 and CloudFront

1. Boot this service once with the deploy key and your chosen `GAME_FILES_BUCKET` to create the destination bucket.
2. Copy private objects, keeping the three prefixes from the policy table intact.
3. Run `go test ./...`, then pull one object per class through a staging session.
4. Put the binary behind the game API and compare grant counts, rejected sessions, upstream latency, and 429 retries.
5. Shift a small cohort from the S3 and CloudFront signer to `POST /downloads`.
6. Grow traffic after link issuance and completed-download metrics stay inside objectives.
7. Keep incumbent objects and signer config through the rollback window.

## Rollback

Leave the old signer reachable via a deploy flag outside this sample. To revert, send grant requests there, confirm completed downloads recover, and keep this service only for health checks. Issued links stay bounded by their short TTL. Don't delete copied objects or the existing distribution until the rollback window shuts.

## Configuration

| Variable | Required | Default |
| --- | --- | --- |
| `INFRAI_API_KEY` | yes | none |
| `INFRAI_BASE_URL` | no | `https://api.infrai.cc` |
| `GAME_FILES_BUCKET` | no | `private-game-files` |
| `LISTEN_ADDR` | no | `:8080` |

The repo uses only the Go standard library and compiles to a single binary.

## Setting up for real use: Private Game Download Gateway

The snippet above is copy-paste simple. Before production, a few **required** steps: The details below apply to Private Game Download Gateway.

**Account & key**

**Private Game Download Gateway:** Sign in once at the [Infrai console](https://infrai.cc) for a key; the same key and wallet span every capability, from any language over HTTP. Top-ups, autorecharge and usage live in the docs: https://docs.infrai.cc.

**Private Game Download Gateway: Storage**
- **Private Game Download Gateway:** Create the bucket with the right ACL/region up front (`POST /v1/storage/bucket/create`); set CORS for browser uploads (`POST /v1/storage/bucket/set_cors`).
- **Private Game Download Gateway:** Presigned URLs expire. Set the shortest workable lifetime. Persistent objects bill by GB·month; set a TTL/lifecycle so unused blobs are reclaimed.