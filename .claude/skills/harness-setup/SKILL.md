---
name: harness-setup
description: ISUCON2026 の競技リポジトリに Claude Code の土台（CLAUDE.md、PR テンプレート、SKILL 一式、秘伝のタレ）を置く。当日の流れ（時刻表）と、各段階で貼るプロンプト（01 docs 作成 → 02 洗い出し → 03 実装 → 04 提出前）もここにある。「ハーネス入れて」「競技リポジトリの初期設定」「CLAUDE.md を置いて」「当日の流れ」「03 のプロンプト」などで使う。
---

# ハーネス設置（ISUCON 用）

競技リポジトリに「振る舞いの指示」を置く。
手順は各 SKILL に、判断材料は SKILL `infra-tuning` の `infra/catalog.md` に、守ること（レギュレーション・スコア 0 回避・再起動耐性・秘密情報）は CLAUDE.md に寄せてある。

## ファイル
| パス | 中身 |
|---|---|
| `install.sh <競技リポジトリ>` | 下の templates と、prep の SKILL 全部（この SKILL 自体も含む。秘伝のタレは infra-tuning の中に入っている）を競技リポジトリにコピーする。既にある CLAUDE.md は上書きしない。当日に作る `bench`・`db-migrate`（bench-setup / db-migrate-setup が作る）は消さずに残す |
| `templates/CLAUDE.md.template` | 競技リポジトリの CLAUDE.md の雛形（守ること・進め方・環境・止まるところ）。`<<...>>` は 9 個 |
| `templates/pull_request_template.md` | `.github/` に置く PR テンプレート（変更内容 / どこに効くのか / 補足図） |
| `prompts/01〜04` | 各段階で貼るプロンプト。`<<...>>` は貼る前に埋める（分からなければ「不明」と書く） |

## 使い方
1. 競技リポジトリを手元に clone する
2. prep のルートで `.claude/skills/harness-setup/install.sh <競技リポジトリのパス>` を実行する。以後の SKILL は競技リポジトリで Claude Code を開いて使う
3. 競技リポジトリの `CLAUDE.md` の `<<...>>` を埋める（マニュアルから分かるものは prompts/01 が埋める）
4. commit → push。もう 1 人は pull して Claude Code を再起動する
5. `~/.config/isucon/isucon.env` と `newrelic.env` を当日の値にする（SKILL bench / newrelic-setup が読む）
6. Makefile ができたら、SKILL `bench-setup` で `bench` を作る
- 当日 SKILL や infra を直すときは競技リポジトリで直す。prep に戻すのは競技が終わってから（2 箇所を同時に直さないため）

## 当日の流れ（時刻は開始・終了からの相対）
| いつ | すー | よち |
|---|---|---|
| 0:00〜0:30 | install.sh → prompts/01 で docs/ と CLAUDE.md | 環境構築・初手ベンチ・GitHub 初期化（SKILL gh-setup / repo-initial-push / repo-clone-setup） |
| 0:30〜1:00 | docs 確認 → commit。タレ（SKILL infra-tuning） | Makefile（SKILL makefile-setup）→ デプロイとベンチの確認（SKILL bench-setup）・APM（SKILL newrelic-setup） |
| 1:00〜 | 02-0 → 02-B。nginx（SKILL nginx-site） | 02-A |
| 午前 | 02-E → 03 を回す | 03 を回す |
| 昼過ぎ | 02-0 → 02-C | 02-D |
| 午後 | 02-E → 03 で大物を分割して入れる | 03 で大物を分割して入れる |
| 終了 -90 分 | 04-A 再起動試験 | 作業を止めて待機 |
| 終了 -50 分 | 04-B 掃除・最終ベンチ | 最終確認リストを目で確認 |
| 終了 -15 分 | 変更しない | 変更しない |

## コツ
- 1 セッション 1 タスク。終わったら `/clear`
- デプロイとベンチは 1 本ずつ。デプロイの OK は人が出す
- 削った詳細（docs の細かい項目、各視点の長い観点リストなど）は `docs/harness-archive.md` にある
