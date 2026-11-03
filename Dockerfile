FROM golang:1.27.3 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download && go mod verify
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/scope-watch ./cmd/scope-watch \
    && mkdir -p /out/data /out/tmp && chmod 1777 /out/tmp

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/scope-watch /scope-watch
COPY --from=build --chown=1000:1000 /out/data /data
COPY --from=build /out/tmp /tmp
USER 1000:1000
WORKDIR /data
ENV PATH=/ \
    SCOPEWATCH_DATA=/data \
    SCOPEWATCH_HEARTBEAT=/tmp/heartbeat \
    TG_BOT_TOKEN_FILE=/run/secrets/tg_bot_token
HEALTHCHECK --interval=5m --timeout=10s --start-period=15m \
    CMD ["/scope-watch", "healthcheck"]
CMD ["/scope-watch", "loop"]
