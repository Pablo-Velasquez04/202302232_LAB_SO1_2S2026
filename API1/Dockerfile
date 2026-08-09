FROM golang:1.26.5-alpine

WORKDIR /app

COPY go.mod main.go ./

RUN go mod download

RUN go build -o api1 .

EXPOSE 8080

CMD ["./api1"]