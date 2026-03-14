# Bunny Deploy Runbook

Практическая памятка для деплоя `houbamzdar.cz` и `api.houbamzdar.cz` на Bunny без потери endpoint-конфигурации.

## Что деплоится

- Backend API: Bunny Magic Containers, образ `houbamzdar/bff:vNN`
- Статика: Bunny Storage + CDN для `houbamzdar.cz`
- Публичные фото: Bunny Storage / CDN для `foto.houbamzdar.cz`

## Перед деплоем

1. Убедиться, что ветка чистая и `go test ./...` проходит.
2. Поднять `imageTag` в [`app.json`](/home/houbamydar/houbamzdar-mvp/app.json).
3. Проверить, что новые статические файлы действительно лежат в `web-static/`.

## Backend: Magic Containers

1. Собрать и запушить образ:
   ```bash
   docker build -t houbamzdar/bff:vNN .
   docker push houbamzdar/bff:vNN
   ```
2. Обновить Bunny Magic Container на новый tag.
3. После обновления проверить, что app остается `active`.
4. Проверить health:
   ```bash
   curl -sS http://api.houbamzdar.cz/health
   curl -sS https://api.houbamzdar.cz/health
   ```

## Важная тонкость по Bunny API

- Для Magic Containers нельзя бездумно слать частичный `PATCH` с урезанным `containerTemplates`.
- Bunny может принять такой запрос и затереть вложенную endpoint-конфигурацию внутри template.
- Если обновление делается через API, сначала нужно получить полный app payload, менять только нужные поля и отправлять обратно полную корректную структуру.
- После любого API-обновления надо отдельно проверить:
  - `publicHost`
  - endpoint routing
  - custom hostname
  - TLS certificate state

## Статика: сайт

1. Загрузить измененные файлы из `web-static/` в Bunny Storage для сайта.
   Если менялись плагины или служебные ассеты фронтенда, отдельно проверить и загрузить вложенные каталоги вроде `web-static/vendor/...`.
2. Сделать CDN purge.
3. Проверить byte-match для ключевых файлов:
   - `index.html`
   - `feed.js`
   - `app.js`
   - `styles.css`
   - `map-clusters.js`
   - `vendor/leaflet-markercluster/*`
   - другие измененные страницы

## Custom hostname и TLS

После обновления backend обязательно проверить:

1. `api.houbamzdar.cz` по HTTP и HTTPS.
2. Что custom hostname все еще привязан к правильному pull zone / endpoint.
3. Что у hostname есть валидный сертификат.

Если `http://api.houbamzdar.cz/health` работает, а `https://api.houbamzdar.cz/health` нет:

- сначала проверить, не слетел ли custom hostname
- потом проверить, выпущен ли сертификат для hostname
- не считать деплой завершенным, пока HTTPS не отвечает без `-k`

## Smoke-check после деплоя

Минимум:

1. `GET /health`
2. `GET /api/session`
3. `GET /api/public/posts`
4. `GET /api/public/captures`
5. Открыть live-страницы:
   - `/feed.html`
   - `/gallery.html`
   - `/map.html`
   - `/public-profile.html?user=...`
   - `/me.html` под логином
6. Проверить:
   - комментарии создаются, редактируются и удаляются
   - переход по avatar/name ведет на public profile
   - профильная карта переключается между своими точками и `Prohlédnuté za houbičky`
   - `Serverový archiv` доступен из `Můj profil`

## Если менялась фронтенд-логика

После деплоя отдельно проверить:

- guest flow
- logged-in flow
- owner flow на своем public profile
- unlock координат за houbičky
- прямой запуск камеры из header-иконки
- страницу `Zpracování fotek` и меню `Foto`
- все карты с несколькими метками: cluster split/merge и spiderfy
- pagination / load-more на страницах, где были изменения

## Завершение

1. Зафиксировать новый image tag в git.
2. Убедиться, что live совпадает с локальными файлами для измененной статики.
3. Только после этого считать Bunny deploy завершенным.
