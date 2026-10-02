# ---- build stage：固定在建置機原生架構上，用 Go 交叉編譯出目標架構 ----
FROM --platform=$BUILDPLATFORM golang:1.23-alpine AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY . .
# go.sum 尚未 commit 時由 go mod tidy 產生；建議之後本機執行 go mod tidy 並 commit go.sum
RUN go mod tidy && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/homeward ./cmd/server

# ---- runtime stage（alpine 內建 busybox wget，供 healthcheck 使用）----
FROM alpine:3.20
COPY --from=build /out/homeward /usr/local/bin/homeward
EXPOSE 8080
ENTRYPOINT ["homeward"]
