---
name: newrelic-setup
description: ISUCON2026 の競技サーバーに NewRelic を入れる・止める。infra agent（CPU・プロセス）の導入、アプリ（Go agent）への環境変数配布と systemd 設定、疎通確認、最終提出前の停止まで。「NewRelic 入れて」「APM 導入」「計測の準備」「NewRelic 止めて」「APM を無効に」などで使う。NRQL で見る側は SKILL newrelic-nrql。
---

# NewRelic セットアップ（ISUCON 用）

手で 5 手順やっていたものを、全台一括のスクリプトにしたもの。
練習サーバー（private-isu）で 2026-10-28 に通した手順がそのまま入っている。
経緯と実機の記録は [docs/newrelic-agent-install.md](https://github.com/suecideTech/isucon2026-prep/blob/main/docs/newrelic-agent-install.md)（prep）。

## 全体像
```
手元 ~/.config/isucon/newrelic.env ─push-env.sh──────▶ 各台 /home/isucon/newrelic.env (600)
                                  ─install-infra.sh─▶ 各台 newrelic-infra（apt / display_name=ssh の Host 名 / process ON）
人がコードに Go agent を入れる（go-agent-snippet.md）→ deploy
                                  ─app-dropin.sh────▶ 各台 <unit>.service.d/newrelic.conf → 再起動 → 接続ログ確認
                                  ─verify.sh────────▶ agent 状態 + nrql.sh の hosts / procs / endpoints / sql
最終提出前                         ─toggle.sh off────▶ NEW_RELIC_ENABLED=false + アプリ再起動 + infra agent disable
```

## スクリプト（リポジトリの `.claude/skills/newrelic-setup/`）
| スクリプト | やること | 所要 |
|---|---|---|
| `fetch-license-key.sh` | NerdGraph でライセンスキー（INGEST - LICENSE）を取り `~/.config/isucon/newrelic.env` に保存。値は表示しない | 10 秒 |
| `push-env.sh [host...]` | 各台に `/home/isucon/newrelic.env` を配置（キー・アプリ名・表示名=Host 名・ENABLED=true） | 10 秒 |
| `install-infra.sh [host...]` | infra agent を apt で導入し、`display_name` と `enable_process_metrics: true` を設定して起動。2 回目は差分だけ | 1 分/台 |
| `app-dropin.sh [unit] [host...]` | アプリの systemd に drop-in（EnvironmentFile）を足して再起動。接続ログ（`application connected`）を表示 | 10 秒/台 |
| `verify.sh [分] [host...]` | 各台の agent 状態と、NRQL 4 プリセット（hosts / procs / endpoints / sql）を表示 | 10 秒 |
| `toggle.sh on\|off [unit] [host...]` | 計測の停止・再開（Go agent は env の ENABLED、infra agent は systemd）。コードは触らない | 10 秒/台 |
| `go-agent-snippet.md` | アプリ側のコード差分パターン（ルーター別・DB 別・起動を止めない初期化） | 読む 5 分、当てる 15 分 |

- 設定: `~/.config/isucon/isucon.env` の `ISU_HOSTS`（ssh の Host 名）と `ISU_APP_UNIT`（アプリの systemd ユニット名）。`~/.config/isucon/newrelic.env` の `NEW_RELIC_APP_NAME`（全台同じ名前。既定 isucon2026）
- ホストを引数で渡すと `ISU_HOSTS` より優先する（1 台だけやり直すとき）
- キー類の中身を表示・出力・コミットしない

## 当日の手順（開始 0:30〜1:00、よち担当。合計 20 分）
1. 前提: `isucon.env` に `ISU_HOSTS` と `ISU_APP_UNIT` を書く。`newrelic.env` に `NEW_RELIC_LICENSE_KEY` があるか見る（無ければ `fetch-license-key.sh`）
2. `push-env.sh` → `install-infra.sh`（全台。アプリの作業と並行してよい）
3. `go-agent-snippet.md` を見てアプリに差分を当てる（ルーターと DB の 2 か所 + 初期化）。PR → デプロイ
4. `app-dropin.sh` → 各台の `agent:` 行が `NewRelic に接続済み` になる
5. 初手ベンチ後に `verify.sh` → 4 表にデータが出れば完了。以後は SKILL `newrelic-nrql` と `bench` の bench-report が使う

## 最終提出前（終了 -90 分、04-pre-submit-check の中で）
- `toggle.sh off` → アプリは APM 無効で再起動、infra agent は disable（OS 再起動後も止まったまま）
- ログ類は `infra/scripts/measure.sh off` で別に止める
- 戻すときは `toggle.sh on`

## ハマりどころ（実機で確認したもの）
- `sql` / `external` のタイムスライスは `appName` では引けない。`nrql.sh` が appId に変換するので `NEW_RELIC_APP_NAME` を正しく入れる
- `enable_process_metrics: true` が無いと `procs` が空になる（install-infra.sh が入れる）
- `hosts` / `procs` の名前は infra の `display_name`（= ssh の Host 名）で出る
- chi 用の公式 `nrgochi` はトランザクション名が URL パスになる。`WrapHandleFunc` でルートパターン名にする
- Go agent は `database/sql` の `*Context` 系クエリだけ計測する。Context なしのクエリは snippet の置換手順で直す
- infra agent は 1 台あたり常駐メモリ 45MB ほど。DB 専用機の buffer pool を決めるときに差し引く
- データ到着は 1〜2 分遅れる。空でもすぐに壊れたと判断しない
