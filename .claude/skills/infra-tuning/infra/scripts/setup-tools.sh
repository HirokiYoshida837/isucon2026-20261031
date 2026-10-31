#!/usr/bin/env bash
# 計測ツールを入れる（全サーバーで実行）。何度実行してもよい。
#   ./setup-tools.sh            # 標準セット
#   ./setup-tools.sh --pprof    # go tool pprof -http の描画用に graphviz も入れる（数十 MB 使う）
#
# 入るもの
# - alp           : nginx アクセスログ集計（GitHub Releases のバイナリを /usr/local/bin に置く）
# - percona-toolkit: pt-query-digest（スロークエリログ集計）
# - htop / sysstat(sar, mpstat, pidstat, iostat) / dstat（apt にあれば）/ jq / unzip
#
# 注意
# - apt を使う。初回起動直後は unattended-upgrades がロックを持つことがあるので、最大 5 分待つ。
# - needrestart がサービス（mysql 等）を勝手に再起動しないよう NEEDRESTART_SUSPEND=1 で抑止する。
set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

ALP_VERSION="${ALP_VERSION:-1.0.22}"
WITH_PPROF=0
for arg in "$@"; do
  case "$arg" in
    --pprof) WITH_PPROF=1 ;;
    -h|--help) sed -n '2,15p' "$0"; exit 0 ;;
    *) die "不明な引数: $arg" ;;
  esac
done

require_sudo

##
# アーキテクチャ判定
##
case "$(uname -m)" in
  x86_64|amd64)  ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) die "未対応のアーキテクチャ: $(uname -m)" ;;
esac
log "arch=$ARCH"

##
# apt パッケージ
##
APT_ENV=(env DEBIAN_FRONTEND=noninteractive NEEDRESTART_SUSPEND=1 NEEDRESTART_MODE=l)
APT_OPTS=(-y -q -o DPkg::Lock::Timeout=300)

pkgs=(htop sysstat percona-toolkit jq unzip curl)
[[ $WITH_PPROF -eq 1 ]] && pkgs+=(graphviz)

# dstat は Ubuntu 24.04 のリポジトリから外れている可能性がある。候補があるときだけ入れる。
if apt-cache policy dstat 2>/dev/null | grep -q 'Candidate: [0-9]'; then
  pkgs+=(dstat)
else
  warn "dstat は apt に無いので入れません（代わりに vmstat / mpstat / pidstat を使う）"
fi

missing=()
for p in "${pkgs[@]}"; do
  dpkg -s "$p" >/dev/null 2>&1 || missing+=("$p")
done

if [[ ${#missing[@]} -gt 0 ]]; then
  log "apt install: ${missing[*]}"
  as_root "${APT_ENV[@]}" apt-get update -q -o DPkg::Lock::Timeout=300
  as_root "${APT_ENV[@]}" apt-get install "${APT_OPTS[@]}" --no-install-recommends "${missing[@]}"
else
  log "apt パッケージは導入済み"
fi

# sysstat の定期収集（sar 用）を有効にする。履歴は /var/log/sysstat に溜まる。
if [[ -f /etc/default/sysstat ]] && ! grep -q '^ENABLED="true"' /etc/default/sysstat; then
  as_root sed -i 's/^ENABLED=.*/ENABLED="true"/' /etc/default/sysstat
  as_root systemctl enable --now sysstat >/dev/null 2>&1 || true
  log "sysstat の定期収集を有効化"
fi

##
# alp
##
current_alp=""
if has_cmd alp; then
  current_alp="$(alp --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1 || true)"
fi
if [[ $current_alp == "$ALP_VERSION" ]]; then
  log "alp $ALP_VERSION は導入済み"
else
  log "alp $ALP_VERSION を導入（現在: ${current_alp:-なし}）"
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' EXIT
  base="https://github.com/tkuchiki/alp/releases/download/v${ALP_VERSION}"
  tarball="alp_linux_${ARCH}.tar.gz"
  curl -fsSL -o "$tmp/$tarball" "$base/$tarball"
  # チェックサムがあれば検証する。
  if curl -fsSL -o "$tmp/checksums.txt" "$base/alp_${ALP_VERSION}_checksums.txt"; then
    (cd "$tmp" && grep " ${tarball}\$" checksums.txt | sha256sum -c -) || die "alp のチェックサム不一致"
  else
    warn "alp のチェックサムファイルが取れないため検証を省略"
  fi
  tar -xzf "$tmp/$tarball" -C "$tmp"
  as_root install -m 0755 "$tmp/alp" /usr/local/bin/alp
fi

##
# 結果表示
##
log "導入結果"
for c in alp pt-query-digest htop sar mpstat pidstat iostat dstat jq; do
  if has_cmd "$c"; then printf '  %-16s %s\n' "$c" "$(command -v "$c")"
  else printf '  %-16s %s\n' "$c" "(なし)"; fi
done
alp --version 2>/dev/null | sed 's/^/  /' || true
pt-query-digest --version 2>/dev/null | sed 's/^/  /' || true
