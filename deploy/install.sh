#!/usr/bin/env bash
# Установка на VPS (Debian/Ubuntu). Запускать из корня проекта под root:
#   sudo TG_BOT_TOKEN=... TG_CHAT_ID=... ./deploy/install.sh
# Повторный запуск = обновление кода (база и конфиг не трогаются).
set -euo pipefail

APP=/opt/scope-watch
DATA=/var/lib/scope-watch
ENVF=/etc/scope-watch.env
SRC="$(cd "$(dirname "$0")/.." && pwd)"

[[ $EUID -eq 0 ]] || { echo "запусти под root (sudo)"; exit 1; }

apt-get install -y -qq python3 python3-venv git rsync >/dev/null

id scopewatch &>/dev/null || useradd --system --home "$DATA" --shell /usr/sbin/nologin scopewatch

mkdir -p "$APP" "$DATA"
rsync -a --delete --exclude .venv --exclude .git --exclude snapshots --exclude .env "$SRC"/ "$APP"/
python3 -m venv "$APP/.venv"
"$APP/.venv/bin/pip" install -q --upgrade pip
"$APP/.venv/bin/pip" install -q "$APP"

if [[ ! -f $ENVF ]]; then
  : "${TG_BOT_TOKEN:?задай TG_BOT_TOKEN}"
  : "${TG_CHAT_ID:?задай TG_CHAT_ID}"
  umask 077
  printf 'TG_BOT_TOKEN=%s\nTG_CHAT_ID=%s\n' "$TG_BOT_TOKEN" "$TG_CHAT_ID" > "$ENVF"
fi
chown root:scopewatch "$ENVF" && chmod 640 "$ENVF"

if [[ ! -d $DATA/.git ]]; then
  git -C "$DATA" init -q
fi
chown -R scopewatch:scopewatch "$DATA"

install -m 644 "$SRC/deploy/scope-watch.service" /etc/systemd/system/
install -m 644 "$SRC/deploy/scope-watch.timer" /etc/systemd/system/
systemctl daemon-reload

# первый запуск: если базы нет, runner сам сделает seed и пришлёт 🌱
if [[ -z "$(ls -A "$DATA" | grep -v '^\.git$' || true)" ]]; then
  echo "первый прогон (seed), ~3 минуты..."
  systemctl start scope-watch.service
fi
systemctl enable --now scope-watch.timer
systemctl list-timers scope-watch.timer --no-pager
echo "готово. логи: journalctl -u scope-watch -f"
