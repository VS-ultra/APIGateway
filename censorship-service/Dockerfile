# Stage 1: сборка
FROM golang:1.21-alpine AS builder

WORKDIR /build


COPY go.mod* ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o censorship-service .

# Stage 2: runtime
FROM alpine:3.19

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

COPY --from=builder /build/censorship-service .


COPY forbidden_words.txt .

RUN addgroup -S appgroup && adduser -S appuser -G appgroup && \
    chown -R appuser:appgroup /app

USER appuser

CMD ["./censorship-service"]