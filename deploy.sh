#!/usr/bin/env sh
# VPS 上執行：拉取最新映像並重啟有更新的服務
set -eu
cd "$(dirname "$0")"
docker compose pull backend-go frontend-web db-migrate
docker compose up -d --remove-orphans
docker image prune -f
docker compose ps
