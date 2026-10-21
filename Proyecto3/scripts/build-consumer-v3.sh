#!/usr/bin/env bash
set -e
cd "$(dirname "$0")/../go-services"
GO_VERSION=$(go env GOVERSION | sed 's/^go//' | sed 's/-X.*//')
echo "Building consumer v3 with Go ${GO_VERSION}..."
sudo -n docker build --build-arg GO_VERSION=$GO_VERSION --build-arg SERVICE=consumer \
  -t zot:5000/munk8s/go-consumer:v3 . || \
sudo docker build --build-arg GO_VERSION=$GO_VERSION --build-arg SERVICE=consumer \
  -t zot:5000/munk8s/go-consumer:v3 .
echo "Pushing to Zot..."
sudo docker push zot:5000/munk8s/go-consumer:v3
echo "Done!"
