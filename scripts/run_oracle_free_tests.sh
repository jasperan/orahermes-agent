#!/usr/bin/env bash
# Run the pytest suite against a local Oracle Database Free container.
#
# This avoids requiring cloud ORACLE_DSN/ORACLE_USER/ORACLE_PASSWORD values:
# the script starts or reuses a Docker container, creates an isolated schema,
# pre-initializes the Oracle session tables, then delegates to run_tests.sh.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

CONTAINER="${ORAHERMES_ORACLE_CONTAINER:-orahermes-oracle-test}"
IMAGE="${ORAHERMES_ORACLE_IMAGE:-container-registry.oracle.com/database/free:latest-lite}"
SYS_PASSWORD="${ORAHERMES_ORACLE_SYS_PASSWORD:-OraHermesTest12345}"
HOST_PORT="${ORAHERMES_ORACLE_PORT:-1621}"
SERVICE="${ORAHERMES_ORACLE_SERVICE:-FREEPDB1}"

if [ -n "${ORAHERMES_ORACLE_TEST_USER:-}" ]; then
  TEST_USER="${ORAHERMES_ORACLE_TEST_USER^^}"
  EPHEMERAL_SCHEMA=0
else
  TEST_USER="HERMES_T_$(date +%s)_$RANDOM"
  EPHEMERAL_SCHEMA=1
fi
TEST_PASSWORD="${ORAHERMES_ORACLE_TEST_PASSWORD:-HermesTest12345}"

if [[ ! "$TEST_USER" =~ ^[A-Z][A-Z0-9_]{0,29}$ ]]; then
  echo "error: ORAHERMES_ORACLE_TEST_USER must match ^[A-Z][A-Z0-9_]{0,29}$" >&2
  exit 1
fi

if [[ ! "$TEST_PASSWORD" =~ ^[A-Za-z][A-Za-z0-9_#]{7,127}$ ]]; then
  echo "error: ORAHERMES_ORACLE_TEST_PASSWORD must be 8+ chars and use only letters, numbers, _, or #" >&2
  exit 1
fi

for required in docker; do
  if ! command -v "$required" >/dev/null 2>&1; then
    echo "error: $required is required for local Oracle Free tests" >&2
    exit 1
  fi
done

VENV=""
for candidate in "$REPO_ROOT/.venv" "$REPO_ROOT/venv" "$HOME/.hermes/hermes-agent/venv"; do
  if [ -f "$candidate/bin/activate" ]; then
    VENV="$candidate"
    break
  fi
done

if [ -z "$VENV" ]; then
  echo "error: no virtualenv found in $REPO_ROOT/.venv or $REPO_ROOT/venv" >&2
  exit 1
fi
PYTHON="$VENV/bin/python"

if docker inspect "$CONTAINER" >/dev/null 2>&1; then
  if [ -z "${ORAHERMES_ORACLE_SYS_PASSWORD+x}" ]; then
    inspected_pwd="$(
      docker inspect "$CONTAINER" --format '{{range .Config.Env}}{{println .}}{{end}}' \
        | awk -F= '$1 == "ORACLE_PWD" {print substr($0, index($0, "=") + 1); exit}'
    )"
    if [ -n "$inspected_pwd" ]; then
      SYS_PASSWORD="$inspected_pwd"
    fi
  fi
  if [ "$(docker inspect "$CONTAINER" --format '{{.State.Running}}')" != "true" ]; then
    docker start "$CONTAINER" >/dev/null
  fi
else
  echo "▶ starting Oracle Free container $CONTAINER on 127.0.0.1:$HOST_PORT"
  docker run -d \
    --name "$CONTAINER" \
    -p "127.0.0.1:$HOST_PORT:1521" \
    -e ORACLE_PWD="$SYS_PASSWORD" \
    "$IMAGE" >/dev/null
fi

echo "▶ waiting for Oracle Free container $CONTAINER to be ready"
deadline=$((SECONDS + 900))
while true; do
  health="$(
    docker inspect "$CONTAINER" \
      --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}'
  )"
  if [ "$health" = "healthy" ]; then
    break
  fi
  if docker exec "$CONTAINER" bash -lc \
    "source /home/oracle/.bashrc 2>/dev/null || true; echo 'SELECT 1 FROM dual;' | sqlplus -s / as sysdba" \
      >/dev/null 2>&1; then
    break
  fi
  if [ "$SECONDS" -ge "$deadline" ]; then
    echo "error: Oracle container $CONTAINER was not ready after 900s (last state: $health)" >&2
    exit 1
  fi
  sleep 10
done

HOST_BINDING="$(docker port "$CONTAINER" 1521/tcp | head -n 1 || true)"
if [ -z "$HOST_BINDING" ]; then
  echo "error: container $CONTAINER does not publish 1521/tcp to the host" >&2
  exit 1
fi
HOST_PORT_ACTUAL="${HOST_BINDING##*:}"
ORACLE_DSN="127.0.0.1:${HOST_PORT_ACTUAL}/${SERVICE}"

DROP_AT_END="${ORAHERMES_DROP_TEST_SCHEMA:-$EPHEMERAL_SCHEMA}"
RESET_SCHEMA="${ORAHERMES_RESET_TEST_SCHEMA:-$EPHEMERAL_SCHEMA}"

drop_schema() {
  if [ "${DROP_AT_END:-0}" != "1" ]; then
    return
  fi
  docker exec -i "$CONTAINER" bash -lc \
    "source /home/oracle/.bashrc 2>/dev/null || true; sqlplus -s / as sysdba" >/dev/null 2>&1 <<SQL || true
WHENEVER SQLERROR CONTINUE
ALTER SESSION SET CONTAINER = ${SERVICE};
DROP USER ${TEST_USER} CASCADE;
EXIT
SQL
}
trap drop_schema EXIT

echo "▶ preparing Oracle schema $TEST_USER on $ORACLE_DSN"
if [ "$RESET_SCHEMA" = "1" ]; then
  docker exec -i "$CONTAINER" bash -lc \
    "source /home/oracle/.bashrc 2>/dev/null || true; sqlplus -s / as sysdba" >/dev/null <<SQL
WHENEVER SQLERROR CONTINUE
ALTER SESSION SET CONTAINER = ${SERVICE};
DROP USER ${TEST_USER} CASCADE;
EXIT
SQL
fi

docker exec -i "$CONTAINER" bash -lc \
  "source /home/oracle/.bashrc 2>/dev/null || true; sqlplus -s / as sysdba" <<SQL
WHENEVER SQLERROR EXIT SQL.SQLCODE
ALTER SESSION SET CONTAINER = ${SERVICE};
CREATE USER ${TEST_USER} IDENTIFIED BY "${TEST_PASSWORD}"
  DEFAULT TABLESPACE USERS QUOTA UNLIMITED ON USERS;
GRANT CONNECT, RESOURCE, CTXAPP TO ${TEST_USER};
GRANT CREATE SESSION, CREATE TABLE, CREATE SEQUENCE, CREATE TRIGGER, CREATE PROCEDURE, CREATE VIEW, CREATE TYPE TO ${TEST_USER};
ALTER USER ${TEST_USER} QUOTA UNLIMITED ON USERS;
EXIT
SQL

echo "▶ initializing Oracle session schema"
ORACLE_DSN="$ORACLE_DSN" \
ORACLE_USER="$TEST_USER" \
ORACLE_PASSWORD="$TEST_PASSWORD" \
"$PYTHON" - <<'PY'
from oracle_state import OracleSessionDB

db = OracleSessionDB()
db.close()
PY

set +e
ORAHERMES_ALLOW_ORACLE_TEST_DB=1 \
ORACLE_DSN="$ORACLE_DSN" \
ORACLE_USER="$TEST_USER" \
ORACLE_PASSWORD="$TEST_PASSWORD" \
"$REPO_ROOT/scripts/run_tests.sh" "$@"
status=$?
set -e

exit "$status"
