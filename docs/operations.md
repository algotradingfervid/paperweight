# Operations

Draft (WP3 Task 6, Steps 1 and 3). Running Paperweight on one Linux server: one
binary, a data directory `/var/lib/paperweight` holding `app.db` and `files/`,
Caddy in front for TLS, Litestream for database backups. The files are in
`deploy/`.

## Binary sizes

Built on 2026-10-25 from commit `d17fb3a` with Go 1.27.3, as `make build` does
(`CGO_ENABLED=0 go build -trimpath`), one build per target:

| Target | Size |
|---|---|
| linux/amd64 | 22,009,771 bytes (21.0 MiB) |
| linux/arm64 | 20,835,462 bytes (19.9 MiB) |

These are WP0 binaries. They get re-measured at release (Task 6 Step 4).

Build a release binary:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o app-linux-amd64 ./cmd/app
```

Use `GOARCH=arm64` for ARM servers.

## Install

Run as root.

1. Create the user and directories:
   ```sh
   useradd --system --home-dir /var/lib/paperweight --shell /usr/sbin/nologin paperweight
   install -d -o paperweight -g paperweight -m 0750 /var/lib/paperweight
   install -d -m 0750 /etc/paperweight
   ```
2. Copy the binary: `install -m 0755 app-linux-amd64 /usr/local/bin/app`.
3. Install the unit: `cp deploy/app.service /etc/systemd/system/paperweight.service`,
   then `systemctl daemon-reload && systemctl enable --now paperweight`.
   The app listens only on `127.0.0.1:8080`; check with
   `curl http://127.0.0.1:8080/healthz` (prints `ok`).
4. Caddy: install it from the distribution or caddyserver.com, copy
   `deploy/Caddyfile` to `/etc/caddy/Caddyfile`, replace
   `paperweight.example.com` with the real domain, open ports 80 and 443, and
   `systemctl reload caddy`. Caddy gets the certificate itself. It sends
   `X-Forwarded-For` and `X-Forwarded-Proto` to the app by default.
5. Litestream v0.5.17: install the release package from
   github.com/benbjohnson/litestream (it ships `litestream.service`, which runs
   `litestream replicate` as root). Then:
   - copy `deploy/litestream.yml` to `/etc/litestream.yml`;
   - write the bucket settings to `/etc/paperweight/litestream.env`
     (mode 0600, owner root):
     ```sh
     LITESTREAM_BUCKET=...
     LITESTREAM_ENDPOINT=https://...      # empty for AWS S3
     LITESTREAM_REGION=...
     LITESTREAM_ACCESS_KEY_ID=...
     LITESTREAM_SECRET_ACCESS_KEY=...
     ```
   - run it as the app user with that file, using `systemctl edit litestream`:
     ```ini
     [Service]
     User=paperweight
     Group=paperweight
     EnvironmentFile=/etc/paperweight/litestream.env
     ```
   - `systemctl enable --now litestream`.

   The config keys (`dbs`, `path`, `replica`, `type`, `bucket`, `path`,
   `endpoint`, `region`, `access-key-id`, `secret-access-key`), the
   `${VAR}` expansion and the default path `/etc/litestream.yml` were checked
   against the v0.5.17 source (`cmd/litestream/main.go`,
   `main_notwindows.go`, `etc/litestream.service`). Not yet run against a real
   bucket; the restore drill (Step 2) will.

## First admin

Pending (WP3 Task 2): on a fresh data directory every page redirects to
`/setup`, where the first admin is created. Open `https://<domain>/setup`
straight after install.

`app admin create` (create an admin from the command line): pending (WP3 Task 2).

## Backup and restore

The database: Litestream streams every change in `app.db` to the bucket.

The uploaded files (`files/`): Litestream does not copy them. The spec (§7) says
they are synced to the same bucket; how is an owner choice, pending. A simple
option is an hourly systemd timer running
`rclone copy /var/lib/paperweight/files <remote>:<bucket>/paperweight/files`
(or `aws s3 sync` without `--delete`). `copy` keeps files the app has removed,
so an older database restore still finds its files.

Restore onto a new server or a fresh data directory. Install steps 1–5 come
first: the restore needs the `paperweight` user (step 1), and
`/etc/litestream.yml` and `/etc/paperweight/litestream.env` (step 5).

1. `systemctl stop paperweight litestream`.
2. Move the old `app.db`, `app.db-wal` and `app.db-shm` aside, if any, and
   Litestream's tracking folder `/var/lib/paperweight/.app.db-litestream`
   (to be confirmed in the restore drill, WP3 Task 6 Step 2).
3. As root, load the bucket settings and restore, then hand the file back to
   the app user:
   ```sh
   set -a; . /etc/paperweight/litestream.env; set +a
   litestream restore -integrity-check full -o /var/lib/paperweight/app.db /var/lib/paperweight/app.db
   chown paperweight:paperweight /var/lib/paperweight/app.db
   ```
   (`-timestamp <RFC 3339 time>` restores to a point in time.)
4. Copy `files/` back from the bucket into `/var/lib/paperweight/files`.
5. `chown -R paperweight:paperweight /var/lib/paperweight`. The restore and the
   integrity check run as root, but the app runs as `paperweight`, so it must
   own `app.db`, its `-wal`/`-shm` files and `files/` again or uploads fail.
6. `systemctl start litestream paperweight`.

Restore drill script `scripts/restore-drill.sh`: pending (WP3 Task 6 Step 2).

## Upgrade

1. Copy the new binary to the server.
2. `systemctl stop paperweight`.
3. `install -m 0755 app-linux-amd64 /usr/local/bin/app`.
4. `systemctl start paperweight`.

Migrations run on start: `app serve` opens the database and applies every
pending migration before it listens (`openDB` in `cmd/app/main.go`). To apply
them without starting the server, run
`sudo -u paperweight app migrate --data /var/lib/paperweight`. Leave Litestream
running; it carries on replicating.

## File garbage collection

The app is to remove unreferenced files hourly on its own (spec §7), along
with an hourly sweep of expired sessions, so there is nothing to schedule. The
WP0 binary does not run these jobs yet: pending (WP3 integration).

`app files gc` (run a GC by hand): pending (WP3 Task 2).

## Logs

The app writes its logs to stdout and stderr, which systemd sends to journald.
The spec (§7) asks for `log/slog` JSON on stdout; the WP0 binary still writes
slog text to stderr: pending (WP3 integration). journald keeps both.

```sh
journalctl -u paperweight -f          # follow
journalctl -u paperweight --since today
journalctl -u litestream -f
```

Caddy's logs: `journalctl -u caddy`.
