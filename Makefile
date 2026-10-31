# Makefile — ISUReal 競技サーバー操作
# レシピの行は必ずタブインデント（スペースだと `*** missing separator` になる）
# サーバーごとに役割が違う（asia1/europe1/america1 = nginx+app、asia2 = PostgreSQL）が Makefile は共通。
# restart は「そのサーバーで enabled のサービスだけ」を再起動する（asia2 でアプリ、app サーバーで PostgreSQL を起動しないため）

GO_PATH := /home/isucon/local/go/bin
APP_UNIT := isureal-go.service
PG_UNIT := postgresql.service
SIGN_UNIT := isureal-sign.service

.PHONY: all.restart
all.restart: postgres.restart nginx.restart isureal.restart sign.restart


# nginx

.PHONY: nginx.restart
nginx.restart:
	@if systemctl is-enabled --quiet nginx.service; then sudo systemctl restart nginx.service; else echo "skip: nginx は enabled ではない"; fi

.PHONY: nginx.reload
nginx.reload:
	@if systemctl is-enabled --quiet nginx.service; then sudo nginx -t && sudo systemctl reload nginx.service; else echo "skip: nginx は enabled ではない"; fi

.PHONY: nginx.logs
nginx.logs:
	journalctl -f -u nginx.service


# postgres（asia2 のみ enabled）

.PHONY: postgres.restart
postgres.restart:
	@if systemctl is-enabled --quiet $(PG_UNIT); then sudo systemctl restart $(PG_UNIT); else echo "skip: postgresql は enabled ではない"; fi

.PHONY: postgres.logs
postgres.logs:
	journalctl -f -u 'postgresql*'


# isureal（Go ソース: go/）

.PHONY: isureal.build
isureal.build:
	cd go && env PATH="$$PATH:$(GO_PATH)" make isureal

.PHONY: isureal.restart
isureal.restart:
	@if systemctl is-enabled --quiet $(APP_UNIT); then sudo systemctl restart $(APP_UNIT); else echo "skip: $(APP_UNIT) は enabled ではない"; fi

# 署名専用サービス（asia2 だけで enabled。他のサーバーでは skip される）
.PHONY: sign.restart
sign.restart:
	@if systemctl is-enabled --quiet $(SIGN_UNIT) 2>/dev/null; then sudo systemctl restart $(SIGN_UNIT); else echo "skip: $(SIGN_UNIT) は enabled ではない"; fi

# asia2 で 1 回だけ実行する（ユニットを置いて enable する）
.PHONY: sign.install
sign.install:
	sudo install -m 644 infra/$(SIGN_UNIT) /etc/systemd/system/$(SIGN_UNIT)
	sudo systemctl daemon-reload
	sudo systemctl enable --now $(SIGN_UNIT)

.PHONY: isureal.logs
isureal.logs:
	journalctl -f -u $(APP_UNIT)


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
	$(MAKE) sign.restart
