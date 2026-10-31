#!/usr/bin/env bash
# 競技リポジトリに Claude Code の土台を置く
# 使い方: .claude/skills/harness-setup/install.sh <競技リポジトリのパス>
set -euo pipefail

SKILL_DIR=$(cd "$(dirname "$0")" && pwd)
PREP_ROOT=$(cd "$SKILL_DIR/../../.." && pwd)
TARGET=${1:-}

if [ -z "$TARGET" ] || [ ! -d "$TARGET" ]; then
  echo "使い方: $0 <競技リポジトリのパス>" >&2
  exit 1
fi
TARGET=$(cd "$TARGET" && pwd)
if [ "$TARGET" = "$PREP_ROOT" ]; then
  echo "prep 自身には置けない。競技リポジトリのパスを渡す" >&2
  exit 1
fi

placed=()

# PR テンプレート
mkdir -p "$TARGET/.github"
cp "$SKILL_DIR/templates/pull_request_template.md" "$TARGET/.github/pull_request_template.md"
placed+=(".github/pull_request_template.md")

# CLAUDE.md（守ること・進め方・環境を 1 枚に。既にあれば残す。埋めた値を消さないため）
if [ -e "$TARGET/CLAUDE.md" ]; then
  echo "skip: CLAUDE.md は既にある（雛形: $SKILL_DIR/templates/CLAUDE.md.template）"
else
  cp "$SKILL_DIR/templates/CLAUDE.md.template" "$TARGET/CLAUDE.md"
  placed+=("CLAUDE.md（雛形から）")
fi

# prep の SKILL を全部写す（当日はどちらのリポジトリで Claude Code を開いても同じ SKILL が使えるようにするため）
# SKILL ごとに --delete するので prep で消したファイルは競技リポジトリからも消える。
# 競技リポジトリにしか無い SKILL（bench-setup / db-migrate-setup が当日に作る bench / db-migrate）はそのまま残る
mkdir -p "$TARGET/.claude/skills"
SKILLS=""
for d in "$PREP_ROOT"/.claude/skills/*/; do
  s=$(basename "$d")
  rsync -a --delete --exclude .DS_Store "$d" "$TARGET/.claude/skills/$s/"
  SKILLS="$SKILLS $s"
done
placed+=(".claude/skills/ (${SKILLS# })")

# .gitignore（手元に出るファイルを除外）
touch "$TARGET/.gitignore"
for line in "bench-logs/" ".claude/settings.local.json" "CLAUDE.local.md" ".DS_Store"; do
  grep -qxF "$line" "$TARGET/.gitignore" || echo "$line" >> "$TARGET/.gitignore"
done
placed+=(".gitignore に除外を追記")

echo "置いたもの（${TARGET}）:"
printf '  - %s\n' "${placed[@]}"
n=$(grep -o '<<[^>]*>>' "$TARGET/CLAUDE.md" | wc -l | tr -d ' ')
echo "CLAUDE.md の未記入 <<...>>: $n 個"
grep -o '<<[^>]*>>' "$TARGET/CLAUDE.md" | sed 's/^/  /' || true
echo "次: CLAUDE.md の <<...>> を埋めて commit する"
