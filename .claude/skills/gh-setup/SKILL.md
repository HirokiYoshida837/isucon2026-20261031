---
name: gh-setup
description: ISUCON2026 の競技サーバーに gh（GitHub CLI）を導入する。「ghをセットアップして」「gh install して」「gh auth setup」などで使う。
---

# gh セットアップ（ISUCON 用）

`infra/scripts/setup-gh.sh`（gh の導入のみ）を各サーバーで実行する。認証は Claude ではなくユーザー本人が行う。

git 操作は HTTPS ではなく SSH 鍵で行う方針。`gh auth login` の中で鍵の生成・GitHub アカウントへの
登録までできるので、HTTPS 用の `gh auth setup-git` は不要。ログイン後に Claude 側でやることは無い。

## スクリプト（リポジトリの `.claude/skills/gh-setup/`）
| スクリプト | やること |
|---|---|
| `install.sh [ホスト...]` | 各ホストに `infra/scripts` を配布し `setup-gh.sh` を実行（gh 導入のみ） |

- ホストの既定値は `~/.config/isucon/isucon.env` の `ISU_HOSTS`（bench と共通の設定ファイル）
- webapp リポジトリの remote が SSH（`git@github.com:...`）になっているか、念のため確認しておく（`infra/scripts/setup-gh.sh` 冒頭のコメント参照）

## 流れ
1. `install.sh` を実行する。各ホストの結果（✅/❌）を確認し、失敗があれば原因を見て再実行する
2. 各ホストで、ユーザー本人に次を実行してもらう
   ```
   ssh <host> 'gh auth login'
   ```
   - preferred protocol は **SSH** を選ぶ
   - "Generate a new SSH key to add to your GitHub account?" は **Y**
   - 表示される URL とコードは手元のブラウザで開いて入力する
3. 以上で完了。Claude 側で追加の作業（`gh auth setup-git` 等）は不要
