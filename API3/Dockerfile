# Etapa 1: Compilación
FROM golang:1.26.5-alpine AS builder
WORKDIR /app
COPY go.mod main.go ./
RUN go mod download
RUN go build -o api3 .

# Etapa 2: Imagen final ligera
FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/api3 .
EXPOSE 8080
CMD ["./api3"]