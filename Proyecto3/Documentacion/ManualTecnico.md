<div align="center">

<img src="Imagenes/logoUSAC.png" width="150">

## UNIVERSIDAD DE SAN CARLOS DE GUATEMALA

### FACULTAD DE INGENIERÍA

### SISTEMAS OPERATIVOS 1

<br><br>

# MANUAL TÉCNICO

<br><br>

**Catedrático:** Ing. Sergio Arnaldo Méndez Aguilar

**Auxiliar:** Elian Angel Fernando Reyes Yac

**Nombre:** Pablo Daniel Velásquez Hernández

**Carnet:** 202302232

**Sección:** P

**Semestre:** Segundo Semestre 2026

<br><br>

</div>


# M.U.M.N.K8s — Manual Técnico y Guía de Instalación

**Curso:** Sistemas Operativos 1 — Universidad de San Carlos de Guatemala, Facultad de Ingeniería  
**Proyecto:** #3 — Monitoreo de Unidades Militares en la Nube con Kubernetes  
**Carnet:** 202302232 → último dígito 2 → **país asignado: RUS**  
**Ruta del Gateway:** `/grpc-202302232`

> **Nota sobre el entorno.** El enunciado plantea GCP/GKE. El auxiliar y el catedrático confirmaron que el proyecto puede desarrollarse **100% local**. Todo se ejecutó en una laptop (i7 11.ª gen, 16 GB de RAM) con **k3d (k3s dentro de Docker)** en lugar de GKE, y **Zot en un contenedor Docker fuera del clúster** en lugar de una VM de GCP. La arquitectura, los componentes y el flujo de datos son los mismos que pide el enunciado.

---

## Tabla de contenido

1. [Arquitectura general](#1-arquitectura-general)
2. [Flujo completo de datos](#2-flujo-completo-de-datos)
3. [Estructura del repositorio](#3-estructura-del-repositorio)
4. [Guía de instalación](#4-guía-de-instalación)
5. [Zot: publicación y consumo de imágenes](#5-zot-publicación-y-consumo-de-imágenes)
6. [Configuración de Gateway API](#6-configuración-de-gateway-api)
7. [Comunicación REST y gRPC](#7-comunicación-rest-y-grpc)
8. [Uso de RabbitMQ](#8-uso-de-rabbitmq)
9. [Operator (Valkey y Grafana)](#9-operator-valkey-y-grafana)
10. [Diseño de datos en Valkey](#10-diseño-de-datos-en-valkey)
11. [Dashboard de Grafana](#11-dashboard-de-grafana)
12. [Configuración de HPA](#12-configuración-de-hpa)
13. [OCI Artifact](#13-oci-artifact)
14. [Optimización de recursos](#14-optimización-de-recursos)
15. [Pruebas realizadas](#15-pruebas-realizadas)
16. [Conclusiones](#16-conclusiones)
17. [Limitaciones conocidas](#17-limitaciones-conocidas)
18. [Extras (Falco)](#18-extras-falco)

---

## 1. Arquitectura general

### Componentes

| Componente | Tecnología | Namespace | Función |
|---|---|---|---|
| Generador de carga | Locust | host | Envía reportes JSON aleatorios |
| Exposición | Gateway API + Traefik | `kube-system` / `apps` | Entrada única al sistema |
| API REST | Rust (axum) | `apps` | Recibe de Locust, reenvía a Go. Escala con HPA |
| Deployment Go 1 | Go, **2 containers** | `apps` | `api` (REST) + `grpc-client` |
| Deployment Go 2 | Go, **2 containers** | `apps` | `grpc-server` + `writer` (RabbitMQ) |
| Broker | RabbitMQ 3.13 | `messaging` | Cola durable `war-reports` |
| Consumer | Go | `apps` | Lee la cola y escribe en Valkey |
| Operator | Operator SDK (Helm) | `munk8s-operator-system` | Gestiona `ValkeyInstance` y `GrafanaInstance` |
| Base de datos | Valkey 8 | `data` | Agregados y series |
| Visualización | Grafana 11.2 + plugin Redis | `data` | Dashboard |
| Registry | Zot | host (Docker) | Todas las imágenes, con HTTPS |

### Decisiones de diseño

- **k3d en lugar de minikube o kind:** es la opción más liviana, y ya incluye Traefik, metrics-server (necesario para el HPA) y `local-path` (para los PVC).
- **Un namespace por función**, cada uno con `ResourceQuota`, para evitar que un componente consuma toda la RAM.
- **Imágenes sobre Alpine**, pequeñas y con shell disponible.
- **Locust fuera del clúster** para no competir por la RAM del nodo.

---

## 2. Flujo completo de datos

1. **Locust** genera un reporte JSON y lo envía por `POST http://localhost:8080/grpc-202302232`.
2. **Traefik** (Gateway API) recibe la petición. El `HTTPRoute` reescribe el prefijo a `/` y la envía al Service `rust-api`.
3. **Rust** valida el JSON y lo reenvía por HTTP interno a `go-api:8081/report`.
4. En el pod **go-api**, el container `api` valida los datos (país de 3 letras, valores no negativos) y llama por `localhost` al container `grpc-client`.
5. **grpc-client** convierte el país al enum del `.proto` (rechaza países fuera de la lista) y llama por **gRPC** a `grpc-server:50051`, repartiendo la carga con `round_robin` entre réplicas.
6. En el pod **grpc-server**, el container `grpc-server` entrega el mensaje por `localhost` al container `writer`.
7. El **writer** publica el mensaje en la cola durable `war-reports` de **RabbitMQ**, con un pool de 8 canales AMQP.
8. El **consumer** lee la cola (4 workers, prefetch 50, ack manual) y ejecuta un **script Lua atómico** en **Valkey**.
9. **Grafana** lee Valkey a través del plugin `redis-datasource` y dibuja el dashboard.

Formato del mensaje:

```json
{
  "country": "RUS",
  "warplanes_in_air": 42,
  "warships_in_water": 14,
  "timestamp": "2026-10-21T20:15:30Z"
}
```

Rangos: `warplanes_in_air` 0–50, `warships_in_water` 0–30, países `USA, RUS, CHN, ESP, GTM`.

---

## 3. Estructura del repositorio

```
proyecto3/
├── README.md                 # Este manual
├── rust-api/                 # API REST en Rust + Dockerfile
├── go-services/
│   ├── proto/war.proto       # Contrato gRPC
│   ├── cmd/api/              # Go Deployment 1, container 1
│   ├── cmd/grpc-client/      # Go Deployment 1, container 2
│   ├── cmd/grpc-server/      # Go Deployment 2, container 1
│   ├── cmd/writer/           # Go Deployment 2, container 2
│   ├── cmd/consumer/         # Consumer RabbitMQ → Valkey
│   └── Dockerfile            # Único, parametrizado con SERVICE
├── operator/                 # Operator SDK (Helm)
│   ├── watches.yaml
│   ├── config/               # CRDs, RBAC, manager
│   └── helm-charts/
│       ├── valkeyinstance/
│       └── grafanainstance/  # incluye dashboards/munk8s.json
├── grafana-image/            # Dockerfile de Grafana con plugin Redis
├── k8s/                      # Manifiestos (namespaces, gateway, apps, HPA, CRs)
├── locust/                   # locustfile.py y countries.json
├── zot/                      # config.json (los certificados NO se versionan)
├── scripts/                  # build-go.sh, cleanup.sh, bench.sh, pull-config.sh
└── docs/
    ├── bench/                # Resultados del benchmark
    └── img/                  # Capturas
```

---

## 4. Guía de instalación

### 4.1 Requisitos

Docker, k3d, kubectl, helm, Go, Rust (rustup), operator-sdk, protoc (con `protoc-gen-go` y `protoc-gen-go-grpc`), mkcert, oras y Locust (en un entorno virtual de Python).

### 4.2 Protecciones de la máquina (modo ahorro de recursos)

```bash
sudo pacman -S earlyoom && sudo systemctl enable --now earlyoom
```

`/etc/docker/daemon.json`:

```json
{ "log-driver": "json-file", "log-opts": { "max-size": "10m", "max-file": "3" } }
```

Script de limpieza `scripts/cleanup.sh` (no toca volúmenes ni los datos de Zot):

```bash
docker container prune -f
docker image prune -f
docker builder prune --keep-storage 5GB -f
```

### 4.3 Zot con HTTPS

```bash
mkcert -install
mkcert -cert-file zot/certs/zot.crt -key-file zot/certs/zot.key zot localhost 127.0.0.1
cp "$(mkcert -CAROOT)/rootCA.pem" zot/certs/rootCA.pem
echo "127.0.0.1 zot" | sudo tee -a /etc/hosts

docker network create munk8s
docker run -d --name zot --network munk8s --memory 256m --restart unless-stopped \
  -p 5000:5000 \
  -v ~/munk8s-data/zot:/var/lib/registry \
  -v "$PWD/zot/config.json:/etc/zot/config.json:ro" \
  -v "$PWD/zot/certs:/etc/zot/certs:ro" \
  ghcr.io/project-zot/zot-linux-amd64:<VERSION>

sudo mkdir -p /etc/docker/certs.d/zot:5000
sudo cp zot/certs/rootCA.pem /etc/docker/certs.d/zot:5000/ca.crt
```

### 4.4 Clúster k3d

```bash
k3d cluster create munk8s --network munk8s --servers 1 --agents 0 \
  --servers-memory 6g -p "8080:80@loadbalancer" \
  --registry-config k8s/registries.yaml \
  --volume "$PWD/zot/certs/rootCA.pem:/etc/ssl/certs/zot-ca.crt@server:*" \
  --k3s-arg "--kubelet-arg=image-gc-high-threshold=70@server:*" \
  --k3s-arg "--kubelet-arg=image-gc-low-threshold=50@server:*" \
  --k3s-arg "--kubelet-arg=container-log-max-size=10Mi@server:*" \
  --k3s-arg "--kubelet-arg=container-log-max-files=3@server:*"
```

### 4.5 Despliegue de la plataforma

```bash
export CARNET=202302232

kubectl apply -f k8s/00-namespaces.yaml
kubectl apply -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.2.1/standard-install.yaml
kubectl apply -f k8s/01-traefik-config.yaml
kubectl apply -f k8s/02-gateway.yaml

# Secretos de RabbitMQ (no se guardan en el repo)
export RABBIT_PASS=$(openssl rand -hex 8)
for ns in messaging apps; do
  kubectl create secret generic rabbit-cred -n $ns \
    --from-literal=user=munk8s --from-literal=pass=$RABBIT_PASS
done

# Imágenes
./scripts/build-go.sh v2 api grpc-client grpc-server writer consumer

kubectl apply -f k8s/10-rust-api.yaml
envsubst < k8s/11-httproute.yaml | kubectl apply -f -
kubectl apply -f k8s/12-rust-hpa.yaml
kubectl apply -f k8s/20-go-api.yaml -f k8s/21-grpc-server.yaml
kubectl apply -f k8s/30-rabbitmq.yaml
kubectl apply -f k8s/40-consumer.yaml
```

### 4.6 Operator y Custom Resources

```bash
cd operator
export IMG=zot:5000/munk8s/operator:<TAG>
make docker-build docker-push deploy IMG=$IMG
cd ..
kubectl apply -f k8s/50-valkey-cr.yaml
kubectl apply -f k8s/51-grafana-cr.yaml
```

### 4.7 Verificación

```bash
kubectl get pods -A | grep -v kube-system
curl -i http://localhost:8080/grpc-$CARNET/health          # 200 ok
kubectl port-forward -n data svc/grafana 3000:3000         # http://localhost:3000
```

Credenciales de Grafana: usuario `admin`, contraseña definida en el CR `GrafanaInstance`.

---

## 5. Zot: publicación y consumo de imágenes

- Zot corre **fuera del clúster** (contenedor Docker en la red `munk8s`) y sirve por **HTTPS** con un certificado emitido por `mkcert`.
- `config.json` activa `dedupe` y `gc` para ahorrar disco.
- **Publicación:** `docker build -t zot:5000/munk8s/<servicio>:<tag> .` y `docker push`.
- **Consumo:** `k8s/registries.yaml` apunta el mirror `zot:5000` al `https://zot:5000` y monta la CA (`ca_file`) dentro del nodo k3s. Los manifiestos usan `image: zot:5000/munk8s/...`.
- **Todas** las imágenes del sistema salen de Zot, incluidas las de terceros (RabbitMQ, Valkey, Grafana) que se descargan una vez, se re-etiquetan y se suben.
- **Regla de tags:** cada reconstrucción usa un tag nuevo, porque el nodo cachea por tag.

Imágenes publicadas:

```
munk8s/rust-api  munk8s/go-api  munk8s/go-grpc-client  munk8s/go-grpc-server
munk8s/go-writer  munk8s/go-consumer  munk8s/rabbitmq  munk8s/valkey
munk8s/grafana  munk8s/operator  munk8s/artifacts/locust-config (OCI)
```

Verificación del catálogo:

```bash
curl -s https://zot:5000/v2/_catalog | jq
```

---

## 6. Configuración de Gateway API

Se usa **Gateway API en lugar de Ingress**, con **Traefik** y su provider nativo `kubernetesGateway`, como indicó el auxiliar para trabajo local.

1. **CRDs estándar** de Gateway API v1.2.1.
2. **Activación del provider** (`k8s/01-traefik-config.yaml`, `HelmChartConfig` en `kube-system`):
   ```yaml
   providers:
     kubernetesGateway:
       enabled: true
   gateway:
     enabled: false
   ```
3. **Gateway** (`k8s/02-gateway.yaml`): `gatewayClassName: traefik`, listener HTTP en el puerto 8000 (entrypoint `web`).
4. **HTTPRoute** (`k8s/11-httproute.yaml`): prefijo `/grpc-202302232`, con filtro `URLRewrite` (`ReplacePrefixMatch: /`) hacia el Service `rust-api:8080`.

---

## 7. Comunicación REST y gRPC

**REST (Locust → Rust → Go api):** HTTP/JSON. Rust usa `axum` con un `reqwest::Client` compartido (pool de conexiones).

**gRPC (grpc-client → grpc-server):** contrato `proto/war.proto`:

```proto
service WarReportService {
  rpc SendReport (WarReportRequest) returns (WarReportResponse);
}
```

El enum `Countries` limita los países aceptados (`usa, rus, chn, esp, gtm`).

**Concurrencia:**
- `net/http` de Go atiende cada petición en su propia goroutine.
- El `grpc-client` limita las llamadas en vuelo con un **semáforo de 256** para evitar que un pico de carga dispare la RAM.
- El `writer` usa un **pool de 8 canales AMQP**, porque un canal AMQP no puede compartirse entre goroutines.
- El `grpc-client` usa `round_robin` sobre un **Service headless**, de modo que el tráfico se reparte entre réplicas de `grpc-server`.

**Pods de 2 containers** (pedido por el enunciado), comunicados por `localhost`:
- `go-api`: `api` + `grpc-client`
- `grpc-server`: `grpc-server` + `writer`

---

## 8. Uso de RabbitMQ

- Broker principal. Desplegado en `messaging` con imagen desde Zot.
- **Cola durable** `war-reports`; los mensajes se publican como `Persistent`.
- **Writer:** publica con `PublishWithContext` (timeout 3 s). Si la conexión se pierde, el container termina y Kubernetes lo reinicia.
- **Consumer:** `Qos(prefetch=50)`, **ack manual**, 4 workers. Un mensaje corrupto se descarta (`Nack` sin requeue). Si Valkey falla, se devuelve a la cola (`Nack` con requeue).
- **Límites:** memoria 640 Mi, watermark absoluto de 400 MiB para que RabbitMQ aplique contrapresión antes de ser terminado por el kernel.

Verificación:

```bash
kubectl exec -n messaging deploy/rabbitmq -- rabbitmqctl list_queues name messages consumers
```

---

## 9. Operator (Valkey y Grafana)

Se construyó **un Operator con Operator SDK (plugin Helm)** que administra **dos kinds independientes**, instalado dentro del clúster.

```bash
operator-sdk init --plugins helm --domain munk8s.io --project-name munk8s-operator
operator-sdk create api --group cache --version v1alpha1 --kind ValkeyInstance
operator-sdk create api --group observability --version v1alpha1 --kind GrafanaInstance
```

### 9.1 CRDs

| Kind | Grupo | Chart |
|---|---|---|
| `ValkeyInstance` | `cache.munk8s.io/v1alpha1` | `helm-charts/valkeyinstance` |
| `GrafanaInstance` | `observability.munk8s.io/v1alpha1` | `helm-charts/grafanainstance` |

`watches.yaml` asocia cada GVK con su chart.

### 9.2 Lógica de reconciliación

El Operator es *Helm-based*: el controlador no contiene código Go propio. Por cada evento sobre un CR:

1. Lee el `spec` del CR y lo usa como **`values`** del chart.
2. Renderiza el chart y compara con el estado real del clúster (*release* de Helm).
3. **Crea** los recursos si el CR es nuevo, **actualiza** (`helm upgrade`) si cambió el `spec` o si alguien modificó/borró un recurso, y **elimina** (`helm uninstall`) cuando se borra el CR.
4. Reconcilia periódicamente, lo que produce **auto-reparación**.

El nombre del CR se usa como `.Release.Name`, por lo que el Service de Valkey siempre se llama `valkey` y el consumer lo encuentra en `valkey.data.svc.cluster.local:6379`.

### 9.3 Chart de Valkey

Recursos: **Deployment** (estrategia `Recreate`), **Service**, **PVC**.

Parámetros del CR: `maxmemory`, `persistence.size`, `resources`, `image`.

Decisiones:
- **AOF** (`appendonly yes`, `everysec`) + **PVC** = persistencia real ante la muerte del pod.
- **`maxmemory-policy volatile-lru`:** si se llega al límite, solo se expulsan claves **con TTL** (streams de datos crudos), nunca los agregados del dashboard. Es la política de expiración que recomienda el enunciado.
- **`Recreate`:** el PVC es `ReadWriteOnce`; no pueden convivir dos pods.

### 9.4 Chart de Grafana

Recursos: **Deployment**, **Service**, **Secret** (credenciales de admin), **ConfigMap de datasource/provisioning** y **ConfigMap con el dashboard** (`dashboards/munk8s.json`).

- El **datasource** `Valkey` (`redis-datasource`) se provisiona por archivo, apuntando a `valkey.data.svc.cluster.local:6379`.
- El **dashboard** se provisiona desde un ConfigMap, así que aparece sin importarlo a mano.
- Las anotaciones `checksum/*` reinician el pod cuando cambia la configuración.
- **Sin PVC:** Grafana no guarda estado propio relevante (todo se provisiona).
- La imagen es propia (`munk8s/grafana`): Grafana oficial + plugin `redis-datasource` ya instalado, para no depender de internet al arrancar.
- Limitación: la contraseña de admin va en texto plano en el CR (aceptable en laboratorio).

### 9.5 RBAC

El `role.yaml` generado cubre el CR y los Secrets de Helm, pero no los recursos que el chart crea. Se añadió `config/rbac/chart_role.yaml` (ClusterRole sobre `services`, `persistentvolumeclaims`, `configmaps`, `secrets`, `serviceaccounts`, `deployments`, `statefulsets`) y su `ClusterRoleBinding` al ServiceAccount del manager.

### 9.6 Evidencia de reconciliación

**Valkey**

```bash
kubectl apply -f k8s/50-valkey-cr.yaml                                   # CREAR
kubectl patch valkeyinstance valkey -n data --type merge \
  -p '{"spec":{"maxmemory":"150mb"}}'                                    # ACTUALIZAR
kubectl delete deploy valkey -n data                                     # AUTO-REPARACIÓN
kubectl delete valkeyinstance valkey -n data                             # ELIMINAR
```

**Grafana**

```bash
kubectl patch grafanainstance grafana -n data --type merge \
  -p '{"spec":{"resources":{"limits":{"memory":"448Mi"}}}}'
```

> La reconciliación se demuestra con `maxmemory` y `resources` y no con `storage`, porque el provisioner `local-path` no permite redimensionar un PVC.

---

## 10. Diseño de datos en Valkey

Cada reporte se procesa con **un script Lua**, que Valkey ejecuta de forma **atómica**. Con 4 workers concurrentes, un ciclo "leer–comparar–escribir" desde Go tendría condiciones de carrera.

| Clave | Tipo | Contenido | Uso en el dashboard |
|---|---|---|---|
| `stats` | hash | `max_planes`, `min_planes`, `max_ships`, `min_ships`, `mode_planes`, `mode_ships` | Máximos, mínimos y modas |
| `freq:planes`, `freq:ships` | zset | frecuencia de cada valor | Base de la moda |
| `top:planes`, `top:ships` | zset | acumulado por país | Top de países |
| `reports:count` | hash | país → total de reportes | Total del país asignado |
| `series:<PAÍS>` | stream | `planes`, `ships`, `ts` | Serie temporal |

- Los streams llevan `MAXLEN ~ 5000` y **TTL de 24 h**. Los agregados **no** llevan TTL.
- La moda se mantiene incrementalmente con un zset de frecuencias.

---

## 11. Dashboard de Grafana

Título: **M.U.M.N.K8s MONITORING DASHBOARD**. Refresco: 5 s. Provisionado de forma declarativa desde el chart.

| # | Panel | Fuente |
|---|---|---|
| 1 | Máx. aviones en aire (global) | `HGET stats max_planes` |
| 2 | Mín. aviones en aire (global) | `HGET stats min_planes` |
| 3 | Máx. barcos en mar (global) | `HGET stats max_ships` |
| 4 | Mín. barcos en mar (global) | `HGET stats min_ships` |
| 5 | Moda de aviones | `HGET stats mode_planes` |
| 6 | Moda de barcos | `HGET stats mode_ships` |
| 7 | Top países con más aviones | `ZSCORE top:planes <país>` ×5, ordenado de mayor a menor |
| 8 | Top países con más barcos | `ZSCORE top:ships <país>` ×5, ordenado de mayor a menor |
| 9 | País asignado | Texto fijo: **RUS** |
| 10 | Total de reportes de RUS | `HGET reports:count RUS` |
| 11 | Serie temporal de RUS (aviones y barcos) | `XRANGE series:RUS - +` |

Transformaciones usadas en los Top: unir las 5 consultas en filas, convertir el valor a número y ordenar descendente. En la serie temporal: filtrar campos y convertir `planes` y `ships` a numérico.

---

## 12. Configuración de HPA

`k8s/12-rust-hpa.yaml`:

```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata: { name: rust-api, namespace: apps }
spec:
  scaleTargetRef: { apiVersion: apps/v1, kind: Deployment, name: rust-api }
  minReplicas: 1
  maxReplicas: 3
  metrics:
  - type: Resource
    resource:
      name: cpu
      target: { type: Utilization, averageUtilization: 30 }
```

- **Requisito:** `requests.cpu: 100m` en el Deployment de Rust, porque el porcentaje se calcula sobre el request.
- Los datos de CPU los provee el **metrics-server** de k3s.
- El HPA escala de 1 a un máximo de 3 réplicas cuando el uso de CPU supera el 30 % del request. Bajo carga controlada con Locust (`-u 150 -r 30`) se observa el escalado automático y el regreso a 1 réplica al cesar la carga.

---

## 13. OCI Artifact

- **Archivo:** `locust/countries.json`, con la lista de países y los rangos de generación.
  ```json
  { "countries": ["USA","RUS","CHN","ESP","GTM"],
    "ranges": { "warplanes_in_air": [0,50], "warships_in_water": [0,30] } }
  ```
- **Publicación:**
  ```bash
  oras push zot:5000/munk8s/artifacts/locust-config:v1 \
    --artifact-type application/vnd.munk8s.config.v1+json \
    countries.json:application/json
  ```
- **Uso:** `scripts/pull-config.sh` ejecuta `oras pull` y deja el archivo en `locust/config/`. El `locustfile.py` **lee ese archivo** para generar países y rangos; no hay valores fijos en el código. Locust no arranca sin él.

---

## 14. Optimización de recursos

El proyecto corre en 16 GB de RAM y disco limitado, por lo que se aplicaron controles en cada capa:

| Capa | Medida |
|---|---|
| Clúster | `--servers-memory 6g`; GC de imágenes del kubelet (70 %/50 %); rotación de logs |
| Namespaces | `ResourceQuota` por namespace y `LimitRange` en `apps` |
| Pods | `requests` y `limits` en todos los containers |
| Go | `GOMEMLIMIT=60MiB` para que el GC respete el límite del pod |
| Rust | Compilación con `lto=thin`, `strip` y sin TLS (tráfico interno) |
| RabbitMQ | Watermark absoluto de memoria |
| Valkey | `maxmemory` + `volatile-lru` + TTL en datos crudos |
| Docker | Logs limitados; `scripts/cleanup.sh` tras cada build |
| Zot | `dedupe` y `gc` activados |
| Host | `earlyoom`; `CARGO_BUILD_JOBS=2` |

---

## 15. Pruebas realizadas

### 15.1 Funcionalidad del pipeline

| Prueba | Resultado |
|---|---|
| `GET /grpc-202302232/health` | 200 `ok` |
| `POST` válido | 200 `{"status":"ok"}` |
| `POST` con país `FRA` | 400 (rechazado por el enum) |
| Locust, 20 usuarios, 30 s | 8 748 requests, **0 fallos**, mediana 9 ms, ~295 req/s |
| Cola de RabbitMQ tras la carga | 0 mensajes, 1 consumer |
| Persistencia: matar el pod de Valkey | Los datos se conservan gracias a AOF + PVC |

### 15.2 Comparativa de réplicas (grpc-server + writer: 1 vs 2)

Script: `scripts/bench.sh`. Reinicia `go-api` tras cada escalado, porque el cliente gRPC resuelve el DNS del Service headless solo al arrancar; sin eso todo el tráfico seguiría yendo a la réplica antigua.

**Análisis:** Con 2 réplicas de `grpc-server`, el throughput a 100 usuarios mejora porque el `grpc-client` reparte las llamadas en `round_robin`. La latencia p95 cae notablemente cuando el pod individual dejaba de ser el cuello de botella. Con 1 réplica, la cola de RabbitMQ se acumula a cargas altas; con 2, la cola permanece vacía o cercana a cero. El CPU se divide entre ambos pods, visible en los volcados `*.top.txt` de `docs/bench/`.

> **Nota sobre Valkey con 2 réplicas:** el chart usa un Deployment con un PVC `ReadWriteOnce`; dos réplicas simultáneas accederían al mismo volumen y corromperían los datos de AOF. Por eso la comparativa de escalado se hizo sobre el consumer (1 vs 2 réplicas), donde el único estado externo es Valkey y no existe ese riesgo.

### 15.3 HPA

El HPA escala la API de Rust automáticamente cuando el uso de CPU supera el 30 % del request declarado (`100m`). La prueba de escalado se ejecuta con `locust -u 150 -r 30 -t 3m` y se monitorea con `kubectl get hpa,pods -n apps -w`. Al bajar la carga, Kubernetes reduce las réplicas tras el período de estabilización (300 s por defecto).

### 15.4 Limitación metodológica

Locust corre en la misma laptop que el clúster y comparte CPU con él. Por eso los números sirven para **comparar configuraciones entre sí**, no como medidas absolutas de capacidad.

---

## 16. Conclusiones

1. La arquitectura desacopla la ingesta del procesamiento mediante RabbitMQ: el writer responde rápido y el consumer procesa a su ritmo, sin que los picos de carga afecten la latencia de la API.
2. El Operator Helm-based permite gestionar Valkey y Grafana de forma declarativa, con creación, actualización, auto-reparación y eliminación a partir de un Custom Resource, sin código Go adicional.
3. Un script Lua atómico evita condiciones de carrera entre los workers del consumer, garantizando que los agregados en Valkey sean siempre consistentes.
4. El HPA escala Rust según CPU, siempre que el Deployment declare `requests.cpu`. Sin ese campo, el metrics-server no tiene base de referencia y el HPA no actúa.
5. Con 2 réplicas de `grpc-server`, el throughput mejora y la cola de RabbitMQ se mantiene vacía bajo cargas que, con 1 réplica, producían acumulación. La mejora es proporcional a la capacidad del consumer para escribir en Valkey.
6. Es posible ejecutar toda la arquitectura en una laptop con 16 GB de RAM aplicando límites en cada capa: clúster, namespaces, pods, runtime de Go y Rust, y herramientas del host.

---

## 17. Limitaciones conocidas

- **Entorno local:** se usó k3d y un Zot en contenedor en lugar de GKE y una VM de GCP (aprobado por la cátedra).
- **Serie temporal:** el stream conserva ~5000 entradas por país, por lo que bajo carga alta solo se ven los últimos minutos. Con valores aleatorios uniformes la gráfica se ve como una franja densa.
- **Moda y top:** al ser datos aleatorios uniformes, la moda salta entre valores cercanos y los países quedan casi empatados.
- **Contraseña de Grafana** en texto plano en el CR (aceptable en laboratorio).
- **Valkey** corre en una sola réplica porque el PVC es `ReadWriteOnce`.
- **Redimensionar PVC** no es posible con el provisioner `local-path` de k3d.

---

## 18. Extras (Falco)

### 18.1 Seguridad en Tiempo Real con Falco

Se implementó **Falco** dentro del clúster utilizando Helm y el driver nativo `modern_ebpf` para auditar llamadas al sistema (syscalls) en tiempo real sin requerir módulos de kernel propietarios.

#### Manifiesto de la Regla Personalizada (`k8s/falco-values.yaml`)

```yaml
driver:
  kind: modern_ebpf

tty: true

customRules:
  rules-custom.yaml: |-
    - rule: Lectura de Archivo Confidencial en Pod
      desc: Detecta la apertura o lectura de archivos sensibles como /etc/shadow o directorios de secretos
      condition: open_read and (fd.name startswith "/etc/shadow" or fd.name startswith "/app/secretos" or fd.name in (sensitive_files))
      output: "ALERTA_SEGURIDAD_FALCO: Intento de lectura no autorizada (archivo=%fd.name usuario=%user.name pod=%k8s.pod.name namespace=%k8s.ns.name comando=%proc.cmdline)"
      priority: CRITICAL
      tags: [seguridad, confidencial, SO1]
```

#### Simulación de la Amenaza

Para disparar la regla en vivo, se ingresó intencionalmente a uno de los pods del clúster para forzar la lectura de un archivo confidencial simulado y de `/etc/shadow`:

```bash
kubectl exec -n data deploy/valkey -- sh -c "mkdir -p /app && echo 'datos_militares_confidenciales_2026' > /app/secretos && cat /app/secretos"
kubectl exec -n data deploy/valkey -- cat /etc/shadow
```

#### Evidencia de Alertas Capturadas en los Logs de Falco

```text
03:58:18.773052522: Critical ALERTA_SEGURIDAD_FALCO: Intento de lectura no autorizada
  (archivo=/app/secretos usuario=root pod=valkey-5f67c86d9c-pf55g namespace=data
  comando=cat /app/secretos) container_id=a2b78e668321 container_name=valkey
  container_image_repository=zot:5000/munk8s/valkey container_image_tag=8
  k8s_pod_name=valkey-5f67c86d9c-pf55g k8s_ns_name=data

03:58:08.720153902: Warning Sensitive file opened for reading by non-trusted program
  (file=/etc/shadow) evt_type=open user=root user_uid=0 process=cat
  command=cat /etc/shadow container_id=a2b78e668321 container_name=valkey
  container_image_repository=zot:5000/munk8s/valkey container_image_tag=8
  k8s_pod_name=valkey-5f67c86d9c-pf55g k8s_ns_name=data
```

Ambas alertas se interceptaron en tiempo real: la regla personalizada disparó con prioridad **CRITICAL** al leer `/app/secretos`, y la regla de Falco por defecto (`Sensitive file opened for reading`) disparó con **Warning** al leer `/etc/shadow`.
