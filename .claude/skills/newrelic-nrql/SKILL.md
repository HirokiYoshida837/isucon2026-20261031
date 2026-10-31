---
name: newrelic-nrql
description: ISUCON2026 の計測データ（APM・インフラ）をチームの NewRelic アカウント（8667760）から NRQL で取得する。「NewRelic 見て」「ボトルネックどこ」「遅いエンドポイント」「重い SQL」「サーバーのリソース」「ベンチ中の CPU」などで使う。別組織の NewRelic MCP は使わない。
---

# NewRelic NRQL（ISUCON 用）

NewRelic MCP は Free プランのアカウントでは使えないため、NerdGraph API に NRQL を直接投げる。

## 前提
- スクリプト: `.claude/skills/newrelic-nrql/nrql.sh`（リポジトリのルートから実行）
- 設定: `~/.config/isucon/newrelic.env`（アカウント 8667760 と User API キー）。中身を表示・出力しないこと
- アプリ名で絞るときは `NEW_RELIC_APP_NAME=<appName>` を付けて実行する（env ファイルに書いてもよい。実行時の値が優先）
  - `sql` / `external` は appName 必須。内部で appId に変換して引く（タイムスライスは appName では引けない）
- 手元に別組織の NewRelic MCP（`newrelic` など）が入っていても、ISUCON では使わない

## 使い方
```bash
cd <リポジトリのルート>
S=.claude/skills/newrelic-nrql/nrql.sh
$S whoami                    # キーと接続先の確認（個人アカウントか）
$S --table endpoints 10      # エンドポイント別の合計時間（直近 10 分）
$S --table sql 10            # SQL 別の合計時間
$S --table external 10       # 外部 HTTP 呼び出し
$S --table errors 10         # エラー
$S --table hosts 10          # サーバー別 CPU / メモリ（p90 / p95 / max）
$S --table procs 10          # プロセス別 CPU / メモリ
$S "SELECT ... FROM ... SINCE 30 minutes ago"   # 任意の NRQL
```
- 第 2 引数は「直近何分」。ベンチ 1 回分を見るなら、ベンチの所要時間＋1〜2 分にする
- ベンチ期間ぴったりで見るときは、Discord のベンチ結果投稿にある `NR_SINCE_MS=... NR_UNTIL_MS=...` を前に付けて実行する
- `--table` を外すと JSON で返る

## 進め方
1. ベンチ直後に `endpoints` と `sql` を見て、合計時間が大きい順に上位 5 件を挙げる
2. 各候補について、コードの該当箇所と改善案（index、N+1 解消、キャッシュなど）を出す
3. `hosts` と `procs` で、どのサーバーのどのプロセスが詰まっているかを添える
4. 結果は「上位ボトルネック」「根拠の数値」「改善案」の順に短くまとめる
- データが空なら、エージェント未導入・アプリ名違い・時間範囲外のどれかを疑う
- `hosts` / `procs` の名前は infra agent の `display_name`（entityName）で出る
