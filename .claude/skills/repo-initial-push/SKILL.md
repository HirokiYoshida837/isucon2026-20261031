---
name: repo-initial-push
description: 競技サーバー上の webapp ディレクトリを git 化して、空の private リポジトリに初回 push する。「初期pushして」「webappをgit化して」「最初のpushをしたい」などで使う。
---

# webapp の初期 push（ISUCON 用）

競技サーバーにはじめから置かれている参照実装（`~/private_isu/webapp` など）には git 履歴が無い。
これを、空のまま作った private リポジトリへ競技サーバーから直接 push して、競技用リポジトリの出発点にする。

前提: 対象サーバーに `gh-setup` SKILL で gh を導入済み、かつユーザー本人が `gh auth login` 済み
（`gh auth status` で確認できる。protocol は SSH）。

## 手順
1. push 先の private リポジトリを GitHub 上に作っておく（空のままでよい。`gh repo view <org>/<repo> --json defaultBranchRef` で `defaultBranchRef.name` が空なら未 push）
2. 競技サーバーで GitHub への SSH 疎通を確認する
   ```
   ssh -T git@github.com
   ```
   「successfully authenticated」が出ればよい（shell access が無い旨のメッセージは正常）
3. 競技サーバーに git の identity が無ければ設定する（`git config --global user.name` が空なら未設定）
   ```
   gh api user --jq '{id: .id, login: .login}'
   git config --global user.name "<login>"
   git config --global user.email "<id>+<login>@users.noreply.github.com"
   git config --global init.defaultBranch main
   ```
   - noreply アドレスを使うのは、本人のメールアドレスをサーバーや git log に残さないため
4. push 対象のディレクトリ（例: `~/private_isu/webapp`）に `.gitignore` を置く
   - 言語ごとの依存物・仮想環境を除外する。最低限:
     ```
     vendor/
     .venv/
     __pycache__/
     *.pyc
     node_modules/
     ```
   - 既にサブディレクトリに `.gitignore` がある場合（例: Go 側）はそのまま活かしてよい
   - 除外すべきものが残っていないかは `du -sh */` で大きいディレクトリを事前に確認する
5. git 化して初回コミット
   ```
   cd ~/private_isu/webapp
   git init -q
   git add -A
   git status --short   # 意図しないファイル（vendor/ や .venv/ など）が入っていないか確認
   git commit -q -m "Initial import of private-isu reference implementation"
   ```
6. push
   ```
   git remote add origin git@github.com:<org>/<repo>.git
   git push -u origin HEAD:main
   ```
7. 確認
   ```
   gh repo view <org>/<repo> --json defaultBranchRef,url
   ```
   `defaultBranchRef.name` が `main` になっていれば成功

## 注意
- 対象は「ビルド・デプロイする実体のディレクトリ」（`makefile-setup` SKILL が前提にする `$ISU_REMOTE_DIR`、既定 `/home/isucon/webapp`）。
  `~/private_isu.git` のような上流参照実装のフルクローン（benchmarker・portal・provisioning 等を含む）はこの対象ではない
- 1 回しかやらない作業。2 回目以降の反映は `bench` の `deploy.sh`（`gh repo sync` / `gh pr checkout`）に任せる
- push 先が空リポジトリであることを必ず事前確認する。歴史が既にある private リポジトリに対しては使わない（force push が必要になり、既存コミットを消す可能性がある）
