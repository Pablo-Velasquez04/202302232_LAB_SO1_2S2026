# Guía de Levantamiento y Demostración del Proyecto: M.U.M.N.K8s

> **NOTA IMPORTANTE:** Todo el proyecto se ejecuta **100% de forma local** sobre tu máquina (usando Docker, k3d, Zot y Kubernetes), confirmado oficialmente por el auxiliar y el catedrático. No se requiere conexión a Google Cloud Platform (GCP).

---

## 1. Datos del Estudiante y Asignación

* **Carnet:** `202302232`
* **Último dígito:** `2`
* **País Asignado:** **RUS** (Rusia)
* **Ruta de Endpoint Gateway:** `/grpc-202302232`
* **Credenciales de Grafana:**
  * **URL:** [http://localhost:3000](http://localhost:3000)
  * **Usuario:** `admin`
  * **Contraseña:** `munk8s-admin`
  * **Dashboard Principal:** [M.U.M.N.K8s MONITORING DASHBOARD](http://localhost:3000/d/mumnk8s-main/m-u-m-n-k8s-monitoring-dashboard)

---

## 2. Levantamiento Rápido en 1 Solo Paso (Recomendado para Calificación)

Hemos creado el script automatizado [`up.sh`](file:///home/pablo/Documents/202302232_LAB_SO1_2S2026/Proyecto3/up.sh) en la raíz del repositorio. Este script realiza todo el flujo de inicio, despliegue, reconciliación del Operator, verificación de pods, sincronización del dashboard corregido y prueba de conectividad de extremo a extremo.

### Ejecución:

Abre una terminal en la carpeta del proyecto y ejecuta:

```bash
cd ~/Documents/202302232_LAB_SO1_2S2026/Proyecto3
./up.sh
```

El script se encargará automáticamente de:
1. Verificar Docker y la red interna `munk8s`.
2. Iniciar el registro local OCI **Zot** (puerto `5000`).
3. Iniciar el clúster Kubernetes **k3d** `munk8s` (con Traefik Gateway en puerto `8080`).
4. Aplicar Namespaces (`apps`, `data`, `messaging`), ResourceQuotas y LimitRanges.
5. Desplegar el **Operator SDK** con la imagen actualizada (`zot:5000/munk8s/operator:v10`).
6. Aplicar los Custom Resources de **Valkey** y **Grafana**.
7. Desplegar **RabbitMQ**, **gRPC Server + Writer**, **Go API + gRPC Client**, **Rust API**, **HPA** y **Consumer**.
8. Sincronizar el dashboard de Grafana con los 11 paneles corregidos y probados.
9. Levantar el túnel `port-forward` para Grafana en segundo plano.
10. Ejecutar un **Smoke Test** enviando un reporte de prueba a través de Traefik para validar toda la canalización.

---

## 3. Levantamiento Manual Paso a Paso

Si el auxiliar solicita ver el levantamiento comando por comando, sigue este orden:

### Paso 1: Registro Local Zot
```bash
# Iniciar contenedor de Zot si estaba detenido
docker start zot 2>/dev/null || docker run -d --name zot --network munk8s \
  --memory 256m --memory-swap 256m --restart unless-stopped -p 5000:5000 \
  -v ~/munk8s-data/zot:/var/lib/registry \
  -v "$PWD/zot/config.json:/etc/zot/config.json:ro" \
  -v "$PWD/zot/certs:/etc/zot/certs:ro" \
  ghcr.io/project-zot/zot-linux-amd64:v2.1.22

# Verificar catálogo seguro en Zot
curl -sk https://zot:5000/v2/_catalog
```

### Paso 2: Iniciar Clúster k3d
```bash
# Si el clúster ya existe:
k3d cluster start munk8s

# Si necesitas crearlo desde cero:
k3d cluster create munk8s \
  --network munk8s \
  --servers 1 --agents 0 \
  --servers-memory 6g \
  -p "8080:80@loadbalancer" \
  --registry-config k8s/registries.yaml \
  --volume "$PWD/zot/certs/rootCA.pem:/etc/ssl/certs/zot-ca.crt@server:*" \
  --k3s-arg "--kubelet-arg=image-gc-high-threshold=70@server:*" \
  --k3s-arg "--kubelet-arg=image-gc-low-threshold=50@server:*" \
  --k3s-arg "--kubelet-arg=container-log-max-size=10Mi@server:*" \
  --k3s-arg "--kubelet-arg=container-log-max-files=3@server:*"
```

### Paso 3: Recursos Base y Red
```bash
kubectl apply -f k8s/00-namespaces.yaml
kubectl apply -f k8s/01-traefik-config.yaml
kubectl apply -f k8s/02-gateway.yaml
kubectl apply -f k8s/03-gatewayclass.yaml
kubectl apply -f k8s/11-httproute.yaml
```

### Paso 4: Operator SDK (Valkey y Grafana)
```bash
cd operator
make deploy IMG=zot:5000/munk8s/operator:v10
kubectl rollout status deployment/munk8s-operator-controller-manager -n munk8s-operator-system
cd ..
```

### Paso 5: Custom Resources (Gestionados por el Operator)
```bash
kubectl apply -f k8s/50-valkey-cr.yaml
kubectl apply -f k8s/51-grafana-cr.yaml

# Sincronizar el ConfigMap del dashboard con el JSON corregido (versión 100)
kubectl create configmap grafana-dashboards -n data \
  --from-file=munk8s.json=operator/helm-charts/grafanainstance/dashboards/munk8s.json \
  --dry-run=client -o yaml | kubectl apply -f -
```

### Paso 6: Mensajería y Microservicios
```bash
kubectl apply -f k8s/30-rabbitmq.yaml
kubectl apply -f k8s/21-grpc-server.yaml
kubectl apply -f k8s/20-go-api.yaml
kubectl apply -f k8s/10-rust-api.yaml
kubectl apply -f k8s/12-rust-hpa.yaml
kubectl apply -f k8s/40-consumer.yaml
```

### Paso 7: Esperar Despliegues y Abrir Grafana
```bash
kubectl rollout status deploy/valkey -n data
kubectl rollout status deploy/grafana -n data
kubectl rollout status deploy/rabbitmq -n messaging
kubectl rollout status deploy/grpc-server -n apps
kubectl rollout status deploy/go-api -n apps
kubectl rollout status deploy/rust-api -n apps
kubectl rollout status deploy/consumer -n apps

# Habilitar acceso local al puerto 3000 de Grafana
pkill -f "kubectl port-forward -n data svc/grafana 3000:3000" 2>/dev/null || true
kubectl port-forward -n data svc/grafana 3000:3000 >/dev/null 2>&1 &
```

---

## 4. Guía de Demostración para la Calificación (Puntos de la Rúbrica)

Durante la calificación, el auxiliar evaluará cada componente. A continuación tienes exactamente qué comandos ejecutar y qué mostrar en pantalla:

### 4.1. Visualización en Grafana (25 Puntos)
1. Abre tu navegador en [http://localhost:3000](http://localhost:3000).
2. Ingresa con `admin` / `munk8s-admin`.
3. Ve a **Dashboards** $\rightarrow$ **M.U.M.N.K8s** $\rightarrow$ **M.U.M.N.K8s MONITORING DASHBOARD**.
4. En otra terminal, genera tráfico ligero con Locust para que el dashboard muestre actividad en vivo:
   ```bash
   CARNET=202302232 locust -f locust/locustfile.py --host http://localhost:8080 --headless -u 5 -r 5 -t 15m
   ```
5. **Explica al auxiliar los 11 paneles requeridos por el enunciado:**
   * **Paneles 1 y 2 (Máx. y Mín. Aviones):** Valores globales (`50` y `0`), acordes a los rangos de Locust.
   * **Paneles 3 y 4 (Máx. y Mín. Barcos):** Valores globales (`30` y `0`).
   * **Paneles 5 y 6 (Moda Aviones y Barcos):** Calculados en tiempo real con zsets y frecuencias en Valkey.
   * **Panel 7 (País Asignado):** Muestra claramente **RUS** (Rusia), correspondiente al carnet `202302232` (terminado en 2).
   * **Panel 8 (Total Reportes de RUS):** Conteo acumulativo de reportes recibidos para Rusia (sube con Locust).
   * **Panel 9 (Top Países - Aviones):** Barra horizontal por país (**RUS, GTM, CHN, USA, ESP**) ordenado de mayor a menor y con las etiquetas claras de los países (no dice `"Value"`).
   * **Panel 10 (Top Países - Barcos):** Barra horizontal de barcos ordenada de mayor a menor con el nombre de cada país.
   * **Panel 11 (Serie Temporal RUS):** Gráfica de evolución continua mostrando la serie de aviones (roja) y de barcos (azul) del país asignado **RUS** generada desde el stream `series:RUS`.

---

### 4.2. Pruebas de Carga con Locust (5 Puntos)
Demuestra la generación de tráfico:
```bash
# Modo Headless (consola):
export CARNET=202302232
locust -f locust/locustfile.py --host http://localhost:8080 --headless -u 20 -r 5 -t 1m

# Modo con Interfaz Web:
locust -f locust/locustfile.py --host http://localhost:8080
# Abre en el navegador: http://localhost:8089
```
Muestra que las peticiones entran a través de `/grpc-202302232` y tienen **0% de fallos**.

---

### 4.3. Autoescalado Horizontal de Pods (HPA) (5 Puntos)
Demuestra el autoescalado de `rust-api` (1 a 3 réplicas con CPU > 30%):

1. En una terminal, observa el estado del HPA en tiempo real:
   ```bash
   kubectl get hpa -n apps -w
   ```
2. En otra terminal, envía una ráfaga sostenida de tráfico para disparar el CPU:
   ```bash
   CARNET=202302232 locust -f locust/locustfile.py --host http://localhost:8080 --headless -u 150 -r 30 -t 2m
   ```
3. Muestra cómo el HPA detecta el incremento de CPU (>30%) y escala automáticamente el deployment `rust-api` de 1 a 2 o 3 réplicas.

---

### 4.4. Operator SDK y Custom Resources (Obligatorio)
Demuestra que Valkey y Grafana son Custom Resources administrados por tu Operator:

```bash
# 1. Ver los CRDs registrados en el clúster
kubectl get crd | grep munk8s

# 2. Ver las instancias de Custom Resources creadas
kubectl get valkeyinstances -n data
kubectl get grafanainstances -n data

# 3. Ver el Operator en ejecución
kubectl get pods -n munk8s-operator-system
```

---

### 4.5. Registro Local Zot y OCI Artifacts (Obligatorio)
Demuestra que las imágenes se construyen y descargan desde tu registro local:

```bash
# 1. Ver catálogo de repositorios e imágenes en Zot
curl -sk https://zot:5000/v2/_catalog | jq .

# 2. Ver tags de una imagen (ejemplo: operator)
curl -sk https://zot:5000/v2/munk8s/operator/tags/list | jq .

# 3. Verificar el OCI Artifact de configuración de Locust
oras manifest fetch zot:5000/munk8s/artifacts/locust-config:v1 \
  --ca-file zot/certs/rootCA.pem | jq .
```

---

### 4.6. Comparativa de Réplicas (1 vs 2 Réplicas gRPC Server + Writer)
Demuestra los resultados del benchmark de rendimiento:

```bash
# Ver los resultados tabulados ya medidos
cat docs/bench/summary.md

# Si te piden correrlo en vivo (dura ~6 min):
./scripts/bench.sh
```

---

## 5. Comandos de Inspección Rápida (Cheat Sheet)

```bash
# Estado general de todos los pods del sistema
kubectl get pods -A

# Ver estado de colas en RabbitMQ
kubectl exec -n messaging deploy/rabbitmq -- rabbitmqctl list_queues name messages consumers

# Ver datos directamente en Valkey
kubectl exec -n data deploy/valkey -- valkey-cli zrange top:planes 0 -1 withscores
kubectl exec -n data deploy/valkey -- valkey-cli hgetall stats
kubectl exec -n data deploy/valkey -- valkey-cli hget reports:count RUS
kubectl exec -n data deploy/valkey -- valkey-cli xlen series:RUS

# Ver logs de ingesta en vivo del Consumer
kubectl logs -n apps deploy/consumer -f --tail 20

# Detener Locust si quedó corriendo en segundo plano
pkill -f locust
```
