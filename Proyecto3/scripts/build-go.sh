#!/usr/bin/env bash
# uso: ./scripts/build-go.sh <tag> [servicios...]
set -e
cd "$(dirname "$0")/../go-services"
TAG=${1:?uso: build-go.sh <tag> [servicios...]}; shift
[ $# -eq 0 ] && set -- api grpc-client grpc-server
GO_VERSION=$(go env GOVERSION | sed 's/^go//' | sed 's/-X.*//')
for s in "$@"; do
  sudo docker build --build-arg GO_VERSION=$GO_VERSION --build-arg SERVICE=$s \
    -t zot:5000/munk8s/go-$s:$TAG .
  sudo docker push zot:5000/munk8s/go-$s:$TAG
done
