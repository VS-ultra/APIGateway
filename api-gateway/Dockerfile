# Stage 1: сборка
FROM golang:1.21-alpine AS builder

WORKDIR /build


COPY go.mod* ./
RUN go mod download

COPY . .


RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o api-gateway .

# Stage 2: минимальный runtime
FROM alpine:3.19


RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

COPY --from=builder /build/api-gateway .


RUN addgroup -S appgroup && adduser -S appuser -G appgroup && \
    chown appuser:appgroup api-gateway

USER appuser

CMD ["./api-gateway"]