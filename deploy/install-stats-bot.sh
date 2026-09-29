#!/bin/sh
# Updates besy-provision and switches on its Telegram statistics bot.
#
# Run as root from the folder holding the new binary:
#
#   sh install-stats-bot.sh               first time: asks for the bot token
#   sh install-stats-bot.sh --chat 12345  allow a chat (the id the bot tells you)
#
# The token goes into /etc/besy/telegram-token (root only) and is never
# printed. The service's command line is not touched: the bot is
# configured by a systemd drop-in. If the updated service does not come
# back up, the previous binary and settings are put back.
set -eu

here=$(cd "$(dirname "$0")" && pwd)
bin=/usr/local/bin/besy-provision
dropdir=/etc/systemd/system/besy-provision.service.d
drop=$dropdir/telegram.conf
tokenfile=/etc/besy/telegram-token

[ "$(id -u)" = 0 ] || { echo "run as root"; exit 1; }
systemctl cat besy-provision >/dev/null 2>&1 || { echo "the besy-provision service is not installed here"; exit 1; }
systemctl cat besy-provision | grep -q "^ExecStart=$bin" || { echo "the service does not run $bin; nothing was changed"; exit 1; }

# The chats already allowed, kept across runs.
chats=""
[ -f "$drop" ] && chats=$(sed -n 's/^Environment=BESY_TG_CHATS=//p' "$drop")

if [ "${1:-}" = "--chat" ]; then
	id="${2:-}"
	case "$id" in
		''|*[!0-9-]*) echo "usage: sh install-stats-bot.sh --chat <number>"; exit 2 ;;
	esac
	case ",$chats," in
		*",$id,"*) echo "chat $id is already allowed" ;;
		*) chats="${chats:+$chats,}$id" ;;
	esac
fi

works() {
	sleep 3
	systemctl is-active --quiet besy-provision || return 1
	code=$(curl -s -o /dev/null -w "%{http_code}" -X POST https://besyvpn.online/v1/issue || true)
	[ "$code" != "404" ] && [ "$code" != "502" ] && [ "$code" != "000" ]
}

stamp=$(date +%Y%m%d-%H%M%S)

# ---- the binary --------------------------------------------------------
if [ -f "$here/besy-provision" ] && ! cmp -s "$here/besy-provision" "$bin"; then
	arch=$(uname -m)
	[ "$arch" = "x86_64" ] || { echo "this build is for x86_64, the server is $arch; ask for a matching build"; exit 1; }
	cp "$bin" "$bin.before-$stamp"
	echo "==> previous binary kept as $bin.before-$stamp"
	install -m 755 "$here/besy-provision" "$bin"
fi

# ---- the token ---------------------------------------------------------
if [ ! -s "$tokenfile" ]; then
	echo "Paste the bot token from @BotFather (it will not be shown) and press Enter:"
	stty -echo 2>/dev/null || true
	read -r token
	stty echo 2>/dev/null || true
	echo
	case "$token" in
		*:*) ;;
		*) echo "that does not look like a bot token (123456:ABC...)"; exit 1 ;;
	esac
	mkdir -p /etc/besy
	umask 077
	printf '%s\n' "$token" > "$tokenfile"
	chmod 600 "$tokenfile"
	unset token
	echo "==> token saved to $tokenfile"
fi

# ---- the settings ------------------------------------------------------
mkdir -p "$dropdir"
[ -f "$drop" ] && cp "$drop" "$drop.before-$stamp"
cat > "$drop" <<CONF
[Service]
Environment=BESY_TG_TOKEN_FILE=$tokenfile
Environment=BESY_TG_CHATS=$chats
Environment=BESY_TG_TZ=Asia/Yekaterinburg
Environment=BESY_TG_REPORT_AT=09:00
StateDirectory=besy-provision
CONF

systemctl daemon-reload
systemctl restart besy-provision

if works; then
	echo "==> besy-provision is running with the bot"
	journalctl -u besy-provision -n 20 --no-pager | grep -i telegram || true
	if [ -z "$chats" ]; then
		echo
		echo "Now open your bot in Telegram and send /start. It will reply with your chat id."
		echo "Then run:  sh install-stats-bot.sh --chat <that id>"
	else
		echo "Allowed chats: $chats. Send /now to the bot."
	fi
	exit 0
fi

echo "!! besy-provision did not come back up; putting the previous version back"
[ -f "$bin.before-$stamp" ] && cp "$bin.before-$stamp" "$bin"
if [ -f "$drop.before-$stamp" ]; then cp "$drop.before-$stamp" "$drop"; else rm -f "$drop"; fi
systemctl daemon-reload
systemctl restart besy-provision
sleep 3
systemctl is-active --quiet besy-provision && echo "   the previous version is running again" || echo "   check: journalctl -u besy-provision -n 50"
journalctl -u besy-provision -n 30 --no-pager
exit 1
