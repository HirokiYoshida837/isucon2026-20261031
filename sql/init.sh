#!/bin/bash

set -eu
cd -- "$(dirname -- "$0")"

if test -f /home/isucon/env.sh; then
	. /home/isucon/env.sh
fi

ISUREAL_DB_HOST=${ISUREAL_DB_HOST:-127.0.0.1}
ISUREAL_DB_PORT=${ISUREAL_DB_PORT:-5432}
ISUREAL_DB_USER=${ISUREAL_DB_USER:-isureal}
ISUREAL_DB_PASSWORD=${ISUREAL_DB_PASSWORD:-isureal}
ISUREAL_DB_NAME=${ISUREAL_DB_NAME:-isureal}

PGPASSWORD="$ISUREAL_DB_PASSWORD" psql \
	--no-psqlrc \
	--no-password \
	--host="$ISUREAL_DB_HOST" \
	--port="$ISUREAL_DB_PORT" \
	--username="$ISUREAL_DB_USER" \
	--dbname="$ISUREAL_DB_NAME" \
	--set=ON_ERROR_STOP=1 \
	--file=reset.sql \
	--file=80-add-index.sql \
	--file=81-follow-func.sql \
	--file=82-friend-request-func.sql \
	--file=83-login-func.sql \
	--file=84-image-keys.sql \
	--file=85-friend-edges.sql \
	--file=86-window-notifications.sql \
	--file=87-create-post-func.sql \
	--file=88-posts-local-date.sql \
	--file=89-mutual-friends.sql \
	--file=90-interaction-func.sql \
	--file=91-delete-post-func.sql \
	--file=92-drop-redundant-index.sql \
	--file=93-image-unlogged.sql \
	--file=94-block-func.sql \
	--file=95-open-posting-window-func.sql \
	--file=96-vacuum-users.sql
