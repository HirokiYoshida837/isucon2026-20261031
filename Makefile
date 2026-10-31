# Makefile — ISUReal 競技サーバー操作
# isucon2026-prep の .claude/skills/makefile-setup から生成
# レシピの行は必ずタブインデント（スペースだと `*** missing separator` になる）
# 全台で共通に使い、そのサーバーで enabled のユニットだけを build・再起動する
# （asia1 / europe1 / america1 は nginx+app、asia2 は PostgreSQL。使わないサービスを起動しないため）

restart_if_enabled = if systemctl is-enabled --quiet $(1); then sudo systemctl restart $(1); fi

.PHONY: all.restart
all.restart: postgresql.restart nginx.restart isureal.restart


# nginx

.PHONY: nginx.restart
nginx.restart:
	$(call restart_if_enabled,nginx)

.PHONY: nginx.logs
nginx.logs:
	journalctl -f -u nginx


# postgresql

.PHONY: postgresql.restart
postgresql.restart:
	$(call restart_if_enabled,postgresql)

.PHONY: postgresql.logs
postgresql.logs:
	journalctl -f -u postgresql@16-main


# isureal（Go ソース: go/）

.PHONY: isureal.build
isureal.build:
	if systemctl is-enabled --quiet isureal-go; then $(MAKE) -C go isureal; fi

# 起動直後に落ちたらデプロイを失敗にする（restart は起動直後のクラッシュでも成功を返すため）
.PHONY: isureal.restart
isureal.restart:
	$(call restart_if_enabled,isureal-go)
	if systemctl is-enabled --quiet isureal-go; then sleep 2; systemctl is-active --quiet isureal-go; fi

.PHONY: isureal.logs
isureal.logs:
	journalctl -f -u isureal-go

# デプロイ: REF が数字なら PR 番号として gh pr checkout、それ以外はブランチ名として gh repo sync
# bench/deploy.sh が `REF=<branch|pr番号> make deploy` の形で各サーバーで呼ぶ（既定 main）
# ターゲット名は deploy のままにする（ISU_DEPLOY_CMD の既定が `make deploy` のため）
REF ?= main

.PHONY: deploy
deploy:
	if echo '$(REF)' | grep -qE '^[0-9]+$$'; then \
		gh pr checkout $(REF) --force; \
	else \
		gh repo sync --branch $(REF) --force; \
		git switch $(REF); \
	fi
	$(MAKE) isureal.build
	$(MAKE) isureal.restart
