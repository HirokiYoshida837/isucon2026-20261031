---
name: makefile-setup
description: 競技が始まったら、確認済みのサーバー構成・デプロイ情報から再起動・ビルド・デプロイ用の Makefile を競技リポジトリに作る。「Makefile作って」「デプロイ用のMakefile」「再起動用のmakefile作って」などで使う。
---

# Makefile 作成（ISUCON 用）

前年度の Makefile（`reference-isucon14.mk`）を土台に、今年の構成に合わせて競技リポジトリに Makefile を作る。
デプロイは `bench` の `deploy.sh` が前提にする形（各サーバーの `$ISU_REMOTE_DIR` 直下で `make deploy` を実行）に合わせる。

## 前提
- 競技リポジトリの CLAUDE.md（SKILL `harness-setup` が置いたもの）の「環境」節が埋まっていること
  - 埋まっていなければ、このスキルより先に埋めてもらう（推測で埋めない）
- Makefile を置く場所は競技リポジトリのルート（各サーバーの `$ISU_REMOTE_DIR`。既定 `/home/isucon/webapp`）
- テンプレート: `.claude/skills/makefile-setup/Makefile.template`（`<<...>>` を埋めて使う）
- 参考: `.claude/skills/makefile-setup/reference-isucon14.mk`（前年度そのまま。手を加えない）

## 集める情報（足りなければ必ず人に聞く。推測で埋めない）
| 項目 | どこで確認するか |
|---|---|
| アプリ名・Go ソースのパス・ビルドコマンド | 競技リポジトリの CLAUDE.md「言語」欄、`docs/app-spec.md` |
| アプリの systemd unit 名 | CLAUDE.md「環境と手順」表の「アプリの systemd unit」 |
| nginx / mysql の設定ファイルの場所・反映方法 | CLAUDE.md「環境と手順」表、`.claude/skills/infra-tuning/infra/README.md` |
| 追加のサービス（マッチング・決済モックなど） | `docs/app-spec.md`、マニュアル原文（`docs/sources/`）。[過去回の構成](https://github.com/suecideTech/isucon2026-prep/blob/main/docs/past-isucon.md) は過去の命名パターンの参考にのみ使い、今年の実在確認はマニュアルで行う |
| デプロイで使うブランチ/PR の取得方法 | `Makefile.template` の `deploy`（REF・`gh pr checkout` / `gh repo sync`）をそのまま使う |

## 作る手順
1. 競技リポジトリの CLAUDE.md と上の表を見て、`<<...>>` を埋める値を揃える。足りなければ質問する
2. `Makefile.template` をコピーして `<<...>>` を埋め、競技リポジトリのルートに `Makefile` として書く
3. レシピの行がタブインデントになっているか確認する（スペースだと `*** missing separator` になる）
4. `make -n deploy` で文法エラーがないか確認する。可能なら 1 台で `make <app>.build` を試す
5. commit して push する。実際のデプロイの確認は SKILL `bench-setup`（作った `bench` の `deploy.sh` で 1 回通す）に任せる

## 注意
- `deploy` ターゲットの名前は変えない（`ISU_DEPLOY_CMD` の既定が `make deploy` のため）
- `deploy` の中身（REF の扱い・`gh pr checkout` / `gh repo sync`）は `Makefile.template` の形のままにする（`deploy.sh` が `REF=<branch|pr番号> make deploy` で呼び、ブランチ指定ならサーバーの HEAD がリモートの先頭と一致するかを確かめるため）
- サーバーごとに役割が違う（nginx 専用・DB 専用など）場合でも、Makefile は 1 つで共通にしてよい。存在しない systemd unit を restart してもエラーで終わるだけで実害は無い
- 当日の構成が前年と違う場合（言語が Go 以外、サーバー台数が違う等）は、`<<app>>.build` や `all.restart` の依存先をそれに合わせて変える
- 変更前に `docs/regulation.md` の禁止事項（変更してはいけないサービス・ファイル）を確認し、それらを Makefile のターゲットに含めない
