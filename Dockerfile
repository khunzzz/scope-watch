FROM python:3.12-slim

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1

WORKDIR /app
COPY pyproject.toml README.md ./
COPY scopewatch ./scopewatch
RUN pip install --no-cache-dir .

# работаем не под root; /data — том с базой scope-watch.db (см. docker-compose.yml)
RUN useradd --uid 1000 --no-create-home app \
    && mkdir /data && chown app:app /data
USER app

ENV SCOPEWATCH_DATA=/data \
    SCOPEWATCH_HEARTBEAT=/tmp/heartbeat

# живой = прогон завершался не позже чем 2 интервала назад
HEALTHCHECK --interval=5m --timeout=10s --start-period=15m \
    CMD python -c "import os,sys,time; i=int(os.environ.get('SCOPEWATCH_INTERVAL',900)); sys.exit(time.time()-os.path.getmtime('/tmp/heartbeat') > 2*i+300)"

CMD ["scope-watch", "loop"]
