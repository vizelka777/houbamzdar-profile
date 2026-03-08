# Houba Mzdar! MVP

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

### Секреты и manifests

- Файлы `app*.json` в репозитории должны оставаться шаблонами без реальных секретов.
- Реальные значения `OIDC_CLIENT_SECRET` и `DB_TOKEN` нужно задавать только в Bunny, а не коммитить в git.
- Для Bunny Database токен берётся из `Database -> Access -> Generate Tokens -> Add Secrets to Magic Container App`.
- Не коммитьте экспорт live-конфигурации из Bunny без предварительной санации секретов.

## Структура БД
Схема базы данных автоматически создается при старте бэкенда (`internal/db/db.go`).
Таблицы:
- `users`: Хранение профилей, синхронизация claims (preferred_username, email, phone_number, picture).
- `sessions`: Управление HTTPOnly куками для BFF-сессий.
- `oidc_login_state`: Временное хранение state, nonce, PKCE challenge для OIDC.

## Синхронизация данных
Upsert логика находится в `internal/db/db.go` метод `UpsertUser`. 
Claims синхронизируются при логине, кроме поля `about_me`, которое управляется пользователем на сайте `houbamzdar.cz` и сохраняется в БД.
