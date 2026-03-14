# Карты и GPS

Краткая рабочая документация по картам и координатам в Houbam Zdar.

## Что реализовано

- координаты сохраняются вместе со снимком в `photo_captures`
- координаты берутся из EXIF GPS, если они есть
- если EXIF GPS нет, используется браузерный Geolocation API
- карты работают на Leaflet + OpenStreetMap
- карта встроена:
  - в общую страницу карты
  - в публичный feed
  - в личный профиль
  - в публичный профиль
  - в lightbox / галерею

## Ключевые файлы

- `web-static/capture.js`
- `web-static/map.html`
- `web-static/map.js`
- `web-static/feed.html`
- `web-static/feed.js`
- `web-static/me.html`
- `web-static/public-profile.html`
- `web-static/gallery.html`
- `web-static/gallery.js`
- `internal/server/captures.go`
- `internal/db/db.go`
- `internal/models/models.go`

## Источник координат

Поток в `web-static/capture.js`:

1. При выборе файла фронтенд пытается прочитать GPS через `exifr.gps(file)`.
2. Если GPS найден в EXIF, используются эти координаты.
3. Если GPS в EXIF нет, фронтенд запрашивает текущую геопозицию через `navigator.geolocation`.
4. После этого изображение нормализуется через canvas.
5. На сервер отправляются:
   - `latitude`
   - `longitude`
   - `accuracy`

Текущая логика приоритета:

- EXIF GPS важнее браузерной геолокации
- если использован EXIF, `accuracy` остается `null`

## Где координаты живут в данных

В `photo_captures` хранятся:

- `latitude REAL`
- `longitude REAL`
- `accuracy_meters REAL`

В API они отдаются как поля capture-модели:

- `latitude`
- `longitude`
- `accuracy_meters`

## Публичная карта

Страница: `web-static/map.html`  
Скрипт: `web-static/map.js`

Поведение:

- карта стартует с центром на Чехии
- данные берутся из `GET /api/public/captures?limit=500`
- на каждый снимок с координатами ставится маркер
- popup показывает:
  - превью фото
  - автора
  - дату

Важно: в текущей реализации нет кластеризации маркеров. Если она понадобится, это отдельная задача.

## Карты в других частях UI

### Feed

В `web-static/feed.js` у поста появляется кнопка `Zobrazit na mapě`, если хотя бы один capture содержит координаты.

Поведение:

- карта разворачивается прямо в карточке поста
- используется Leaflet
- если точек несколько, карта подгоняется по bounds

### Профили

Leaflet подключен в:

- `web-static/me.html`
- `web-static/public-profile.html`

Обе страницы используют координаты снимков для визуализации находок пользователя.

### Галерея и lightbox

- `web-static/gallery.js` передает map data в lightbox
- gallery и public captures используют те же поля координат capture-модели

## Внешние зависимости

Подключаются на страницах:

- `Leaflet CSS`
- `Leaflet JS`
- `exifr` на `capture.html`

## Что проверять после изменений

1. Фото с EXIF GPS сохраняет координаты без обращения к текущей геолокации.
2. Фото без EXIF GPS получает координаты из браузера, если пользователь дал разрешение.
3. `GET /api/public/captures` отдает координаты для опубликованных снимков.
4. `map.html` показывает маркеры и popup.
5. В feed кнопка карты появляется только у постов с координатами.
6. После деплоя обновляются страницы и скрипты, которые реально менялись.
