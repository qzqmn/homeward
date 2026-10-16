#!/usr/bin/env sh
# VPS 上執行：拉取最新映像並重啟有更新的服務
set -eu
cd "$(dirname "$0")"

# .env 沒有的話 compose 會用預設值（包括空白密碼）靜默啟動——擋下來比較安全
[ -f .env ] || { echo "錯誤：找不到 .env（請先 cp .env.example .env 並填好設定）" >&2; exit 1; }

docker compose pull backend-go frontend-web db-migrate
docker compose up -d --remove-orphans
# 只清本專案（compose project）產生的 dangling 映像，不要動到共用 VPS 上其他專案的映像
docker image prune -f --filter "label=com.docker.compose.project=$(basename "$(pwd)")"
docker compose ps
