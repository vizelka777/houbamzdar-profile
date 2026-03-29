# Chat Service

Отдельный `chat-service` для `Houbám Zdar` живёт в этом же репозитории, но деплоится как самостоятельный Bunny Magic Container app.

## Архитектура

- Monolith `api.houbamzdar.cz`
  - остаётся source of truth для `users`, `sessions`, OIDC login и moderation flags
  - выдаёт короткоживущий `chat_token` через `POST /api/chat/token`
- Chat service `chat.houbamzdar.cz`
  - проверяет `chat_token`
  - читает пользователей и их moderation status из основной identity DB в read-only режиме
  - хранит комнаты, direct rooms, сообщения и read-state в отдельной chat DB

## Что уже поддержано

- публичные комнаты (`kind = public`)
- seeded комната `general`
- direct rooms (`kind = dm`)
- поиск пользователей для старта DM
- отправка сообщений
- unread count по room
- mark-as-read

## Локальный запуск

1. Основной монолит:
   - используйте `.env.example`
   - задайте `CHAT_TOKEN_SECRET`, `CHAT_TOKEN_ISSUER`, `CHAT_TOKEN_AUDIENCE`
2. Chat service:
   - скопируйте `.env.chat.example` в отдельный env-файл
3. Запуск:
   ```bash
   go run main.go
   go run ./cmd/chat
   ```

## Основные env для chat-service

- `CHAT_DB_URL`
- `CHAT_DB_TOKEN`
- `IDENTITY_DB_URL`
- `IDENTITY_DB_TOKEN`
- `CHAT_TOKEN_SECRET`
- `CHAT_TOKEN_ISSUER`
- `CHAT_TOKEN_AUDIENCE`
- `FRONT_ORIGIN`

`CHAT_TOKEN_*` должны в точности совпадать с конфигурацией монолита.

## HTTP endpoints

- `GET /health`
- `GET /api/chat/me`
- `GET /api/chat/users/search?q=...`
- `GET /api/chat/rooms/public`
- `POST /api/chat/rooms/public`
- `GET /api/chat/rooms/direct`
- `POST /api/chat/rooms/direct`
- `GET /api/chat/rooms/{roomID}/messages`
- `POST /api/chat/rooms/{roomID}/messages`
- `POST /api/chat/rooms/{roomID}/read`

## Bunny deploy

Рекомендуемый вариант:

- отдельный Magic Containers app
- отдельный custom hostname `chat.houbamzdar.cz`
- отдельная Bunny Database `chathoubamzdar`

Образ собирается из [`Dockerfile.chat`](/home/houbamydar/houbamzdar-mvp/Dockerfile.chat).

Пример:

```bash
docker build -f Dockerfile.chat -t houbamzdar/chat-service:v1 .
docker push houbamzdar/chat-service:v1
```

## Фронтенд

- статическая страница: [`chat.html`](/home/houbamydar/houbamzdar-mvp/web-static/chat.html)
- клиент: [`chat.js`](/home/houbamydar/houbamzdar-mvp/web-static/chat.js)
- frontend получает `chat_token` у монолита и затем ходит в `chat-service` по `Authorization: Bearer ...`
