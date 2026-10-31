---
name: repo-clone-setup
description: 2台目以降の競技サーバーで、初期配布された webapp ディレクトリを GitHub の private リポジトリ（1台目が repo-initial-push した先）と同期する git リポジトリに置き換える。「2台目をセットアップして」「他のサーバーもgit化して」「リモート設定して」などで使う。
---

# 2台目以降の webapp 同期セットアップ（ISUCON 用）

1台目は `repo-initial-push` SKILL で git 化して GitHub に push 済み。
2台目以降は「初期配布された webapp と同じ内容」が既に置かれているだけで git 履歴が無いので、
push はせず、1台目が push した内容を clone して置き換える。

## 前提
- 1台目で `repo-initial-push` が完了し、GitHub の private リポジトリに `main` ブランチがある
  （`gh repo view <org>/<repo> --json defaultBranchRef` で確認できる）
- 対象サーバーに `gh-setup` SKILL で gh 導入・認証済み（`gh auth status` で確認）

## 手順
1. 対象サーバーで GitHub への SSH 疎通を確認する
   ```
   ssh -T git@github.com
   ```
2. GitHub の内容を一時ディレクトリに clone し、配布されている webapp ディレクトリと diff する
   （上書きする前に必ず行う。差分の正体を確認せずに進めない）
   ```
   git clone --branch main git@github.com:<org>/<repo>.git /tmp/webapp-check
   diff -rq \
     --exclude=.git --exclude=vendor --exclude=.venv --exclude=node_modules --exclude=__pycache__ \
     ~/private_isu/webapp /tmp/webapp-check
   ```
3. diff の結果で分岐する
   - 差分なし（対象ディレクトリの除外パターンは `repo-initial-push` の `.gitignore` と揃える）→ 4 に進む
   - 差分あり（DB 接続情報やサーバー固有の設定ファイルなど）→ **ここで止めて人に相談する**。
     差分ファイルの内容を確認し、どちらを正とするか・どう反映するかを決めてから進める。
     推測で上書きしない
4. 既存ディレクトリをバックアップしてから clone で置き換える
   ```
   mv ~/private_isu/webapp ~/private_isu/webapp.orig
   git clone --branch main git@github.com:<org>/<repo>.git ~/private_isu/webapp
   ```
5. 1台目と同じコミットになっているか確認する
   ```
   cd ~/private_isu/webapp
   git remote -v
   git status
   git log --oneline -1
   ```
   1台目の `git log --oneline -1` と一致することを確認する
6. 問題なければ一時ファイルを消す
   ```
   rm -rf ~/private_isu/webapp.orig /tmp/webapp-check
   ```
7. 以降の反映は `bench` の `deploy.sh`（Makefile の `deploy` ターゲット、
   `gh repo sync` / `gh pr checkout`）に任せる。全台でこの SKILL を実行しておけば、
   `deploy.sh` が全台共通で動く

## 注意
- 2台目以降で `repo-initial-push`（`git init` → `commit` → `push`）をやってはいけない。
  重複したコミット履歴や push の衝突（1台目の履歴を上書き）になる
- diff で差分が見つかった場合、何も考えずに上書きしない。サーバー固有の設定（DB接続情報、
  証明書、ポート番号など）がある場合は、競技用の設定管理方法（環境ごとに別管理、Ansible 等）
  を別途検討する。このリポジトリでは未確定（2026-10-28 時点）
- `~/private_isu/webapp.orig`（バックアップ）は確認が終わるまで消さない。消すのは手順 6 の
  diff・ログ確認が済んだあとだけ
