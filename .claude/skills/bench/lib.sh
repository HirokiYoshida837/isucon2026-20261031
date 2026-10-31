# ISUCON2026 deploy/bench スクリプト共通設定（source して使う）
SKILL_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ISU_ENV_FILE="${ISU_ENV_FILE:-$HOME/.config/isucon/isucon.env}"
[ -f "$ISU_ENV_FILE" ] && { set -a; . "$ISU_ENV_FILE"; set +a; }
: "${ISU_HOSTS:=isu1 isu2 isu3}"                 # ~/.ssh/config の Host 名（スペース区切り）
: "${ISU_REMOTE_DIR:=/home/isucon/webapp}"       # 競技サーバー上の git リポジトリ
: "${ISU_DEPLOY_CMD:=make deploy}"               # 各サーバーで実行するデプロイコマンド
: "${ISU_BEFORE_CMD:=}"                          # ベンチ前に各サーバーで実行（ログローテート等）
: "${ISU_AFTER_CMD:=}"                           # ベンチ後に各サーバーで実行（標準出力が集計結果）
: "${ISU_LOG_DIR:=$(cd "$SKILL_DIR/../../.." && pwd)/bench-logs}"  # スコア履歴と集計結果の保存先（既定はリポジトリ直下。.gitignore 済み）
: "${ISU_MEMBER:=$(git config user.name 2>/dev/null || echo member)}"
: "${ISU_APP_UNIT:=isureal-go}"                  # アプリの systemd unit（pprof の負荷検知でアクションログを読む）
: "${ISU_PPROF_HOSTS:=$ISU_HOSTS}"               # ベンチ中に pprof を取るホスト（pprof が無いホストは自動でスキップ）
: "${ISU_PPROF_PORT:=6060}"                      # アプリの pprof ポート（go/pprof.go。127.0.0.1 のみ）
: "${ISU_PPROF_SECONDS:=60}"                     # CPU プロファイルの長さ（秒）。負荷走行は開始 15 秒〜90 秒あたり
: "${ISU_PPROF_TRIGGER:=5}"                      # 負荷開始とみなす、アクションログの 3 秒あたりの行数
: "${ISU_PPROF_MAX_WAIT:=240}"                   # 負荷開始を待つ上限（秒）。超えたらそのまま取る
POST="$SKILL_DIR/discord-post.sh"
NRQL="$SKILL_DIR/../newrelic-nrql/nrql.sh"
SSH_OPTS=(-o ConnectTimeout=10 -o BatchMode=yes)
mkdir -p "$ISU_LOG_DIR"
# 時刻表示（JST）。引数はエポック秒
jst() { TZ=Asia/Tokyo date -r "$1" '+%Y-%m-%d %H:%M:%S JST' 2>/dev/null || TZ=Asia/Tokyo date -d "@$1" '+%Y-%m-%d %H:%M:%S JST'; }
