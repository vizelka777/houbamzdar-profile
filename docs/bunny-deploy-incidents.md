# Bunny Deploy Incidents

Журнал реальных проблем, которые уже случались при live-деплое `houbamzdar-mvp` на Bunny, и конкретных способов восстановления.

## Когда читать

Читать до любых действий с:

- Bunny Magic Containers
- Bunny Pull Zones
- Bunny Edge Scripts
- live custom hostname `api.houbamzdar.cz`

Особенно важно читать этот файл перед recovery после неудачного `PATCH` в Bunny API.

## Инцидент 2026-03-16: dynamic Gemini model selector + live recovery

### Что делалось

- выкатывался backend `houbamzdar/bff:v27`
- выкатывался обновленный Bunny validator script с `/models`
- выкатывались `gallery.html`, `gallery.js`, `styles.css`

### Проблема 1: неправильные Bunny API path

Симптом:

- запросы на старые/угаданные path вроде `https://api.bunny.net/magic-container/...` и `https://api.bunny.net/edgescript/...` возвращали HTML `404 Page Not Found`

Причина:

- использовались неверные endpoint paths

Как решено:

- для Magic Containers использовать `https://api.bunny.net/mc/apps/{appId}`
- для Edge Script использовать `https://api.bunny.net/compute/script/{scriptId}`

Практический вывод:

- если Bunny API вернул HTML вместо JSON, сначала проверить path, а не payload

### Проблема 2: `GET /mc/apps/{id}` нельзя слать обратно как `PATCH`

Симптом:

- попытка собрать `PATCH` на основе live `GET` payload дала validation errors
- Bunny ругался на поля endpoint-конфигурации:
  - `One of configurations (CDN/Anycast/InternalIP) must be specified`
  - позже `Endpoint must contain at least one port mapping`

Причина:

- read schema и write schema у Magic Containers отличаются
- структура `endpoints` из `GET` не эквивалентна телу для `PATCH`

Как решено:

- не использовать `GET` payload как будто это готовый `PATCH`
- для write schema указывать CDN endpoint в виде:
  - `endpoints[].cdn.pullZoneId`
  - `endpoints[].cdn.isSslEnabled`
  - `endpoints[].cdn.portMappings`

Практический вывод:

- при работе с `mc/apps` нельзя предполагать симметрию `GET` и `PATCH`

### Проблема 3: частичный `PATCH` по `containerTemplates` снёс endpoint-конфиг

Симптом:

- после узкого `PATCH` в app template:
  - у template пропали старые endpoint bindings
  - `displayEndpoint` сменился на новый системный hostname
  - старый `api.houbamzdar.cz` перестал работать
- `https://api.houbamzdar.cz/health` начал отдавать Bunny `403 Domain suspended or not configured`

Причина:

- Bunny принял частичное обновление template, но пересобрал endpoint routing
- nested endpoint config оказался не сохранён в прежнем виде

Как решено:

- сначала проверили фактический live-state через системный Bunny hostname
- убедились, что новый системный endpoint живой
- затем повторно привязали custom hostname к новому pull zone

Команды/действия, которые помогли:

- проверить custom host:
  - `curl -sk https://api.houbamzdar.cz/health`
- проверить системный host:
  - `curl -sk https://<current-mc-host>.b-cdn.net/health`
- reattach hostname:
  - `POST https://api.bunny.net/pullzone/{pullZoneId}/addHostname`
  - body: `{"Hostname":"api.houbamzdar.cz"}`

Практический вывод:

- после любого `PATCH` в `mc/apps` обязательно сразу проверить:
  - `api.houbamzdar.cz`
  - текущий системный `mc-*.b-cdn.net`
  - актуальный `pullZoneId`

### Проблема 4: Bunny создал новый system hostname и новый pull zone

Симптом:

- после recovery Magic Container начал использовать уже другой системный hostname
- `displayEndpoint` и `pullZoneId` изменились во время операции

Причина:

- Bunny мог перевыпустить CDN endpoint для app

Как решено:

- не полагаться на старые значения из памяти
- после каждого существенного апдейта перечитывать:
  - `GET /mc/apps/{appId}`
- использовать свежие значения:
  - `displayEndpoint.address`
  - `containerTemplates[0].endpoints[0].pullZoneId`

Практический вывод:

- `pullZoneId` и системный `mc-*.b-cdn.net` считать динамическими

### Проблема 5: custom hostname был восстановлен только через pull zone API

Симптом:

- после исправления app template системный endpoint уже работал
- custom host `api.houbamzdar.cz` всё ещё не обслуживался

Причина:

- custom hostname не был прикреплён к новому pull zone автоматически

Как решено:

- использован Core API:
  - `POST /pullzone/{pullZoneId}/addHostname`
- после этого и HTTP, и HTTPS на `api.houbamzdar.cz` снова начали отдавать `200 OK`

Практический вывод:

- если системный host живой, а custom host мёртв, проблема обычно уже не в контейнере, а в pull zone hostname binding

### Проблема 6: состояние надо проверять не только по `/health`

Симптом:

- `/health` мог выглядеть живым, но нужно было убедиться, что backend реально отрабатывает API

Как решено:

- после recovery дополнительно проверялось:
  - `GET /api/session`
- это подтвердило, что приложение обслуживает реальные JSON endpoint, а не только health handler

Практический вывод:

- после recovery минимум проверять:
  - `/health`
  - `/api/session`

### Проблема 7: Edge Script deploy оказался проще, чем Magic Containers

Что сработало без сюрпризов:

- получить active release:
  - `GET /compute/script/{id}/releases/active`
- загрузить новый код:
  - `POST /compute/script/{id}/code`
- опубликовать release:
  - `POST /compute/script/{id}/publish`

Практический вывод:

- для Edge Script не нужно изобретать ручной release payload
- достаточно `code` + `publish`

### Проблема 8: live-статика обновилась только после upload + purge

Что делалось:

- `gallery.html`
- `gallery.js`
- `styles.css`

Как решено:

- файлы загружены напрямую в storage zone `houbamzdar`
- затем выполнен:
  - `POST /pullzone/3647035/purgeCache`

Практический вывод:

- после любого фронтенд-деплоя на `houbamzdar.cz` purge обязателен, иначе модератор может видеть старый JS/UI из CDN cache

## Надёжный порядок действий для следующих деплоев

1. Перед deploy прочитать:
   - [`docs/bunny-deploy.md`](/home/houbamydar/houbamzdar-mvp/docs/bunny-deploy.md)
   - [`docs/bunny-deploy-incidents.md`](/home/houbamydar/houbamzdar-mvp/docs/bunny-deploy-incidents.md)
2. Перед `PATCH` в `mc/apps` зафиксировать текущие значения:
   - `appId`
   - `templateId`
   - `displayEndpoint.address`
   - `pullZoneId`
3. После любого `PATCH` немедленно проверить:
   - `https://api.houbamzdar.cz/health`
   - `https://api.houbamzdar.cz/api/session`
   - `https://<current-system-host>.b-cdn.net/health`
4. Если custom host сломался, а system host жив:
   - определить актуальный `pullZoneId`
   - вызвать `POST /pullzone/{pullZoneId}/addHostname`
5. После фронтенд-апдейта:
   - upload изменённых файлов
   - purge website pull zone cache
6. После Edge Script update:
   - проверить `/health`
   - проверить `/models` или нужный рабочий endpoint

## Полезные live идентификаторы на момент 2026-03-16

- Magic Container app id: `pdZdipupoFrUKG4`
- main site pull zone: `3647035`
- AI validator script id: `68215`
- website storage zone name: `houbamzdar`

Важно:

- эти значения нужно перепроверять перед recovery, потому что системные CDN endpoint и pull zone у Magic Containers могут измениться.
