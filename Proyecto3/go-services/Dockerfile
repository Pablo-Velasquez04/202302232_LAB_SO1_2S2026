ARG GO_VERSION
FROM golang:${GO_VERSION}-alpine AS build
ARG SERVICE
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/app ./cmd/${SERVICE}

FROM alpine:3.20
RUN adduser -D app
COPY --from=build /out/app /usr/local/bin/app
USER app
CMD ["app"]
