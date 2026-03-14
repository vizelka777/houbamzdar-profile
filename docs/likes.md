# Лайки публикаций

Краткая рабочая документация по системе лайков в Houbam Zdar.

## Что реализовано

- Лайки доступны только авторизованным пользователям.
- Один пользователь может поставить только один лайк на один пост.
- Повторный `POST` в тот же endpoint работает как toggle: лайк снимается.
- Публичная лента всегда отдает:
  - `likes_count`
  - `is_liked_by_me`
- При удалении поста связанные лайки удаляются из БД.

## Ключевые файлы

- `internal/db/db.go`
- `internal/models/models.go`
- `internal/server/posts.go`
- `internal/server/server.go`
- `web-static/feed.js`
- `web-static/styles.css`
- `internal/db/db_test.go`
- `internal/server/likes_test.go`

## Схема данных

Миграция: `20260314_create_post_likes_table`

Таблица `post_likes`:

- `post_id TEXT NOT NULL`
- `user_id INTEGER NOT NULL`
- `created_at TEXT NOT NULL DEFAULT (datetime('now'))`
- `PRIMARY KEY (post_id, user_id)`

Дополнительно:

- индекс `idx_post_likes_post_id`
- индекс `idx_post_likes_user_id`
- foreign keys на `posts(id)` и `users(id)` с `ON DELETE CASCADE`

В коде удаление лайков также делается явно в `DeletePost`, чтобы не зависеть только от поведения foreign keys конкретной SQLite/libSQL конфигурации.

## API

### Toggle like

`POST /api/posts/{postID}/like`

Требования:

- пользователь должен быть залогинен
- пост должен существовать
- пост должен быть `published`

Ответ:

```json
{
  "ok": true,
  "likes_count": 3,
  "is_liked": true
}
```

Ошибки:

- `401` для гостя
- `404` если пост не найден или не опубликован
- `500` для ошибок БД

## Поля в ленте

Структура `Post` расширена полями:

- `likes_count`
- `is_liked_by_me`

Они заполняются:

- в `ListPosts(...)`
- в `ListPublicPosts(...)`
- в `GetPost(...)`

Для гостя `is_liked_by_me` всегда `false`.

## Фронтенд

Лайк рендерится в `web-static/feed.js`.

Текущее поведение:

- для гостя клик по кнопке лайка ведет на `${API_URL}/auth/login?next=feed`
- для авторизованного пользователя UI обновляется оптимистично
- при ошибке состояние кнопки откатывается
- повторный клик блокируется, пока не завершится текущий запрос

Классы и стили:

- `.like-btn`
- `.like-btn.active`

## Что проверять после изменений

1. Гость не может лайкать пост.
2. Авторизованный пользователь может поставить и снять лайк.
3. В `/api/public/posts` корректно меняются `likes_count` и `is_liked_by_me`.
4. После удаления поста в `post_likes` не остается мусора.
5. После деплоя обновлены:
   - backend image
   - `feed.js`
   - `styles.css`

## Тесты

Покрытие есть в:

- `internal/server/likes_test.go`
  Проверяет `401` для гостя, `404` для отсутствующего поста, постановку и снятие лайка, а также состояние публичной ленты.
- `internal/db/db_test.go`
  Проверяет миграцию `post_likes`, feed state и cleanup при удалении поста.
