FROM golang:1.23-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /blog-front ./main.go

FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=builder /blog-front .

COPY --from=builder /app/view ./view
COPY --from=builder /app/static ./static
COPY --from=builder /app/index.html .
COPY --from=builder /app/deploy/config.yaml ./config/config.yaml

EXPOSE 8080

CMD ["./blog-front"]
