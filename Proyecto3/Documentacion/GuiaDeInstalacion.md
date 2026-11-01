<div align="center">

<img src="Imagenes/logoUSAC.png" width="150">

## UNIVERSIDAD DE SAN CARLOS DE GUATEMALA

### FACULTAD DE INGENIERÍA

### SISTEMAS OPERATIVOS 1

<br><br>

# GUÍA DE INSTALACIÓN

<br><br>

**Catedrático:** Ing. Sergio Arnaldo Méndez Aguilar

**Auxiliar:** Elian Angel Fernando Reyes Yac

**Nombre:** Pablo Daniel Velásquez Hernández

**Carnet:** 202302232

**Sección:** P

**Semestre:** Segundo Semestre 2026

<br><br>

</div>

# M.U.M.N.K8s — Guía de Instalación

Esta guía levanta el proyecto completo **100% en local** (k3d + Zot en Docker), de cero hasta tener el dashboard de Grafana recibiendo datos. Los comandos están pensados para **Linux (Arch/Omarchy)**; en otras distribuciones cambia solo el gestor de paquetes.

---

## 1. Requisitos de la máquina

### 1.1 Hardware

| Recurso | Mínimo | Recomendado | Notas |
|---|---|---|---|
| CPU | 4 núcleos | 4 núcleos / 8 hilos o más | Probado en i7 de 11.ª gen |
| RAM | 12 GB (con swap) | **16 GB** | Con todo desplegado se usan ~9 GB en total, incluido el escritorio. Compilar Rust agrega 2-4 GB temporales |
| Disco libre | 30 GB | **40 GB** | Imágenes, caché de build, `target/` de Rust, módulos de Go y datos de Zot |
| Swap | 4 GB | 8 GB o zram | Evita congelamientos en los picos de compilación |
| GPU | No se usa | — | |
| Internet | Necesario durante la instalación | — | Para bajar imágenes base y dependencias. Después el clúster no lo necesita |

> Los valores son estimaciones de uso. El tope de RAM del clúster se fija en 6 GB al crearlo.

### 1.2 Sistema

- Linux **x86_64** (las imágenes de Zot usadas son `amd64`).
- Docker en ejecución y tu usuario dentro del grupo `docker`.
- Puertos libres en el host: **5000** (Zot), **8080** (entrada al clúster) y, al usar Grafana, **3000**.

```bash
ss -ltnp | grep -E ':(5000|8080|3000)\b'     # no debe mostrar nada
```

---

## 2. Herramientas necesarias

| Herramienta | Para qué |
|---|---|
| Docker | Contenedores, Zot y base de k3d |
| k3d | Clúster k3s dentro de Docker |
| kubectl | Cliente de Kubernetes |
| helm | Probar charts (`helm template`) |
| Go (1.22 o superior) | Servicios Go |
| Rust (rustup) | API REST |
| protoc + plugins de Go | Generar código gRPC |
| operator-sdk | Construir el Operator |
| make, git, envsubst | Utilidades (`envsubst` viene en el paquete `gettext`) |
| mkcert | Certificado HTTPS para Zot |
| oras | Publicar y descargar el OCI Artifact |
| Python 3 + Locust | Generador de carga |

### 2.1 Instalación en Arch / Omarchy

```bash
sudo pacman -S --needed docker kubectl helm go rustup protobuf make git gettext mkcert python
sudo systemctl enable --now docker
sudo usermod -aG docker $USER        # cierra sesión y vuelve a entrar

rustup default stable

yay -S k3d-bin operator-sdk oras     # o instala los binarios desde sus páginas de releases
```

Plugins de protobuf para Go:

```bash
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.34.2
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1
echo 'export PATH=$PATH:$(go env GOPATH)/bin' >> ~/.bashrc      # o ~/.zshrc
source ~/.bashrc
```

Locust en un entorno virtual:

```bash
python -m venv ~/.venvs/locust
source ~/.venvs/locust/bin/activate
pip install locust
```

### 2.2 Verificar versiones

```bash
docker --version && k3d version && kubectl version --client
helm version --short && go version && rustc --version
protoc --version && operator-sdk version && mkcert -version && oras version
locust --version
```

---

## 3. Obtener el proyecto

```bash
git clone <URL_DEL_REPOSITORIO> ~/proyecto-so1
cd ~/proyecto-so1/proyecto3
export CARNET=202302232
```

> Agrega `export CARNET=202302232` a tu `~/.bashrc` o `~/.zshrc`. Si la variable queda vacía, la ruta del Gateway resulta `/grpc-` y responde 404.

---

## 4. Protecciones de la laptop (hacer una sola vez)

**4.1 earlyoom** (mata el proceso más pesado antes de que el sistema se congele):

```bash
sudo pacman -S earlyoom
sudo systemctl enable --now earlyoom
```

**4.2 Swap.** Comprueba que exista:

```bash
free -h
swapon --show
```

Si no hay swap ni zram, crea un swapfile de 8 GB (en ext4):

```bash
sudo fallocate -l 8G /swapfile
sudo chmod 600 /swapfile
sudo mkswap /swapfile
sudo swapon /swapfile
echo '/swapfile none swap defaults 0 0' | sudo tee -a /etc/fstab
```

> En btrfs el procedimiento es distinto (`btrfs filesystem mkswapfile`). Verifica con `findmnt /`.

**4.3 Límite de logs de Docker.** Si `/etc/docker/daemon.json` ya existe, fusiona el contenido en vez de sobrescribirlo:

```bash
cat /etc/docker/daemon.json 2>/dev/null
sudo tee /etc/docker/daemon.json <<'EOF'
{
  "log-driver": "json-file",
  "log-opts": { "max-size": "10m", "max-file": "3" }
}
EOF
sudo systemctl restart docker
```

**4.4 Script de limpieza** (`scripts/cleanup.sh`, ya incluido en el repo). Se ejecuta después de cada build y **no toca volúmenes ni los datos de Zot**:

```bash
chmod +x scripts/*.sh
./scripts/cleanup.sh
```

> **Nunca** uses `docker volume prune` ni `docker system prune -a --volumes`: podrías borrar el registry.

**Verificación:** `systemctl is-active earlyoom` responde `active` y `docker info | grep -i "logging driver"` muestra `json-file`.

---

## 5. Zot (registry con HTTPS)

**5.1 Certificados:**

```bash
mkdir -p ~/munk8s-data/zot zot/certs
mkcert -install
mkcert -cert-file zot/certs/zot.crt -key-file zot/certs/zot.key zot localhost 127.0.0.1
chmod 644 zot/certs/zot.key
cp "$(mkcert -CAROOT)/rootCA.pem" zot/certs/rootCA.pem
grep -q '127.0.0.1 zot' /etc/hosts || echo "127.0.0.1 zot" | sudo tee -a /etc/hosts
```

**5.2 Contenedor.** Consulta el último tag estable en `https://github.com/project-zot/zot/releases` y no uses `latest`:

```bash
docker network create munk8s 2>/dev/null || true
export ZOT_VERSION=vX.Y.Z        # <-- reemplaza por el tag real

docker run -d --name zot --network munk8s \
  --memory 256m --memory-swap 256m \
  --restart unless-stopped \
  -p 5000:5000 \
  -v ~/munk8s-data/zot:/var/lib/registry \
  -v "$PWD/zot/config.json:/etc/zot/config.json:ro" \
  -v "$PWD/zot/certs:/etc/zot/certs:ro" \
  ghcr.io/project-zot/zot-linux-amd64:$ZOT_VERSION
```

**5.3 Confianza de Docker en el certificado:**

```bash
sudo mkdir -p /etc/docker/certs.d/zot:5000
sudo cp zot/certs/rootCA.pem /etc/docker/certs.d/zot:5000/ca.crt
```

**5.4 Prueba:**

```bash
curl https://zot:5000/v2/_catalog                       # {"repositories":[]}
docker pull alpine:3.20
docker tag alpine:3.20 zot:5000/test/alpine:3.20
docker push zot:5000/test/alpine:3.20
curl https://zot:5000/v2/_catalog                       # incluye test/alpine
```

**Verificación:** `curl` responde sin errores de certificado y el catálogo muestra `test/alpine`.

---

## 6. Clúster k3d

`k8s/registries.yaml` (incluido en el repo) le dice al nodo que confíe en Zot:

```yaml
mirrors:
  "zot:5000":
    endpoint:
      - https://zot:5000
configs:
  "zot:5000":
    tls:
      ca_file: /etc/ssl/certs/zot-ca.crt
```

Crear el clúster (un solo nodo, con tope de 6 GB de RAM):

```bash
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

Se conservan **Traefik** (Gateway API), **metrics-server** (HPA) y **local-path** (PVC) que trae k3s.

**Prueba crítica: bajar una imagen desde Zot dentro del clúster**

```bash
kubectl run test-zot --image=zot:5000/test/alpine:3.20 --restart=Never --command -- sleep 60
kubectl get pod test-zot -w
kubectl delete pod test-zot
```

**Verificación:** el pod pasa a `Running` y `kubectl get nodes` muestra 1 nodo `Ready`.

> Si queda en `ImagePullBackOff`, el problema es la confianza TLS. Revisa:
> ```bash
> docker exec k3d-munk8s-server-0 cat /etc/rancher/k3s/registries.yaml
> docker exec k3d-munk8s-server-0 ls /etc/ssl/certs/zot-ca.crt
> ```

---

## 7. Namespaces, Gateway API y secretos

**7.1 Namespaces con límites de recursos:**

```bash
kubectl apply -f k8s/00-namespaces.yaml
kubectl get ns
kubectl get resourcequota -A
```

**7.2 CRDs de Gateway API y provider de Traefik:**

```bash
export GWAPI_VERSION=v1.2.1
kubectl apply -f https://github.com/kubernetes-sigs/gateway-api/releases/download/$GWAPI_VERSION/standard-install.yaml
kubectl apply -f k8s/01-traefik-config.yaml
kubectl -n kube-system rollout status deploy/traefik
```

**7.3 Gateway:**

```bash
kubectl apply -f k8s/03-gatewayclass.yaml   # solo si este archivo existe en tu repo
kubectl apply -f k8s/02-gateway.yaml
kubectl get gatewayclass
kubectl get gateway -n apps
```

**Verificación:** `gatewayclass traefik` en `ACCEPTED True` y `munk8s-gateway` en `PROGRAMMED True`.

**7.4 Secretos de RabbitMQ** (no se guardan en el repo). Un Secret por namespace:

```bash
export RABBIT_PASS=$(openssl rand -hex 8)
for ns in messaging apps; do
  kubectl create secret generic rabbit-cred -n $ns \
    --from-literal=user=munk8s --from-literal=pass=$RABBIT_PASS
done
```

> Si necesitas recrearlos, bórralos primero con `kubectl delete secret rabbit-cred -n <ns>`.

---

## 8. Construir y publicar las imágenes en Zot

> **Regla de tags:** cada vez que reconstruyas una imagen, sube el tag (`v2`, `v3`...) y actualiza el YAML correspondiente. El nodo cachea por tag y, si lo reutilizas, seguirá usando la imagen vieja. Los tags de abajo deben coincidir con los de `k8s/*.yaml`.

**8.1 Imágenes de terceros** (se descargan una vez y se re-publican en Zot):

```bash
docker pull rabbitmq:3.13-management-alpine
docker tag  rabbitmq:3.13-management-alpine zot:5000/munk8s/rabbitmq:3.13
docker push zot:5000/munk8s/rabbitmq:3.13

docker pull valkey/valkey:8-alpine
docker tag  valkey/valkey:8-alpine zot:5000/munk8s/valkey:8
docker push zot:5000/munk8s/valkey:8

docker build -t zot:5000/munk8s/grafana:11.2.0-redis grafana-image/
docker push zot:5000/munk8s/grafana:11.2.0-redis
```

**8.2 API en Rust** (limita los hilos de compilación para no agotar la RAM):

```bash
cd rust-api
cargo generate-lockfile
export RUST_VERSION=$(rustc --version | cut -d' ' -f2)
docker build --build-arg RUST_VERSION=$RUST_VERSION -t zot:5000/munk8s/rust-api:v1 .
docker push zot:5000/munk8s/rust-api:v1
cd .. && ./scripts/cleanup.sh
```

> Vigila `btop`. Si la RAM sube de 12 GB durante la compilación, baja `CARGO_BUILD_JOBS` a 1 en el Dockerfile.

**8.3 Servicios en Go.** Primero genera el código gRPC y comprueba que compila:

```bash
cd go-services
protoc --go_out=. --go_opt=paths=source_relative \
       --go-grpc_out=. --go-grpc_opt=paths=source_relative proto/war.proto
go mod tidy
go build ./... && echo "COMPILA OK"
cd ..
```

Luego construye y publica con el script (acepta `<tag> [servicios...]`):

```bash
./scripts/build-go.sh v1 api grpc-client
./scripts/build-go.sh v2 grpc-server writer
./scripts/build-go.sh v3 consumer
./scripts/cleanup.sh
```

**Verificación:**

```bash
curl -s https://zot:5000/v2/_catalog
```

Debe incluir `munk8s/rust-api`, `munk8s/go-api`, `munk8s/go-grpc-client`, `munk8s/go-grpc-server`, `munk8s/go-writer`, `munk8s/go-consumer`, `munk8s/rabbitmq`, `munk8s/valkey` y `munk8s/grafana`.

---

## 9. Operator (Valkey y Grafana)

El Operator ya viene generado en `operator/` (con `watches.yaml`, los dos charts y el dashboard). Solo se compila, publica y despliega.

```bash
cd operator
export IMG=zot:5000/munk8s/operator:v1        # usa un tag nuevo en cada reconstrucción
make docker-build IMG=$IMG
make docker-push  IMG=$IMG
make deploy       IMG=$IMG
kubectl rollout status deploy/munk8s-operator-controller-manager -n munk8s-operator-system
cd .. && ./scripts/cleanup.sh
```

> `docker-build` descarga la imagen base `helm-operator` de quay.io y `make deploy` descarga kustomize la primera vez: necesitas internet.

**Verificación:**

```bash
kubectl get crd | grep munk8s
kubectl get deploy -n munk8s-operator-system
```

Deben aparecer `valkeyinstances.cache.munk8s.io` y `grafanainstances.observability.munk8s.io`, y el manager en `1/1`.

---

## 10. Desplegar la plataforma

El orden importa: **Valkey antes del consumer**, para que este tenga dónde escribir.

**10.1 Datos y visualización (los crea el Operator a partir de Custom Resources):**

```bash
kubectl apply -f k8s/50-valkey-cr.yaml
kubectl apply -f k8s/51-grafana-cr.yaml
kubectl get deploy,svc,pvc,pods -n data
```

**Verificación:**

```bash
kubectl exec -n data deploy/valkey -- valkey-cli ping          # PONG
```

Deben existir `valkey` y `grafana` en `1/1`, el Service `valkey` y el PVC `valkey-data` en `Bound`.

**10.2 Mensajería:**

```bash
kubectl apply -f k8s/30-rabbitmq.yaml
kubectl rollout status deploy/rabbitmq -n messaging --timeout=180s
kubectl exec -n messaging deploy/rabbitmq -- rabbitmqctl list_queues
```

**10.3 Aplicaciones:**

```bash
kubectl apply -f k8s/10-rust-api.yaml
envsubst < k8s/11-httproute.yaml | kubectl apply -f -
kubectl apply -f k8s/20-go-api.yaml -f k8s/21-grpc-server.yaml
kubectl apply -f k8s/40-consumer.yaml
kubectl apply -f k8s/12-rust-hpa.yaml
kubectl get pods -n apps
```

**Verificación:** en `apps`, `go-api` y `grpc-server` en **2/2**, y `rust-api` y `consumer` en **1/1**. Revisa también:

```bash
kubectl get pods -A | grep -v kube-system
kubectl get hpa -n apps                       # TARGETS tipo 2%/30%
kubectl exec -n messaging deploy/rabbitmq -- rabbitmqctl list_queues name messages consumers
# war-reports   0   1
```

---

## 11. OCI Artifact y Locust

**11.1 Publicar la configuración de Locust como OCI Artifact** (una sola vez):

```bash
cd locust
oras push zot:5000/munk8s/artifacts/locust-config:v1 \
  --artifact-type application/vnd.munk8s.config.v1+json \
  countries.json:application/json
cd ..
```

> Si `oras` se queja del certificado, añade `--ca-file zot/certs/rootCA.pem` a cada comando.

**11.2 Descargarla y generar tráfico.** Locust **necesita** el archivo descargado:

```bash
source ~/.venvs/locust/bin/activate
./scripts/pull-config.sh
CARNET=$CARNET locust -f locust/locustfile.py --host http://localhost:8080 \
  --headless -u 20 -r 10 -t 30s
```

---

## 12. Verificación extremo a extremo

**12.1 Petición manual:**

```bash
curl -i http://localhost:8080/grpc-$CARNET/health        # 200 ok

curl -i -X POST http://localhost:8080/grpc-$CARNET \
  -H 'Content-Type: application/json' \
  -d '{"country":"RUS","warplanes_in_air":42,"warships_in_water":14,"timestamp":"2026-03-12T20:15:30Z"}'
# 200 {"status":"ok"}

curl -i -X POST http://localhost:8080/grpc-$CARNET \
  -H 'Content-Type: application/json' \
  -d '{"country":"FRA","warplanes_in_air":1,"warships_in_water":1,"timestamp":"2026-03-12T20:15:30Z"}'
# 400 (país fuera de la lista)
```

**12.2 Datos en Valkey** (después de correr Locust):

```bash
kubectl exec -n data deploy/valkey -- valkey-cli hgetall stats
kubectl exec -n data deploy/valkey -- valkey-cli hgetall reports:count
kubectl exec -n data deploy/valkey -- valkey-cli zrange top:planes 0 -1 withscores
```

**12.3 Dashboard de Grafana:**

```bash
kubectl port-forward -n data svc/grafana 3000:3000
```

Abre `http://localhost:3000`. Usuario `admin` y la contraseña definida en `k8s/51-grafana-cr.yaml`. El dashboard está en **Dashboards → M.U.M.N.K8s**. Para ver los valores moverse, deja Locust corriendo en otra terminal:

```bash
CARNET=$CARNET locust -f locust/locustfile.py --host http://localhost:8080 \
  --headless -u 5 -r 5 -t 20m
```

**12.4 Monitoreo de recursos:**

```bash
kubectl top pods -A
docker stats --no-stream
free -h
```

---

## 13. Pruebas opcionales

```bash
# Comparativa 1 vs 2 réplicas de grpc-server + writer (~6 min, resultados en docs/bench/)
./scripts/bench.sh

# HPA: observar en una terminal...
kubectl get hpa,pods -n apps -w
# ...y generar carga sostenida en otra
CARNET=$CARNET locust -f locust/locustfile.py --host http://localhost:8080 \
  --headless -u 150 -r 30 -t 3m
```

Cierra cualquier Locust anterior con `pkill -f locust` antes de ejecutar `bench.sh`.

---

## 14. Uso diario

```bash
# Apagar sin perder nada (libera la RAM)
k3d cluster stop munk8s
docker stop zot

# Volver a encender
docker start zot
k3d cluster start munk8s
kubectl get pods -A

# Limpieza de disco
./scripts/cleanup.sh
```

---

## 15. Desinstalar todo

```bash
k3d cluster delete munk8s
docker rm -f zot
docker network rm munk8s
rm -rf ~/munk8s-data/zot          # borra TODAS las imágenes publicadas
sudo sed -i '/127.0.0.1 zot/d' /etc/hosts
sudo rm -rf /etc/docker/certs.d/zot:5000
```

---

## 16. Solución de problemas

| Síntoma | Causa probable | Solución |
|---|---|---|
| `ImagePullBackOff` en cualquier pod | El nodo no confía en Zot, o la imagen/tag no existe | Ver sección 6 (diagnóstico) y revisar `curl -s https://zot:5000/v2/_catalog` |
| Pod usa una versión vieja del código | Se reutilizó un tag | Sube el tag y actualiza el YAML |
| `curl` al Gateway devuelve **404** | `CARNET` vacío, o falla el `URLRewrite` | `echo "[$CARNET]"` y `kubectl describe httproute grpc-route -n apps` |
| Gateway no queda `Programmed` | Versión de CRDs incompatible con Traefik, o puerto del listener | `kubectl describe gateway munk8s-gateway -n apps` |
| `curl` a Rust devuelve **502** | El pod `go-api` aún no está listo | `kubectl get pods -n apps` y revisar logs |
| El CR no crea recursos y el log dice `forbidden` | Falta RBAC del Operator | Revisar `config/rbac/chart_role.yaml` y su binding |
| Consumer con errores `valkey:` | Valkey no desplegado o sin Service | Aplicar `50-valkey-cr.yaml` primero |
| La cola de RabbitMQ crece sin parar | Consumer caído | `kubectl logs -n apps deploy/consumer` |
| Datasource de Grafana falla | Plugin Redis ausente o Valkey inaccesible | Verificar que la imagen sea `munk8s/grafana:11.2.0-redis` y `valkey-cli ping` |
| Dashboard vuelve al estado anterior tras reiniciar Grafana | El JSON del chart no tiene los cambios | Reemplazar `operator/helm-charts/grafanainstance/dashboards/munk8s.json` y reconstruir el Operator con tag nuevo |
| HPA muestra `<unknown>/30%` | metrics-server aún sin datos | Esperar 1-2 minutos; confirmar `requests.cpu` en Rust |
| El sistema se congela por RAM | Compilación o demasiados pods | `earlyoom`, `CARGO_BUILD_JOBS=1`, cerrar el navegador |
| Disco lleno | Caché de Docker | `./scripts/cleanup.sh` |