#!/bin/sh
# Puts the BESY VPN pages on besyvpn.online and makes nginx serve them.
#
# Run as root from the folder you uploaded:   sh install-site.sh
#
# Touches only besyvpn.online: copies the pages into /var/www/besyvpn and,
# in /etc/nginx/sites-available/besyvpn.online, replaces the catch-all
# "return 404" with serving that folder. The key service (/v1/issue) and
# every other site on the server stay as they are. The old config is kept
# next to it, and if nginx does not accept the new one it is put back.
set -eu

here=$(cd "$(dirname "$0")" && pwd)
dst=/var/www/besyvpn
conf=/etc/nginx/sites-available/besyvpn.online

[ "$(id -u)" = 0 ] || { echo "run as root"; exit 1; }
[ -f "$here/index.html" ] && [ -f "$here/privacy.html" ] || { echo "the pages are not next to this script"; exit 1; }
[ -f "$conf" ] || { echo "$conf not found"; exit 1; }

echo "==> pages into $dst"
mkdir -p "$dst/en"
for f in index.html privacy.html terms.html oferta.html; do
	[ -f "$here/$f" ] && install -m 644 "$here/$f" "$dst/$f"
done
for f in index.html privacy.html terms.html; do
	[ -f "$here/en/$f" ] && install -m 644 "$here/en/$f" "$dst/en/$f"
done

if grep -q "root $dst;" "$conf"; then
	echo "==> nginx already serves $dst"
else
	backup="$conf.before-site-$(date +%Y%m%d-%H%M%S)"
	cp "$conf" "$backup"
	echo "==> old config kept as $backup"
	# the first "location / { return 404; }" is the one in the https block
	perl -0pi -e 's{location\s*/\s*\{\s*return\s+404;\s*\}}{root '"$dst"';\n    index index.html;\n\n    location / {\n        try_files \$uri \$uri.html \$uri/ =404;\n    }}' "$conf"
	if ! grep -q "root $dst;" "$conf"; then
		cp "$backup" "$conf"
		echo "could not find the place to change in $conf; nothing was changed"
		exit 1
	fi
	if ! nginx -t; then
		cp "$backup" "$conf"
		echo "nginx did not accept the change; the old config is back, nothing else was touched"
		exit 1
	fi
	systemctl reload nginx
fi

echo "==> checking"
for p in / /privacy.html /terms.html /en/privacy.html /oferta.html; do
	code=$(curl -s -o /dev/null -w "%{http_code}" "https://besyvpn.online$p" || true)
	echo "   https://besyvpn.online$p  $code"
done
code=$(curl -s -o /dev/null -w "%{http_code}" -X POST "https://besyvpn.online/v1/issue" || true)
echo "   key service /v1/issue answers $code (anything but 404/502 means it is still there)"
