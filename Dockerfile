# Этап 1: Сборка
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Устанавливаем зависимости
COPY go.mod go.sum ./
RUN go mod download

# Копируем исходный код
COPY . .

# Собираем бинарник
RUN CGO_ENABLED=0 GOOS=linux go build -o bff .

# Этап 2: Финальный минимальный образ
FROM alpine:latest

WORKDIR /app

# Устанавливаем корневые сертификаты для работы HTTPS/OIDC
RUN apk --no-cache add ca-certificates

# Копируем бинарник из builder
COPY --from=builder /app/bff .

EXPOSE 8080

CMD ["./bff"]
