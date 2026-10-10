# базовый образ закреплён по digest: тот же образ при каждой сборке
FROM python:3.12-slim@sha256:a6e34c598f2467ed0e9a8d349809fcd8b5c603269512df273a0bb1784edc11b1

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PIP_NO_CACHE_DIR=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1

WORKDIR /app

# зависимости — строго по хэшам из lock-файла (пересобрать: uv pip compile pyproject.toml --generate-hashes -o requirements.lock)
COPY requirements.lock .
RUN pip install --require-hashes -r requirements.lock

COPY pyproject.toml README.md ./
COPY scopewatch ./scopewatch
RUN pip install --no-deps . \
    && rm -rf /app/scopewatch /app/build

# работаем не под root; /data — том с базой scope-watch.db (см. docker-compose.yml)
RUN useradd --uid 1000 --no-create-home --shell /usr/sbin/nologin app \
    && mkdir /data && chown app:app /data
USER app

ENV SCOPEWATCH_DATA=/data \
    SCOPEWATCH_HEARTBEAT=/tmp/heartbeat \
    TG_BOT_TOKEN_FILE=/run/secrets/tg_bot_token

# живой = прогон завершался не позже чем 2 интервала назад
HEALTHCHECK --interval=5m --timeout=10s --start-period=15m \
    CMD python -c "import os,sys,time; i=int(os.environ.get('SCOPEWATCH_INTERVAL',900)); sys.exit(time.time()-os.path.getmtime('/tmp/heartbeat') > 2*i+300)"

CMD ["scope-watch", "loop"]
