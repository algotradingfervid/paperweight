#!/usr/bin/env bash
# Development equivalent of deploy/paperweight-files-sync.service: copies
# <data>/files to <backup>/files, leaving out the upload spool (files/tmp).
# Like rclone copy, it never deletes, so an older database restore still finds
# its files. Usage: scripts/restore-drill/sync-files.sh [data] [backup]
set -euo pipefail

data="${1:-data}"
backup="${2:-backup}"
mkdir -p "$backup/files"
rsync -a --exclude '/tmp/' "$data/files/" "$backup/files/"
