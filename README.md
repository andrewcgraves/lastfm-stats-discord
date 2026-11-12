# lastfm-stats-discord

A Discord bot that posts a weekly digest of each member's Last.fm listening
(top tracks, top artists and scrobble count), with an optional chart of
scrobbles per day.

The Docker image is published to Docker Hub as
[`andrewcgraves/lastfm-bot`](https://hub.docker.com/repository/docker/andrewcgraves/lastfm-bot).

## Slash commands

| Command | What it does |
| --- | --- |
| `/link-lastfm lastfm-username:<name>` | Links your Discord account to a Last.fm user (the name is checked against Last.fm first). |
| `/manual-trigger` | Posts this week's digest right now. |
| `/ping` | Health check; replies with the gateway latency. |

## Running

```sh
docker run -d --name lastfm-bot --restart unless-stopped \
  --env-file ./bot.env \
  -p 8080:8080 \
  andrewcgraves/lastfm-bot:latest
```

**Always run it with a restart policy** (`--restart unless-stopped`, or your
platform's equivalent). If the bot cannot recover its Discord connection on
its own, it exits on purpose so that the restart gives it a clean slate.

Variables can also come from a `.env` file. By default the bot reads
`/.aws/config/.env` (the original deployment's path); set `ENV_FILE` to use
another path. Variables already set in the environment win over the file.

### Configuration

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `DISCORD_TOKEN` | yes | | Bot token. |
| `CHANNEL_ID` | yes | | Channel the weekly digest is posted to. |
| `LASTFM_API_KEY` | yes | | Last.fm API key. |
| `TABLE_NAME` | yes | | DynamoDB table holding the account links (`discordID` number, `lastFMName` string). |
| `AWS_REGION` | | `us-west-2` | Region of the table. AWS credentials are found the usual way (env vars, `~/.aws`, instance role). |
| `SPOTIFY_ID` / `SPOTIFY_SECRET` | | | Spotify app credentials, used for artist thumbnails. Without them, embeds have no thumbnail. |
| `DIGEST_SCHEDULE` | | `30 12 * * 6` | Cron expression for the weekly post (default: Saturdays 12:30). |
| `DIGEST_TIMEZONE` | | `UTC` | IANA time zone for the schedule and the graph's days, e.g. `America/Los_Angeles`. |
| `DIGEST_ACTIVITY_GRAPH` | | `false` | Attach a chart of scrobbles per day to the digest. |
| `HEALTH_PORT` | | `8080` (or `PORT`) | Port for `GET /healthz`. `off` disables it. |
| `WATCHDOG_RECONNECT_AFTER` | | `5m` | Force a fresh gateway connection after the connection has been unhealthy this long. |
| `WATCHDOG_EXIT_AFTER` | | `30m` | Exit (for a supervisor restart) after the connection has been unhealthy this long. `0` disables. |
| `LOG_LEVEL` | | `info` | `debug`, `info`, `warn` or `error`. |

`LASTFM_API_SECRET` and `BACKBLAZE_*` are no longer used. The graph is now
attached to the Discord message directly, so Backblaze isn't needed.

## How it stays up

- **Gateway:** discordgo reconnects on its own. A watchdog also checks the
  connection every 30s (connected, with heartbeat ACKs arriving). If the
  connection stays unhealthy it forces a new one. If even that fails for
  `WATCHDOG_EXIT_AFTER`, the process exits so it can be restarted.
- **Startup:** startup does not crash-loop during a Discord outage. The
  connection is retried with backoff, and only an invalid token makes the
  bot give up.
- **No crashes from bad data:** every command, scheduled job and per-user
  digest section is isolated with panic recovery. One user's Last.fm error
  shows up as a note in their own embed and doesn't block everyone else's
  digest.
- **Timeouts and retries everywhere:** Last.fm, Spotify, DynamoDB and
  Discord calls all have timeouts and retry transient failures. Last.fm
  calls are rate limited.
- **Discord limits:** digests are split into messages of at most 10
  embeds / 6000 characters. Slash commands acknowledge within Discord's
  3-second window and send their result afterwards.

## Development

```sh
go test -race ./...
go run .            # reads .env in the working directory too
```

Code layout:

- `main.go`: wiring, scheduler, graceful shutdown
- `internal/bot`: Discord session, slash commands, watchdog, health endpoint
- `internal/digest`: builds the digest embeds and the activity graph
- `internal/lastfm`: Last.fm API client
- `internal/artwork`: Spotify artist-image lookup
- `internal/store`: DynamoDB account links
- `internal/config`: environment configuration
