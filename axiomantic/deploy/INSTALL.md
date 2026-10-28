# Установка нового сайта рядом со старым

Сервер: Ubuntu 20.04, nginx 1.18, Python 3.8. На нём уже работают старый
сайт axiomantic.ru (PHP, `/var/www/axiomantic`), CRM на lk.axiomantic.ru
(`/opt/axiomantic`, порт 8020), генератор и сайты клиентов. Новый сайт
ставится под своими именами и ничего из этого не трогает:

| что | где |
|---|---|
| программа панели | `/usr/local/bin/axiomantic-leads` |
| служба | `axm-site`, пользователь `axmsite`, `127.0.0.1:8090` |
| исходники сайта | `/opt/axm-site` (пишет только root) |
| опубликованные выпуски | `/var/www/axm-site/current` |
| заявки, пользователи панели, черновик | `/var/lib/axm-site` (`700`) |
| токен Telegram (по желанию) | `/etc/axm-site/leads.env` (`600`, root) |
| nginx | `snippets/axm-site.conf`, `sites-available/axm-site*.conf` |

## Новый пустой сервер (Timeweb и т. п.)

Ubuntu 22.04 / 24.04 или Debian 12, ничего больше не установлено.

1. В DNS у регистратора поменять A-записи `axiomantic.ru` и `www.axiomantic.ru`
   на адрес нового сервера и подождать, пока они обновятся (обычно до часа).
2. Залить архив в `/root` и выполнить:

```bash
cd /root && tar xzf axm-site-bundle.tar.gz && cd axm-site-bundle
./axm-site.sh fresh
```

Скрипт сам поставит nginx, Python и certbot, спросит логин владельца
(пароль покажет один раз — не пересылайте его в чатах), получит сертификат и
включит сайт. Затем `https://axiomantic.ru/admin/` → «Опубликовать».

## 1. Архив

На своём компьютере (или здесь в репозитории): `axiomantic/deploy/bundle.sh`
→ `axm-site-bundle.tar.gz`. Go на сервере не нужен — программа уже собрана.
Скопировать на сервер и распаковать:

```bash
scp axm-site-bundle.tar.gz root@77.221.142.156:/root/
ssh root@77.221.142.156
cd /root && tar xzf axm-site-bundle.tar.gz && cd axm-site-bundle
```

## 2. Тестовый адрес new.axiomantic.ru

DNS уже ведёт `*.axiomantic.ru` на этот сервер, нужен только сертификат:

```bash
certbot certonly --nginx -d new.axiomantic.ru
./axm-site.sh install
```

Скрипт спросит логин владельца панели и один раз покажет пароль. Его
вводят только в панели — не в чатах и не в письмах. Затем:

1. `https://new.axiomantic.ru/admin/` → войти → «Опубликовать».
2. Проверить сайт на `https://new.axiomantic.ru/`: страницы, форму
   (заявка появится в панели), блог (комментарий ждёт одобрения в
   «Комментариях»).
3. Telegram (по желанию): `sudoedit /etc/axm-site/leads.env`,
   строки `AXM_TG_TOKEN=…` и `AXM_TG_CHAT=…`, затем `systemctl restart axm-site`.

Поисковики тестовый адрес не индексируют (`X-Robots-Tag: noindex`).

## 3. Переключение axiomantic.ru

```bash
./axm-site.sh switch
```

Скрипт находит старый server block axiomantic.ru, показывает, какие пути он
передаёт другим программам, и спрашивает подтверждение. Старый файл
переносится в `/root/axm-site-backup/<дата>/`, не удаляется. Если nginx не
принимает новую конфигурацию, всё возвращается само.

Если в файле старого сайта есть и другие адреса (например, lk. или
поддомены клиентов), скрипт останавливается: такой файл переключают
вручную — убирают из него только блоки `axiomantic.ru www.axiomantic.ru`,
затем `ln -s /etc/nginx/sites-available/axm-site.conf /etc/nginx/sites-enabled/`
и `nginx -t && systemctl reload nginx`.

## 4. Откат

```bash
./axm-site.sh rollback
```

Старый сайт снова на axiomantic.ru; новый остаётся установленным, панель
доступна на new.axiomantic.ru.

## 5. Потом

- `./axm-site.sh status` — что работает и какой файл обслуживает домен.
- Обновление: новый архив, `./axm-site.sh install` (данные и пользователи
  сохраняются), в панели — «Опубликовать».
- Когда новый сайт прижился: `rm /etc/nginx/sites-enabled/axm-site-test.conf`
  и `systemctl reload nginx`. Адрес new.axiomantic.ru в списке разрешённых
  источников заявок после этого ничего не открывает: этот адрес больше
  не обслуживает ни один сайт.
- Старый PHP-сайт и его данные лежат в `/var/www/axiomantic` нетронутыми.
