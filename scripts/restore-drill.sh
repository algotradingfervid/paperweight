#!/usr/bin/env bash
# Restore drill (spec section 10 item 4, WP3 Task 6, CD-226). In a temp directory:
# Litestream replicates with deploy/litestream.dev.yml, an e2e-tag writer runs
# real fixture.Order approvals on the real app and is killed with SIGKILL inside
# an open transaction once 200 are committed, Litestream catches up and stops,
# files/ is synced twice with a file removed in between, app.db is restored and
# files/ copied into a new data directory, and restore-drill verify checks
# PRAGMA integrity_check, the database content, the SHA-256 of every file and
# the approved documents against the writer's own output.
#
# The count rule (checkApprovals in scripts/restore-drill/verify.go): every id
# the writer printed is approved in the restored database, the source and the
# restore hold exactly the same approved ids, and at most one of them went
# unprinted (a commit the kill stopped from being printed). The final figure is
# the writer's count.
#
# Expected output ends with `integrity ok` ... `approvals 200/200`. A kill that
# lands after the next commit shows a higher count, still equal on both sides.
# Needs Litestream v0.5.17 at bin/tools/litestream, or LITESTREAM=<path>.
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
LITESTREAM="${LITESTREAM:-$REPO/bin/tools/litestream}"
CONFIG="$REPO/deploy/litestream.dev.yml"
APPROVALS=200

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
WRITER="$WORK/writer"
(cd "$REPO" && go build -o "$DRILL" ./scripts/restore-drill)
(cd "$REPO" && go build -tags e2e -o "$WRITER" ./scripts/restore-drill/writer)
cd "$WORK"
mkdir data backup restored

# Leaves data/app.db and data/files/ as the crash left them, with the writer
# dead. Past $APPROVALS the orders are heavy, so kill-inflight finds a real
# engine transaction's pages in the WAL with no commit frame and kills then.
APPROVED_SQL="SELECT id FROM fixture_order WHERE state = 'approved'"
run_writer() {
	"$WRITER" --data data --replica backup/app.db --n "$APPROVALS" >writer.acks 2>writer.log &
	WRITER_PID=$!
	uncommitted="$("$DRILL" kill-inflight --acks writer.acks --after "$APPROVALS" --wal data/app.db-wal --pid "$WRITER_PID")"
	wait "$WRITER_PID" 2>/dev/null || true
	WRITER_PID=""
	committed="$(grep -c '^committed ' writer.acks || true)"
	echo "writer killed with SIGKILL mid-write: $committed committed, $uncommitted uncommitted WAL frames at the kill"
}

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

"$DRILL" verify --source data --restored restored --acks writer.acks --approved-sql "$APPROVED_SQL"
