#!/usr/bin/env bash
# Installs the new Axiomantic site next to the old one and switches
# axiomantic.ru over to it, with a way back. Run from the unpacked bundle
# (deploy/bundle.sh makes it), as root:
#
#   ./axm-site.sh install    sources, service, nginx snippet; new.axiomantic.ru
#                            when its certificate exists. The old site, the CRM
#                            and every other nginx file stay as they are.
#   ./axm-site.sh switch     axiomantic.ru → the new site; the old server block
#                            is moved to a backup folder, not deleted
#   ./axm-site.sh rollback   puts the old server block back
#   ./axm-site.sh status     what is installed and what serves axiomantic.ru
#   ./axm-site.sh fresh      an empty server (Ubuntu 22.04/24.04, Debian 12):
#                            packages, the site, a certificate, axiomantic.ru on
#                            the new site at once. DNS must already point here.
#
# Names are chosen not to touch what already runs on the server:
# user axmsite, /opt/axm-site, /var/www/axm-site, /var/lib/axm-site,
# /etc/axm-site, service axm-site, 127.0.0.1:8090.
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
PORT=8090
USER_NAME=axmsite
SRC=/opt/axm-site
WWW=/var/www/axm-site
DATA=/var/lib/axm-site
ETC=/etc/axm-site
BIN=/usr/local/bin/axiomantic-leads
ADMIN="$DATA/admin.json"
NGX=/etc/nginx
BACKUP_ROOT=/root/axm-site-backup
DOMAIN=axiomantic.ru
TEST_DOMAIN=new.axiomantic.ru

say() { printf '\n== %s\n' "$*"; }
die() { printf '\nОШИБКА: %s\n' "$*" >&2; exit 1; }

[ "$(id -u)" = 0 ] || die "запустите через sudo"

reload_nginx() {
	if nginx -t 2>&1; then
		systemctl reload nginx
		sleep 2 # workers with the old configuration finish their requests
	else
		return 1
	fi
}

# Files under sites-enabled and conf.d with a server block for the given
# name (and nothing but that name and its www.), symlinks resolved.
blocks_for() {
	local name="$1" f
	for f in "$NGX"/sites-enabled/* "$NGX"/conf.d/*.conf; do
		[ -e "$f" ] || continue
		if grep -Eq "server_name[^;]*[[:space:]]${name//./\\.}([[:space:]]|;)" "$f"; then echo "$f"; fi
	done
	return 0
}

cmd_install() {
	[ "$(uname -m)" = x86_64 ] || die "бинарник собран для x86_64, а здесь $(uname -m)"
	python3 -c 'import sys; sys.exit(sys.version_info < (3, 8))' || die "нужен python3 3.8 или новее"
	command -v nginx >/dev/null || die "nginx не найден"
	if ss -ltn "sport = :$PORT" | grep -q LISTEN && ! systemctl is-active --quiet axm-site; then
		die "порт $PORT уже занят другим сервисом; поменяйте PORT здесь, в axm-site.service и snippet-axm-site.conf"
	fi

	say "пользователь $USER_NAME"
	id "$USER_NAME" >/dev/null 2>&1 || useradd --system --no-create-home --shell /usr/sbin/nologin "$USER_NAME"

	say "программа $BIN"
	install -m 755 "$HERE/bin/axiomantic-leads" "$BIN"

	say "исходники сайта $SRC"
	# a fresh copy each time, so a file removed from the site goes away too
	rm -rf "$SRC.new"
	install -d -m 755 "$SRC.new"
	cp -a "$HERE/site-src/." "$SRC.new/"
	chown -R root:root "$SRC.new"
	chmod -R u=rwX,go=rX "$SRC.new"
	rm -rf "$SRC.old"
	[ -d "$SRC" ] && mv "$SRC" "$SRC.old"
	mv "$SRC.new" "$SRC"
	rm -rf "$SRC.old"

	install -d -m 755 -o "$USER_NAME" -g "$USER_NAME" "$WWW"
	install -d -m 700 -o "$USER_NAME" -g "$USER_NAME" "$DATA"
	install -d -m 750 -o root -g "$USER_NAME" "$ETC"
	[ -e "$ETC/leads.env" ] || install -m 600 -o root -g root /dev/null "$ETC/leads.env"

	if [ ! -e "$ADMIN" ]; then
		say "первая установка: рабочее место и владелец панели"
		"$BIN" admin workspace set -config "$ADMIN" -id studio -name "Axiomantic" \
			-origins "https://$DOMAIN,https://www.$DOMAIN,https://$TEST_DOMAIN" \
			-site "https://$DOMAIN" \
			-site-dir "$SRC" -site-root "$WWW" -site-data "$DATA/site"
		local login=""
		# only plain characters: a paste can bring invisible ones along
		until printf '%s' "$login" | LC_ALL=C grep -Eq '^[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}$'; do
			[ -z "$login" ] || echo "Не похоже на email (или в нём невидимые символы) — наберите вручную."
			read -r -p "Логин владельца панели (email): " login
			login="$(printf '%s' "$login" | LC_ALL=C tr -cd 'A-Za-z0-9._%+@-')"
		done
		local name=""
		read -r -p "Имя для панели [Владелец]: " name
		echo "Пароль будет показан один раз — сохраните его в менеджер паролей и не пересылайте в чатах."
		"$BIN" admin user add -config "$ADMIN" -workspace studio -login "$login" -name "${name:-Владелец}" -role owner
		chown "$USER_NAME:$USER_NAME" "$ADMIN"
		chmod 600 "$ADMIN"
	fi

	say "служба axm-site"
	install -m 644 "$HERE/deploy/axm-site.service" /etc/systemd/system/axm-site.service
	systemctl daemon-reload
	systemctl enable axm-site >/dev/null 2>&1
	systemctl restart axm-site
	sleep 2
	systemctl is-active --quiet axm-site || { journalctl -u axm-site -n 30 --no-pager; die "служба не запустилась"; }
	curl -fsS -o /dev/null "http://127.0.0.1:$PORT/admin/" || die "панель не отвечает на 127.0.0.1:$PORT"

	say "nginx"
	install -m 644 "$HERE/deploy/snippet-axm-site.conf" "$NGX/snippets/axm-site.conf"
	install -m 644 "$HERE/deploy/axm-site.conf" "$NGX/sites-available/axm-site.conf"
	install -m 644 "$HERE/deploy/axm-site-test.conf" "$NGX/sites-available/axm-site-test.conf"
	if [ -e "/etc/letsencrypt/live/$TEST_DOMAIN/fullchain.pem" ]; then
		if [ -n "$(blocks_for "$TEST_DOMAIN" | grep -v axm-site-test.conf || true)" ]; then
			echo "$TEST_DOMAIN уже описан в другом файле nginx — тестовый адрес не включаю"
		else
			ln -sf "$NGX/sites-available/axm-site-test.conf" "$NGX/sites-enabled/axm-site-test.conf"
			reload_nginx || { rm -f "$NGX/sites-enabled/axm-site-test.conf"; die "nginx не принял конфигурацию; тестовый адрес выключен, старый сайт не тронут"; }
			echo "Тестовый адрес: https://$TEST_DOMAIN/admin/ — войдите и нажмите «Опубликовать»."
		fi
	else
		echo "Сертификата для $TEST_DOMAIN нет. Получить: certbot certonly --nginx -d $TEST_DOMAIN"
		echo "затем снова: $0 install"
	fi
	say "готово; старый сайт работает как раньше"
}

cmd_switch() {
	[ -e "$WWW/current/index.html" ] || die "новый сайт ещё не опубликован: войдите в панель на https://$TEST_DOMAIN/admin/ и нажмите «Опубликовать»"
	[ -e "$NGX/sites-enabled/axm-site.conf" ] && die "axiomantic.ru уже на новом сайте"
	local old f bad=""
	old="$(blocks_for "$DOMAIN" | grep -v '/axm-site' || true)"
	[ -n "$old" ] || die "не нашёл старый server block для $DOMAIN"
	# a file that also serves other names (lk., client subdomains) is not
	# ours to move
	for f in $old; do
		if grep -Eo 'server_name[^;]*' "$f" | tr -s ' \t' '\n' | grep -v '^server_name$' | grep -Ev "^(www\.)?$DOMAIN$" | grep -q .; then
			bad="$bad $f"
		fi
	done
	[ -z "$bad" ] || die "в$bad кроме $DOMAIN есть другие адреса — переключите вручную (см. INSTALL.md)"

	# whatever the old block hands to other programs stops working on
	# axiomantic.ru after the switch: show it and ask
	echo "Старый server block: $old"
	echo "Пути, которые он передаёт другим программам (после переключения они перестанут работать на $DOMAIN):"
	grep -hoE '(location[^{]*|(proxy|fastcgi|uwsgi)_pass[^;]*)' $old | sed 's/^/  /' | sort -u || true
	local answer=""
	read -r -p "Переключить $DOMAIN на новый сайт? Напишите yes: " answer
	[ "$answer" = yes ] || die "отменено, ничего не изменено"

	local dir="$BACKUP_ROOT/$(date +%Y%m%d-%H%M%S)"
	say "старые файлы → $dir"
	install -d -m 700 "$dir"
	: >"$dir/moved"
	for f in $old; do
		cp -a "$f" "$dir/" # the symlink itself, or the file
		echo "$f" >>"$dir/moved"
		rm "$f"
	done
	ln -sf "$NGX/sites-available/axm-site.conf" "$NGX/sites-enabled/axm-site.conf"
	if ! reload_nginx; then
		rm -f "$NGX/sites-enabled/axm-site.conf"
		while read -r f; do cp -a "$dir/$(basename "$f")" "$f"; done <"$dir/moved"
		reload_nginx || true
		die "nginx не принял новую конфигурацию — всё возвращено как было"
	fi
	echo "$dir" >"$BACKUP_ROOT/last"
	say "axiomantic.ru теперь показывает новый сайт. Вернуть старый: $0 rollback"
}

cmd_rollback() {
	[ -e "$BACKUP_ROOT/last" ] || die "нет записи о переключении"
	local dir f
	dir="$(cat "$BACKUP_ROOT/last")"
	rm -f "$NGX/sites-enabled/axm-site.conf"
	while read -r f; do cp -a "$dir/$(basename "$f")" "$f"; done <"$dir/moved"
	reload_nginx || die "nginx не принял старую конфигурацию; файлы лежат в $dir"
	rm -f "$BACKUP_ROOT/last"
	say "axiomantic.ru снова показывает старый сайт; новый остаётся установленным"
}

cmd_status() {
	echo "служба axm-site: $(systemctl is-active axm-site 2>/dev/null || true)"
	echo "опубликованный выпуск: $(readlink "$WWW/current" 2>/dev/null || echo нет)"
	echo "server block для $DOMAIN:"
	blocks_for "$DOMAIN" | sed 's/^/  /'
	echo "server block для $TEST_DOMAIN:"
	blocks_for "$TEST_DOMAIN" | sed 's/^/  /'
}

cmd_fresh() {
	if [ -n "$(ls "$NGX"/sites-enabled 2>/dev/null | grep -v '^default$' || true)" ]; then
		die "на сервере уже есть сайты в nginx — для такого сервера: install, затем switch"
	fi
	say "пакеты"
	export DEBIAN_FRONTEND=noninteractive
	apt-get update -qq
	apt-get install -y -qq nginx python3 certbot python3-certbot-nginx curl iproute2 >/dev/null
	systemctl enable --now nginx >/dev/null 2>&1 || true

	# DNS: the domain must already lead here, or the certificate cannot be issued
	local here there
	here="$(hostname -I 2>/dev/null | tr ' ' '\n' | grep -E '^[0-9.]+$' | head -1)"
	there="$(getent ahostsv4 "$DOMAIN" | awk 'NR==1{print $1}')"
	echo "Адрес сервера: ${here:-?}; $DOMAIN указывает на: ${there:-ничего}"
	if [ -z "$there" ] || ! hostname -I | tr ' ' '\n' | grep -qx "$there"; then
		local go=""
		read -r -p "Похоже, DNS ещё не ведёт сюда (у облаков адрес может быть за NAT). Продолжить? Напишите yes: " go
		[ "$go" = yes ] || die "смените A-записи $DOMAIN и www.$DOMAIN на адрес этого сервера и запустите снова"
	fi

	cmd_install

	say "сертификат для $DOMAIN"
	if [ ! -e "/etc/letsencrypt/live/$DOMAIN/fullchain.pem" ]; then
		local mail=""
		read -r -p "Email для писем Let's Encrypt о сроке сертификата (можно пусто): " mail
		if [ -n "$mail" ]; then
			certbot certonly --nginx --non-interactive --agree-tos -m "$mail" -d "$DOMAIN" -d "www.$DOMAIN"
		else
			certbot certonly --nginx --non-interactive --agree-tos --register-unsafely-without-email -d "$DOMAIN" -d "www.$DOMAIN"
		fi
	fi
	rm -f "$NGX/sites-enabled/default"
	ln -sf "$NGX/sites-available/axm-site.conf" "$NGX/sites-enabled/axm-site.conf"
	reload_nginx || die "nginx не принял конфигурацию; смотрите nginx -t"
	say "готово: https://$DOMAIN/admin/ — войдите и нажмите «Опубликовать»"
}

case "${1:-}" in
install) cmd_install ;;
switch) cmd_switch ;;
rollback) cmd_rollback ;;
status) cmd_status ;;
fresh) cmd_fresh ;;
*) sed -n '2,19p' "$0"; exit 2 ;;
esac
