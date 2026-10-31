# 01. マニュアル → docs/ と CLAUDE.md を作る

## いつ・誰が
- 競技開始直後、マニュアルが公開されたらすぐ（すー）。所要 15〜25 分
- もう 1 人は初手ベンチとリポジトリ作成を進める
- 前提: `harness-setup/install.sh` で CLAUDE.md が置いてある

## 貼るプロンプト
````text
ISUCON2026 が始まった。マニュアルを読んで docs/ と CLAUDE.md を仕上げてほしい。
目的は、以後のセッションがルールを外さずに改善できる土台を作ること。

入力
- 当日マニュアル: <<URL か貼り付け>>
- アプリケーションマニュアル: <<URL か貼り付け>>
- レギュレーション: <<URL か貼り付け>>
- webapp のコード: <<パス。無ければ「なし」>>

作るもの（日本語、1 行 1 文、各項目に出典「資料名「節」」を付ける。推測は【推測】を付けて分ける）
1. docs/sources/ に原文の写し（manual.md / app-manual.md / regulation.md）
2. docs/regulation.md: 禁止事項・失格条件・変更不可のもの・外部リソースの可否・初期化の要件・終了時に満たす状態・主催者に質問したい点
3. docs/scoring.md: 計算式・ベンチの流れと時間・FAIL 条件・タイムアウト・エンドポイント別の猶予時間・最終スコアの定義
4. docs/restart-test.md: 追試の手順と、確認項目ごとの自チームのコマンド
5. docs/endpoints.md と docs/schema.md（コードがあれば）: メソッド / パス / ハンドラ / 読むテーブル / 書くテーブルの表。テーブルごとの index
6. CLAUDE.md: 既存の <<...>> を埋め、「この問題の注意」に固有の注意点を 3〜5 行足す
7. docs/backlog.md と docs/bench-log.md: 空の表だけ（列は 02-E と 03 のプロンプトにある）

制約
- CLAUDE.md の「守ること」を先に読む。作業は docs/ と CLAUDE.md だけ
- サーバーへの ssh は読み取りのコマンドだけ
- 原文を「禁止 / 失格 / 必ず / 以内 / 秒 / 猶予 / 初期化 / 再起動」で grep し、反映漏れがないか確認する
- commit は 1 回。push は私の確認後

最後の報告: 作ったファイル、regulation と scoring の要点 5 行ずつ、残った <<...>>、主催者への質問
````

## 終わったら
- 人が docs/regulation.md と docs/scoring.md を原文と見比べる
- commit → push。もう 1 人は pull して Claude Code を再起動する
