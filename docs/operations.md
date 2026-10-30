# Operations

Running Paperweight on one Linux server: one binary, a data directory `/var/lib/paperweight` holding `app.db` and `files/`,
Caddy in front for TLS, Litestream for database backups, an hourly rclone copy
for uploaded files. The files are in `deploy/`.

## Binary sizes

Built on 2026-10-30 from commit `04e28f4` with Go 1.27.3
(`CGO_ENABLED=0 GOOS=linux go build -trimpath`), one build per target. These
sizes are for that commit; they are re-measured before the `core-v1` tag.

| Target | Size |
|---|---|
| linux/amd64 | 32,876,708 bytes (31.4 MiB) |
| linux/arm64 | 30,431,462 bytes (29.0 MiB) |

Build a release binary:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o app-linux-amd64 ./cmd/app
```

Use `GOARCH=arm64` for ARM servers.

## Install

Run as root. Replacing a server whose bucket already holds backups? Don't follow
this list as written: it starts services. Go to [Restore](#restore) first.

1. Create the user and directories:
   ```sh
   useradd --system --home-dir /var/lib/paperweight --shell /usr/sbin/nologin paperweight
   install -d -o paperweight -g paperweight -m 0750 /var/lib/paperweight
   install -d -o paperweight -g paperweight -m 0700 /var/lib/paperweight/files
   install -d -m 0750 /etc/paperweight
   apt install sqlite3      # used by the upgrade and restore checks
   ```
2. Copy the binary: `install -m 0755 app-linux-amd64 /usr/local/bin/app`.
3. Install the unit: `cp deploy/app.service /etc/systemd/system/paperweight.service`,
   replace `paperweight.example.com` in its `--trusted-origin` with the real
   domain, then `systemctl daemon-reload && systemctl enable --now paperweight`.
   The app listens only on `127.0.0.1:8080`; check with
   `curl http://127.0.0.1:8080/healthz` (prints `ok`). The unit runs with
   `--secure`, so `/setup` answers 404 from the start.
4. Caddy: install it from the distribution or caddyserver.com, copy
   `deploy/Caddyfile` to `/etc/caddy/Caddyfile`, replace
   `paperweight.example.com` with the real domain, and create the first admin
   (see [First admin](#first-admin)). Only then open ports 80 and 443 and
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
     # Empty for AWS S3.
     LITESTREAM_ENDPOINT=
     LITESTREAM_REGION=ap-south-1
     LITESTREAM_ACCESS_KEY_ID=...
     LITESTREAM_SECRET_ACCESS_KEY=...
     ```
     systemd reads this file too and keeps a comment on a value's line as part
     of the value, so comments go on their own line. Litestream's `${...}` has
     no default syntax, so the region must be set here. Keep this file in the production secrets, never in the repository.
   - run it as the app user with that file, using `systemctl edit litestream`:
     ```ini
     [Service]
     User=paperweight
     Group=paperweight
     EnvironmentFile=/etc/paperweight/litestream.env
     ```
   - `systemctl enable --now litestream`.

   The config keys, the `${VAR}` expansion and the default path
   `/etc/litestream.yml` were checked against the v0.5.17 source
   (`cmd/litestream/main.go`, `main_notwindows.go`, `etc/litestream.service`).
   `deploy/litestream.yml` was run once on v0.5.17 against a local
   S3-compatible server (replicate, then restore with a full integrity check).
   Not yet run against a real bucket: see [Go-live](#go-live).
6. Files backup: install rclone from the distribution or rclone.org (checked
   with v1.75.2; the unit expects `/usr/bin/rclone`). Then:
   - add the rclone remote to `/etc/paperweight/litestream.env`, with the same
     keys as above (systemd doesn't expand one variable inside another):
     ```sh
     RCLONE_CONFIG_PAPERWEIGHT_TYPE=s3
     RCLONE_CONFIG_PAPERWEIGHT_PROVIDER=AWS
     RCLONE_CONFIG_PAPERWEIGHT_REGION=ap-south-1
     RCLONE_CONFIG_PAPERWEIGHT_ACCESS_KEY_ID=...
     RCLONE_CONFIG_PAPERWEIGHT_SECRET_ACCESS_KEY=...
     RCLONE_CONFIG_PAPERWEIGHT_NO_CHECK_BUCKET=true
     ```
     For another S3-compatible provider, set `PROVIDER=Other` and add
     `RCLONE_CONFIG_PAPERWEIGHT_ENDPOINT=https://...`.
   - `cp deploy/paperweight-files-sync.service deploy/paperweight-files-sync.timer /etc/systemd/system/`
   - `systemctl daemon-reload && systemctl enable --now paperweight-files-sync.timer`
   - check one run: `systemctl start paperweight-files-sync.service`, then
     `journalctl -u paperweight-files-sync`.

   The rclone command and the remote were run once against the same local
   S3-compatible server. The two systemd units have not been run: there is no
   systemd on the development machine.

## First admin

The app is public with no 2FA, so nobody on the internet may claim the install.
With `--secure`, as the unit runs it, `/setup` answers 404 and an empty database
shows `/signin` (CD-228, CD-231). So the first admin is created on the server,
before ports 80 and 443 open. The command asks for the password twice:

```sh
sudo -u paperweight app admin create --data /var/lib/paperweight --email <email> --name "<name>"
```

Once the ports are open, sign in. Signed-in `/` sends an Administrator to
`/setup/company` while no company exists; fill that in once.

An Administrator who forgets a password recovers without SQL. This sets a
temporary password, asked for twice, that must be changed at the next sign-in. It
also clears the lockout and ends that user's sessions. It accepts Administrators
only:

```sh
sudo -u paperweight app admin reset-password --data /var/lib/paperweight --email <email>
```

Without `--secure` (development only), every page redirects to `/setup` until
the first admin exists.

## Backup and restore

Production backs up to an S3 bucket. Development and testing back up to the
local, gitignored `backup/` folder (CD-226).

| | Database (`app.db`) | Uploaded files (`files/`) |
|---|---|---|
| How | Litestream streams every commit | `rclone copy`, hourly (`paperweight-files-sync.timer`, CD-141) |
| Where | `<bucket>/paperweight/app.db` | `<bucket>/paperweight/files` |
| Region | ap-south-1 (CD-228) | ap-south-1, same bucket |
| Kept for | Daily snapshots for 90 days, with `-timestamp` restores inside that window (CD-228) | Forever: `copy` never deletes, so an older database restore still finds its files |
| Can lose in a disaster | About a second of commits (replica sync interval, default 1 s) | Up to an hour of uploads |

Retention is `snapshot.retention: 2160h` at the top level of
`deploy/litestream.yml` (default 24h). A full snapshot is stored each day, so the
bucket holds about 90 copies of the database: check its size against the storage
bill after a month. `files/tmp` holds uploads in progress and is never copied.

### Restore

**Never start Litestream on a fresh or empty database against a used bucket
path.** It snapshots that database and makes it the latest state. A plain
`restore` then returns that database and still exits 0, integrity check
included. The app creates a fresh `app.db` when it starts, so on a replacement
server neither service may start before the restore. If it has happened, see
[Started on a fresh database](#started-on-a-fresh-database).

On a replacement server, prepare it first, starting nothing:

- Install step 1 and step 2.
- Step 3: only the `cp` and `systemctl daemon-reload`, no `enable --now`.
- Steps 5 and 6: install the packages, config, env file and `systemctl edit`
  override, no `enable --now`. The Litestream package doesn't start itself.
- Step 4 comes after the restore.

Before restoring, on a replacement server or an existing one, write down two
things from memory or your own records, not from the database: how many users
the app should have, and the date of the newest record you remember. Step 4
compares the restored database with them.

Then:

1. `systemctl stop paperweight litestream`.
2. Move aside, if they exist, the old `app.db`, `app.db-wal` and `app.db-shm`,
   and Litestream's tracking folder `/var/lib/paperweight/.app.db-litestream`.
   The folder was confirmed in the drill: Litestream makes it beside the
   database, `restore` doesn't create or read it, and replication carried on
   correctly whether or not it was removed. Move it anyway so the restored
   database starts from clean state. `restore` refuses to write over an existing
   `app.db` without `-force`.
3. As root, load the bucket settings and restore, then hand the file back to
   the app user:
   ```sh
   set -a; . /etc/paperweight/litestream.env; set +a
   litestream restore -integrity-check full -o /var/lib/paperweight/app.db /var/lib/paperweight/app.db
   chown paperweight:paperweight /var/lib/paperweight/app.db
   ```
   (`-timestamp <RFC 3339 time>` restores to a point in time.) A restore writes
   only `app.db`, no `-wal` or `-shm`.
4. Compare the database with what you wrote down, because the exit code says
   nothing about whose data this is:
   ```sh
   sqlite3 /var/lib/paperweight/app.db 'SELECT count(*) FROM users; SELECT max(at) FROM audit_log'
   ```
   The first line is the user count and the second the newest change. Having
   some users proves nothing: a fresh database whose first admin was created
   before Litestream started shows one. If either value falls short of your
   note, start nothing and go to
   [Started on a fresh database](#started-on-a-fresh-database).
5. Copy `files/` back from the bucket:
   `rclone copy paperweight:$LITESTREAM_BUCKET/paperweight/files /var/lib/paperweight/files`
6. `chown -R paperweight:paperweight /var/lib/paperweight`. The restore and the
   integrity check run as root, but the app runs as `paperweight`, so it must
   own `app.db` and `files/` again or uploads fail.
7. `systemctl enable --now litestream paperweight paperweight-files-sync.timer`.
   Litestream carries on replicating to the same bucket path. On a replacement
   server, do install step 4 now; the first admin came back with the database.

### Started on a fresh database

Your older data is still in the bucket; recent commits may not be (see the end
of this section). Restore from a time before the mistake.

1. `systemctl stop paperweight litestream`. Do this straight away: the app must
   take no more work on the wrong database.
2. Find when Litestream first started on it, with
   `journalctl -u litestream --since "<when the server was set up>"`, and pick
   a time before that, in UTC, for example `2026-11-02T09:00:00Z`.
3. Do [Restore](#restore) steps 2 to 7, adding `-timestamp <that time>` to the
   `litestream restore` line. Step 4 must now match what you wrote down.
   Anything saved between that time and the mistake isn't recovered.

Both this and the replacement-server order were rehearsed with the development
configuration, from real data replicated and then moved aside. One of two long
rehearsals, 400 s on the fresh database, lost the commits made seconds before
the mistake, and neither `-timestamp` nor `-txid` recovered them. The cause and
the size of the loss aren't established, so treat recent commits as possibly
lost and check them.

### Development backups

From the repo root, with the app's data in `./data`:

```sh
bin/tools/litestream replicate -config deploy/litestream.dev.yml   # leave running
scripts/restore-drill/sync-files.sh                                # copies files/ to backup/files
bin/tools/litestream restore -config deploy/litestream.dev.yml -o /tmp/app.db ./data/app.db
```

`backup/` is gitignored because the repository is public. `bin/tools/litestream`
is Litestream v0.5.17 from the GitHub release, not checked in; verify the
download against the release's `checksums.txt`.

### Restore drill

`scripts/restore-drill.sh` proves the backup path in a temp directory, using
`deploy/litestream.dev.yml` and `sync-files.sh` unchanged. The writer
(`scripts/restore-drill/writer`, build tag `e2e`, because the release binary has
no fixture DocTypes) opens the real app, creates a requester and an approver, and
runs real `fixture.Order` approvals through the engine, with an attachment on
every fourth. It prints each approval only after its commit and keeps going past
200 with orders big enough to spill into the WAL. Once 200 are printed, the drill
freezes the writer with SIGSTOP to read the WAL, resuming it until the WAL holds
uncommitted frames, and only then sends SIGKILL, so the kill lands inside an open
transaction. Litestream catches up and stops, and `app.db` and `files/` are
restored into a new data directory. A
file removed between two syncs must still come back, so a `--delete` in the sync
fails the drill. It then checks `PRAGMA integrity_check`, that the database file
is byte-identical after a checkpoint, the SHA-256 of every file, and the approved
documents. It ends with:

```
integrity ok
database sha256 ok
files 50/50 sha256 ok
approvals 200/200
```

The approvals figure is the writer's own count, never a database's. The check
passes only if every id the writer printed is approved in the restored database,
the source and the restore hold exactly the same approved ids, and at most one of
them went unprinted. That one is a commit the kill stopped from being printed;
the drill then adds a line saying so. A kill that lands after a later commit shows that count on both sides,
for example `approvals 201/201`.

The writer waits for the replica before it starts approving. At full speed it
starves the write Litestream needs for its first sync (`create _litestream_seq
table: database is locked` in the log). Litestream retries and recovers, but one
stopped before the retry has replicated nothing.

Last run: 2026-10-30 on darwin/arm64, ten runs in a row, each `approvals 200/200`.
Removing a restored file, adding or dropping acknowledgements, swapping an
acknowledged id and a `--delete` in the sync each failed the drill.

### Go-live

Still to do before real data, because everything above ran only against local
storage:

- Create the bucket in ap-south-1 with S3 Block Public Access on and a key
  limited to it.
- Decide on keys. The steps above share one key, and Litestream's must be able
  to delete (retention), so anyone who gets that key can erase the backups. Give
  rclone its own key that can't delete, in the `RCLONE_CONFIG_PAPERWEIGHT_*`
  lines, or record that the owner accepts the shared one.
- Set up a way to notice failures, because neither announces itself. Litestream
  stays `active` while replication fails, and a failed timer run only shows in
  `systemctl --failed` and the journal. Options: Litestream's `heartbeat-url`
  setting with a monitor that alerts when the pings stop, and `OnFailure=` on
  `paperweight-files-sync.service`. In `journalctl -u litestream`, a single
  `sync error ... SQLITE_BUSY` followed by `sync recovered` is harmless; a
  climbing `consecutive_errors` means backups have stopped.
- Drill against the real bucket: restore `app.db` and `files/` onto a spare data
  directory and check them as the steps above do (CD-226). Record the date and
  the result here.
- Confirm the two systemd units run on the server.

## Upgrade

1. Copy the new binary to the server.
2. Take the pre-upgrade backup. With both services stopped the local database
   is the authority, so it is copied and becomes the rollback source. The bucket
   is then restored and compared with it, which shows whether replication has
   kept up (`systemctl stop litestream` exits 0 even when it hasn't). Paste the
   whole block:
   ```sh
   backup=/var/lib/paperweight/pre-upgrade-$(date +%Y%m%d-%H%M)
   set -a && . /etc/paperweight/litestream.env && set +a &&
   systemctl stop paperweight &&
   systemctl start paperweight-files-sync.service &&
   systemctl stop litestream &&
   mkdir "$backup" &&
   cp -p /var/lib/paperweight/app.db "$backup/app.db" &&
   { [ ! -e /var/lib/paperweight/app.db-wal ] || cp -p /var/lib/paperweight/app.db-wal "$backup/app.db-wal"; } &&
   sqlite3 "$backup/app.db" 'PRAGMA wal_checkpoint(TRUNCATE)' &&
   litestream restore -integrity-check full -o "$backup/replica.db" /var/lib/paperweight/app.db &&
   cmp "$backup/app.db" "$backup/replica.db" &&
   cp -p /usr/local/bin/app "$backup/app" &&
   systemctl start litestream &&
   echo "backup ok: $backup"
   ```
   If it doesn't end with `backup ok`, don't upgrade. Bring the site back with
   `systemctl start litestream paperweight`. If `cmp` reported a difference,
   the bucket is behind the server: read `journalctl -u litestream` for why, fix
   it, and take the backup again in a new folder. `$backup/app.db` is still a
   good copy, but the bucket isn't one until the comparison passes.
3. `install -m 0755 app-linux-amd64 /usr/local/bin/app`.
4. `systemctl start paperweight`.

Migrations run on start: `app serve` opens the database and applies every
pending migration before it listens (`app.OpenDB` in `internal/app/app.go`). To apply
them without starting the server, run
`sudo -u paperweight app migrate --data /var/lib/paperweight`. Leave Litestream
running during steps 3 and 4; it carries on replicating.

### Rollback

Migrations only go forward, so the old binary needs the old database. Rollback
loses everything saved since the upgrade. `$backup` is the folder from step 2,
and its `app.db` is the local copy, not the one restored from the bucket.

1. `systemctl stop paperweight litestream`.
2. Move the upgraded database aside:
   ```sh
   cd /var/lib/paperweight
   mkdir failed-upgrade
   mv app.db app.db-wal app.db-shm .app.db-litestream failed-upgrade/
   ```
3. Put back the database and the binary:
   ```sh
   install -o paperweight -g paperweight -m 0640 "$backup/app.db" /var/lib/paperweight/app.db
   install -m 0755 "$backup/app" /usr/local/bin/app
   ```
4. `systemctl start litestream paperweight`. Litestream takes the rolled-back
   database as the latest state, so a later restore returns it, not the failed
   upgrade (rehearsed with the development configuration).

Remove the `pre-upgrade-*` folder once the new version has run for a while.
`files/` needs no rollback: if the file GC removed something the old database
still names, `rclone copy` it back from the bucket, which keeps every file.

## File garbage collection

`app serve` removes unreferenced files hourly on its own (spec §7), along with
expired sessions and lockouts, so there is nothing to schedule.

By hand, as the app user:

```sh
sudo -u paperweight app files gc --data /var/lib/paperweight      # delete unused files now
sudo -u paperweight app files check --data /var/lib/paperweight   # compare attachment rows and files
```

`files check` prints what doesn't match and exits non-zero on any finding.

## Logs

The app writes `log/slog` JSON to stdout (spec §7), which systemd sends to
journald.

```sh
journalctl -u paperweight -f          # follow
journalctl -u paperweight --since today
journalctl -u litestream -f
```

Caddy's logs: `journalctl -u caddy`.
