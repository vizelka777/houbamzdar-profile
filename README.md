# Houbam Zdar MVP

Backend (BFF) и Frontend статика для сайта houbamzdar.cz.

## Локальный запуск

1. Скопируйте `.env.example` в `.env` и заполните значениями. Убедитесь, что `SESSION_COOKIE_SECURE=false` для работы на localhost по http:
   ```bash
   cp .env.example .env
   ```

2. Запустите Go сервер:
   ```bash
   go run main.go
   ```
   Сервер запустится на порту `8080`.

3. Для раздачи статических файлов используйте любой HTTP-сервер в папке `web-static`, например:
   ```bash
   python -m http.server 8000 -d web-static/
   ```
   *Замечание: Если вы запускаете локально, вам нужно будет изменить адреса в конфигурации OIDC-клиента на `ahoj420.eu` или использовать локальный OIDC.*

## Деплой на Bunny

1. **API (BFF)**: Разверните Docker контейнер в Bunny Magic Container (порт `8080`).
   - Настройте все переменные окружения, указанные в `.env.example`.
   - Привяжите домен `api.houbamzdar.cz` к вашему Magic Container.

2. **Статика (Frontend)**: 
   - Создайте Bunny Storage Zone.
   - Загрузите все файлы из папки `web-static` (включая `index.html`, `me.html`, `app.js`, `styles.css`) в корень вашего Storage.
   - Подключите Bunny CDN Pull Zone к вашему Storage.
   - Привяжите домен `houbamzdar.cz` к CDN Pull Zone.
   - Не забудьте в настройках Bunny Edge Rule / CDN включить CORS-заголовки, если это потребуется (в данном случае мы используем API на поддомене, CORS разруливается со стороны Go backend).

3. **База данных**:
   - Создайте Bunny Database (libSQL).
   - Скопируйте URL (`DB_URL`) и токен доступа (`DB_TOKEN`) в настройки окружения Magic Container.

4. **Хранилище фото**:
   - Можно использовать одну и ту же Bunny Storage Zone для загруженных и опубликованных фото.
   - Если используете single-zone режим, задайте одинаковые значения для `BUNNY_PRIVATE_*` и `BUNNY_PUBLIC_*`.
   - `foto.houbamzdar.cz` остаётся доменом, по которому приложение строит ссылки на опубликованные фото.
   - Заполните в Magic Container переменные:
     - `BUNNY_PRIVATE_STORAGE_ZONE`
     - `BUNNY_PRIVATE_STORAGE_KEY`
     - `BUNNY_PUBLIC_STORAGE_ZONE`
     - `BUNNY_PUBLIC_STORAGE_KEY`
     - `BUNNY_PUBLIC_BASE_URL`
   - Загрузка на сервер означает, что файл уже считается shared. Публикация только добавляет ссылки из галереи и публикаций.
   - Точные координаты в файле не хранятся. Координаты и точность лежат только в БД.

### Секреты и manifests

- Файлы `app*.json` в репозитории должны оставаться шаблонами без реальных секретов.
- Реальные значения `OIDC_CLIENT_SECRET` и `DB_TOKEN` нужно задавать только в Bunny, а не коммитить в git.
- Для Bunny Database токен берётся из `Database -> Access -> Generate Tokens -> Add Secrets to Magic Container App`.
- Не коммитьте экспорт live-конфигурации из Bunny без предварительной санации секретов.

## Структура БД
Схема базы данных автоматически создается при старте бэкенда (`internal/db/db.go`).
Таблицы:
- `users`: Хранение локального ника, флагов верификации и аватара без сохранения e-mail/телефона. Колонки `access_token`, `refresh_token` и `token_expires_at` оставлены в схеме только для обратной совместимости и быстрого возврата старого поведения, но новые значения в них не записываются, а уже сохранённые значения очищаются migration-ом и при login sync приводятся к `NULL`.
- `sessions`: Управление HTTPOnly куками для BFF-сессий.
- `oidc_login_state`: Временное хранение state, nonce, PKCE challenge для OIDC.
- `photo_captures`: Загруженные снимки пользователя, их метаданные, статус публикации и ключи в Bunny storage.

## Поток фото

1. Пользователь снимает фото в `capture.html`, снимок временно сохраняется в `IndexedDB`.
2. При отправке на сервер backend нормализует изображение и повторно кодирует его, чтобы не тащить EXIF-метаданные как есть.
3. Оригинал сохраняется в Bunny Storage.
4. Координаты, точность и статус (`private` / `published`) сохраняются в `photo_captures`.
5. В single-zone конфигурации публикация просто начинает отдавать ссылку на тот же объект через `foto.houbamzdar.cz`; в legacy two-zone конфигурации backend по-прежнему может копировать файл в public zone.
6. При снятии с публикации ссылки из продукта исчезают, но сам загруженный файл может оставаться в storage до явного удаления.

## Синхронизация данных
Upsert логика находится в `internal/db/db.go` метод `UpsertUser`. 
Claims синхронизируются при логине, кроме поля `about_me`, которое управляется пользователем на сайте `houbamzdar.cz` и сохраняется в БД.

## Дополнительная документация

- `docs/bunny-deploy.md` — рабочая памятка по деплою на Bunny и проверкам после выката.
- `docs/likes.md` — рабочая документация по лайкам публикаций.
- `docs/maps.md` — рабочая документация по картам и GPS.
