#!/usr/bin/env bash
# gh（GitHub CLI）を入れる（各競技サーバーで isucon ユーザーで実行）。認証はこのスクリプトではやらない。
#   ./setup-gh.sh
#
# 何に使うか
# - Makefile の deploy が `gh repo sync --branch` でブランチ最新を取得する前提（harness/README.md）。
#
# 導入後、認証は手動で行う（ssh 越しでもデバイスフローで完結する。トークンファイルは使わない）。
#   gh auth login
#     - "preferred protocol for Git operations" は SSH を選ぶ
#     - "Generate a new SSH key to add to your GitHub account?" は Y
#       （鍵の生成と GitHub アカウントへの登録までこの 1 回でやってくれる）
#     - 表示される URL とコードは手元のブラウザで開いて入力する
#
# git 操作（fetch/pull、gh repo sync）はこの SSH 鍵で行う。HTTPS 用の credential helper
# （gh auth setup-git）は使わない。webapp リポジトリの remote が SSH になっているか確認する。
#   git -C <webapp のパス> remote -v                                      # 確認
#   git -C <webapp のパス> remote set-url origin git@github.com:<org>/<repo>.git  # 必要なら SSH に変更
#
# 何度実行してもよい（インストール済みなら飛ばす）。
set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

##
# インストール（公式 apt リポジトリ）
##
if has_cmd gh; then
  log "gh は導入済み: $(gh --version | head -1)"
else
  require_sudo
  log "gh の apt リポジトリを追加"
  as_root mkdir -p -m 755 /etc/apt/keyrings
  tmp="$(mktemp)"
  trap 'rm -f "$tmp"' EXIT
  curl -fsSL -o "$tmp" https://cli.github.com/packages/githubcli-archive-keyring.gpg
  as_root install -m 0644 "$tmp" /etc/apt/keyrings/githubcli-archive-keyring.gpg

  arch="$(dpkg --print-architecture)"
  echo "deb [arch=$arch signed-by=/etc/apt/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
    | as_root tee /etc/apt/sources.list.d/github-cli.list >/dev/null

  as_root env DEBIAN_FRONTEND=noninteractive NEEDRESTART_SUSPEND=1 NEEDRESTART_MODE=l \
    apt-get update -q -o DPkg::Lock::Timeout=300
  as_root env DEBIAN_FRONTEND=noninteractive NEEDRESTART_SUSPEND=1 NEEDRESTART_MODE=l \
    apt-get install -y -q -o DPkg::Lock::Timeout=300 --no-install-recommends gh
  log "導入: $(gh --version | head -1)"
fi

##
# 認証状態の確認（認証そのものは手動）
##
if gh auth status >/dev/null 2>&1; then
  log "認証済み"
  gh auth status
else
  warn "未認証。続けて以下を実行すること:"
  warn "  gh auth login   # protocol は SSH、SSH key の生成・登録も Y にする"
fi
