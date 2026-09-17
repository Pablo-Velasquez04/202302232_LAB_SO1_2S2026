#!/bin/bash

# Imagenes para la carga de trabajo
IMAGES=(
  "img-alto-cpu"
  "roldyoran/go-client"
  "img-bajo-consumo"
  "img-intruso"
)

# Creación aleatoria de 5 contenedores
for i in {1..5}
do
    RAND_INDEX=$((RANDOM % 4))
    SELECTED_IMAGE=${IMAGES[$RAND_INDEX]}
    
    CLEAN_NAME=${SELECTED_IMAGE////_}
    CONTAINER_NAME="contenedor_${CLEAN_NAME}_$(date +%s)_$i"
    
    docker run -d --name "$CONTAINER_NAME" "$SELECTED_IMAGE" > /dev/null
done