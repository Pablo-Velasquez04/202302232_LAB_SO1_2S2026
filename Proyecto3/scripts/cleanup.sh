#!/usr/bin/env bash
# Limpieza segura: NO toca volúmenes ni la data de Zot
set -e
echo "== Antes =="; docker system df
docker container prune -f
docker image prune -f                      # solo imágenes huérfanas
docker builder prune --keep-storage 5GB -f # caché de build, deja 5 GB
echo "== Después =="; docker system df
df -h / | tail -1
