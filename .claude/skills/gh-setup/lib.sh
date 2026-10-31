# gh-setup スクリプト共通設定（source して使う）
SKILL_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ISU_ENV_FILE="${ISU_ENV_FILE:-$HOME/.config/isucon/isucon.env}"
[ -f "$ISU_ENV_FILE" ] && { set -a; . "$ISU_ENV_FILE"; set +a; }
: "${ISU_HOSTS:=isu1 isu2 isu3}"                      # ~/.ssh/config の Host 名（スペース区切り。bench と共通）
: "${ISU_INFRA_REMOTE_DIR:=/home/isucon/infra}"       # サーバー上の infra/ の置き場
SSH_OPTS=(-o ConnectTimeout=10 -o BatchMode=yes)
