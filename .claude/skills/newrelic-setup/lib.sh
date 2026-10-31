# ISUCON2026 newrelic-setup 共通設定（source して使う。単体では実行しない）
# shellcheck disable=SC2034
SKILL_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# 当日の環境設定（ホスト一覧・アプリの systemd ユニット名）
ISU_ENV_FILE="${ISU_ENV_FILE:-$HOME/.config/isucon/isucon.env}"
[ -f "$ISU_ENV_FILE" ] && { set -a; . "$ISU_ENV_FILE"; set +a; }
: "${ISU_HOSTS:=isu1 isu2 isu3}"     # ~/.ssh/config の Host 名（スペース区切り）
: "${ISU_APP_UNIT:=}"                # アプリの systemd ユニット名（例: isu-go、isuride-go.service）

# NewRelic の秘密情報（中身を表示・出力しない）
NR_ENV_FILE="${NEW_RELIC_ENV_FILE:-$HOME/.config/isucon/newrelic.env}"
[ -f "$NR_ENV_FILE" ] && { set -a; . "$NR_ENV_FILE"; set +a; }
: "${NEW_RELIC_APP_NAME:=isucon2026}"
NR_GRAPHQL="${NEW_RELIC_GRAPHQL_URL:-https://api.newrelic.com/graphql}"

# 各サーバー側の置き場
REMOTE_ENV=/home/isucon/newrelic.env
INFRA_CONF=/etc/newrelic-infra.yml

NRQL="$SKILL_DIR/../newrelic-nrql/nrql.sh"
SSH_OPTS=(-o ConnectTimeout=10 -o BatchMode=yes)

log()  { printf '\033[36m[%s]\033[0m %s\n' "$(date +%H:%M:%S)" "$*" >&2; }
warn() { printf '\033[33m[WARN]\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[31m[ERROR]\033[0m %s\n' "$*" >&2; exit 1; }

# 引数があればそれをホスト一覧、無ければ ISU_HOSTS
hosts_or_default() { if [ $# -gt 0 ]; then echo "$*"; else echo "$ISU_HOSTS"; fi; }

need_license() { [ -n "${NEW_RELIC_LICENSE_KEY:-}" ] || die "NEW_RELIC_LICENSE_KEY が無い。先に fetch-license-key.sh を実行"; }
need_unit() { [ -n "$ISU_APP_UNIT" ] || die "アプリの systemd ユニット名が必要（引数か isucon.env の ISU_APP_UNIT）"; }

# リモートで bash スクリプト（標準入力）を実行する。$1=host、残りはスクリプトへの引数
remote_bash() { local h="$1"; shift; ssh "${SSH_OPTS[@]}" "$h" "bash -s -- $(printf '%q ' "$@")"; }
