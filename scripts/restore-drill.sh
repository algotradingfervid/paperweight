#!/usr/bin/env bash
# Restore drill (spec section 10 item 4, WP3 Task 6, CD-226). In a temp directory:
# Litestream replicates with deploy/litestream.dev.yml, a writer is killed with
# SIGKILL mid-write, Litestream catches up and stops, files/ is synced twice with
# a file removed in between, app.db is restored and files/ copied into a new
# data directory, and restore-drill verify checks
# PRAGMA integrity_check, the database content, the SHA-256 of every file and
# the approved count against the committed count.
#
# Expected output ends with `integrity ok` ... `approvals 200/200`.
# Needs Litestream v0.5.17 at bin/tools/litestream, or LITESTREAM=<path>.
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
LITESTREAM="${LITESTREAM:-$REPO/bin/tools/litestream}"
CONFIG="$REPO/deploy/litestream.dev.yml"
APPROVALS=200
LIMIT_SECONDS=60

if [ ! -x "$LITESTREAM" ]; then
	echo "Litestream not found at $LITESTREAM: install v0.5.17 there or set LITESTREAM" >&2
	exit 1
fi

WORK="$(mktemp -d "${TMPDIR:-/tmp}/paperweight-restore-drill.XXXXXX")"
LITESTREAM_PID=""
WRITER_PID=""
cleanup() {
	status=$?
	[ -z "$WRITER_PID" ] || kill -9 "$WRITER_PID" 2>/dev/null || true
	[ -z "$LITESTREAM_PID" ] || kill -9 "$LITESTREAM_PID" 2>/dev/null || true
	if [ "$status" -eq 0 ]; then
		rm -rf "$WORK"
	else
		echo "drill failed; logs kept in $WORK" >&2
	fi
}
trap cleanup EXIT

DRILL="$WORK/restore-drill"
(cd "$REPO" && go build -o "$DRILL" ./scripts/restore-drill)
cd "$WORK"
mkdir data backup restored

# wait_for_line <file> <line>: waits for a whole line, failing after LIMIT_SECONDS.
wait_for_line() {
	local tries=$((LIMIT_SECONDS * 10))
	while ! grep -qx "$2" "$1" 2>/dev/null; do
		tries=$((tries - 1))
		if [ "$tries" -le 0 ]; then
			echo "timed out waiting for '$2' in $1" >&2
			return 1
		fi
		sleep 0.1
	done
}

# ---- WRITER: the one part that changes when the e2e-tag app exists -----------
# Today: a temporary stand-in (scripts/restore-drill/standin.go) doing real
# committed WAL writes. Replace this block with: build `go build -tags e2e
# -o "$WORK/app" ./cmd/app`, start `app serve --data data`, drive $APPROVALS
# fixture approvals, and SIGKILL the app during the next one. Then set
# COUNT_SQL to count approved documents and delete standin.go.
#
# Contract: run_writer leaves data/app.db and data/files/ as the crash left
# them, with the writer dead, and sets COMMITTED to the approvals acknowledged.
COUNT_SQL="SELECT count(*) FROM drill_approvals WHERE state = 'approved'"
run_writer() {
	"$DRILL" standin-writer --data data --n "$APPROVALS" >writer.acks 2>writer.log &
	WRITER_PID=$!
	wait_for_line writer.acks inflight
	kill -9 "$WRITER_PID"
	wait "$WRITER_PID" 2>/dev/null || true
	WRITER_PID=""
	COMMITTED="$(grep -c '^committed ' writer.acks || true)"
	echo "writer killed with SIGKILL mid-write: $COMMITTED committed, $(wc -c <data/app.db-wal | tr -d ' ') WAL bytes on disk"
}
# ---- END WRITER --------------------------------------------------------------

"$LITESTREAM" replicate -config "$CONFIG" >litestream.log 2>&1 &
LITESTREAM_PID=$!

run_writer

# SIGTERM makes Litestream sync what is left and only then exit.
kill -TERM "$LITESTREAM_PID"
wait "$LITESTREAM_PID" || {
	echo "Litestream exited with an error" >&2
	exit 1
}
LITESTREAM_PID=""
echo "litestream caught up and stopped"

# Two syncs with a file removed in between: like rclone copy, the sync must keep
# what the app removed, or an older database restore loses its files. The file
# goes back for verify, so a --delete in the sync shows up as "missing".
"$REPO/scripts/restore-drill/sync-files.sh" data backup
removed="$(find data/files -type f -not -path 'data/files/tmp/*' | sort | head -1)"
mv "$removed" removed-between-syncs
"$REPO/scripts/restore-drill/sync-files.sh" data backup
mv removed-between-syncs "$removed"

"$LITESTREAM" restore -config "$CONFIG" -integrity-check full -o restored/app.db ./data/app.db >restore.log 2>&1 || {
	cat restore.log >&2
	exit 1
}
cp -R backup/files restored/files
echo "restored into a new data directory"

"$DRILL" verify --source data --restored restored --committed "$COMMITTED" --count-sql "$COUNT_SQL"
