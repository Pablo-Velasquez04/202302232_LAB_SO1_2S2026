#!/usr/bin/env bash
# Descarga la configuración de Locust desde Zot como OCI Artifact
set -e
cd "$(dirname "$0")/.."
mkdir -p locust/config
oras pull zot:5000/munk8s/artifacts/locust-config:v1 \
  --ca-file zot/certs/rootCA.pem \
  -o locust/config
echo "Descargado:"; cat locust/config/countries.json
