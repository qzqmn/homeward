#!/bin/sh
# 歸途 Homeward — 資料庫備份迴圈（在 postgis 映像內執行，有 pg_dump 可用）。
# 由 docker-compose 的 db-backup 服務呼叫：每次 pg_dump 全量壓縮到 /backups，
# 只保留最新 N 份（BACKUP_KEEP，預設14 ≈ 兩週）。掛載目標建議直接落在
# NAS 子目錄（例如 /mnt/nas/homeward-backups），NAS 就有第二份資料。
set -e

INTERVAL="${BACKUP_INTERVAL_SEC:-86400}"   # 預設一天一次
KEEP="${BACKUP_KEEP:-14}"
DIR="${BACKUP_DIR:-/backups}"
PGHOST="${POSTGRES_HOST:-postgis-db}"
PGUSER="${POSTGRES_USER:-homeward}"
PGDB="${POSTGRES_DB:-homeward}"
export PGPASSWORD="${POSTGRES_PASSWORD:?POSTGRES_PASSWORD 必填}"

mkdir -p "$DIR"

while :; do
  STAMP="$(date +%Y%m%d-%H%M%S)"
  TMPFILE="$DIR/.inprogress.sql.gz"
  FINAL="$DIR/homeward-$STAMP.sql.gz"
  echo "[backup] start $STAMP -> $FINAL"
  # 先寫暫存檔，成功再改名：斷電/沖寫中斷不會留下「看起來完好」的殘檔
  if pg_dump -h "$PGHOST" -U "$PGUSER" -d "$PGDB" | gzip > "$TMPFILE"; then
    mv "$TMPFILE" "$FINAL"
    echo "[backup] done: $(du -h "$FINAL" | cut -f1)"
  else
    echo "[backup] ERROR: pg_dump failed，暫存檔已移除" >&2
    rm -f "$TMPFILE"
  fi
  # 只留最新 KEEP 份，舊的自動清掉
  ls -1t "$DIR"/homeward-*.sql.gz 2>/dev/null | tail -n +$((KEEP + 1)) | while IFS= read -r old; do
    rm -f "$old"
    echo "[backup] pruned: $(basename "$old")"
  done
  sleep "$INTERVAL"
done
