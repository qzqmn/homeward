# ---- build stage：固定在建置機原生架構上，用 Go 交叉編譯出目標架構 ----
# 用 1.26（而非 go.mod 宣告的 1.25）是因為 pgx/go-redis 的測試用間接依賴
# （rogpeppe/go-internal）需要 go >= 1.25 才能解析；官方映像把 GOTOOLCHAIN
# 鎖成 local，版本不夠不會自動升級工具鏈，只能讓建置映像本身夠新。
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
# 先抽 go.mod/go.sum 做依賴層快取；go.sum 已 commit，依賴版本從此可重現
# （不再每次建置即時 go mod tidy 解析新版）
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/homeward ./cmd/server

# ---- runtime stage（alpine 內建 busybox wget，供 healthcheck 使用）----
FROM alpine:3.20
COPY --from=build /out/homeward /usr/local/bin/homeward
EXPOSE 8080
ENTRYPOINT ["homeward"]
