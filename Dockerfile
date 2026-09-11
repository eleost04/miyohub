FROM node:22-alpine AS frontend
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
COPY internal/buildinfo/VERSION /src/internal/buildinfo/VERSION
RUN npm run build

FROM golang:1.27-alpine AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
ARG VERSION
RUN test -z "$VERSION" || test "$VERSION" = "$(tr -d '\r\n' < internal/buildinfo/VERSION)"
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/miyohub ./cmd/miyohub

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S -g 10001 miyohub \
    && adduser -S -D -H -u 10001 -G miyohub miyohub \
    && mkdir -p /data /app/web/dist \
    && chown miyohub:miyohub /data
WORKDIR /app
COPY --from=backend /out/miyohub /usr/local/bin/miyohub
COPY --from=frontend /src/web/dist/ /app/web/dist/
ENV MIYOHUB_DATA_DIR=/data MIYOHUB_HOST=0.0.0.0 MIYOHUB_PORT=5890
USER miyohub
VOLUME ["/data"]
EXPOSE 5890
HEALTHCHECK --interval=30s --timeout=6s --start-period=10s --retries=3 \
  CMD wget -q -T 5 -O /dev/null http://127.0.0.1:5890/api/v1/health || exit 1
ENTRYPOINT ["miyohub"]
CMD ["serve"]
