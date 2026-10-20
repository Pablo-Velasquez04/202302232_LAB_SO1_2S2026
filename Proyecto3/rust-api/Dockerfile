ARG RUST_VERSION
FROM rust:${RUST_VERSION}-alpine AS build
RUN apk add --no-cache musl-dev
WORKDIR /src
ENV CARGO_BUILD_JOBS=2
COPY Cargo.toml Cargo.lock ./
COPY src ./src
RUN cargo build --release

FROM alpine:3.20
RUN adduser -D app
COPY --from=build /src/target/release/rust-api /usr/local/bin/rust-api
USER app
EXPOSE 8080
CMD ["rust-api"]
