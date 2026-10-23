#!/usr/bin/env bash
# ==============================================================================
# M.U.M.N.K8s - Script de Levantamiento y Verificación Rápida
# Carnet: 202302232 | País: RUS
# ==============================================================================
set -euo pipefail

SCRIPT_FILE="$(readlink -f "${BASH_SOURCE[0]}")"
SCRIPT_DIR="$(cd "$(dirname "${SCRIPT_FILE}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${PROJECT_ROOT}"

CARNET="${CARNET:-202302232}"
export CARNET

GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

log_info()  { echo -e "${BLUE}[INFO]${NC} $1"; }
log_ok()    { echo -e "${GREEN}[OK]${NC} $1"; }
log_warn()  { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_err()   { echo -e "${RED}[ERROR]${NC} $1"; }

echo -e "${GREEN}================================================================${NC}"
echo -e "${GREEN}   M.U.M.N.K8s - LEVANTAMIENTO DEL PROYECTO (100% LOCAL)         ${NC}"
echo -e "${GREEN}   Carnet: ${CARNET} | País Asignado: RUS                          ${NC}"
echo -e "${GREEN}================================================================${NC}"

# 1. Verificar Docker
log_info "1. Verificando estado de Docker..."
if ! docker info >/dev/null 2>&1; then
    log_err "Docker no está en ejecución. Inicia el servicio Docker con 'sudo systemctl start docker'."
    exit 1
fi
log_ok "Docker está activo."

# 2. Verificar o levantar contenedor Zot
log_info "2. Verificando contenedor de Registro Local Zot..."
if docker ps --format '{{.Names}}' | grep -q "^zot$"; then
    log_ok "Zot ya está en ejecución (puerto 5000)."
elif docker ps -a --format '{{.Names}}' | grep -q "^zot$"; then
    log_info "Iniciando contenedor existente de Zot..."
    docker start zot
    log_ok "Zot iniciado."
else
    log_info "Creando y arrancando contenedor Zot..."
    docker network create munk8s 2>/dev/null || true
    docker run -d --name zot --network munk8s \
        --memory 256m --memory-swap 256m \
        --restart unless-stopped \
        -p 5000:5000 \
        -v ~/munk8s-data/zot:/var/lib/registry \
        -v "${PROJECT_ROOT}/zot/config.json:/etc/zot/config.json:ro" \
        -v "${PROJECT_ROOT}/zot/certs:/etc/zot/certs:ro" \
        ghcr.io/project-zot/zot-linux-amd64:v2.1.22
    log_ok "Zot creado e iniciado."
fi

# 3. Verificar o levantar clúster k3d
log_info "3. Verificando clúster k3d (munk8s)..."
if k3d cluster list | grep -q "^munk8s"; then
    if docker ps --format '{{.Names}}' | grep -q "k3d-munk8s-server-0"; then
        log_ok "El clúster k3d 'munk8s' ya está corriendo."
    else
        log_info "Iniciando clúster k3d 'munk8s'..."
        k3d cluster start munk8s
        log_ok "Clúster k3d iniciado."
    fi
else
    log_info "Creando clúster k3d 'munk8s'..."
    docker network create munk8s 2>/dev/null || true
    k3d cluster create munk8s \
        --network munk8s \
        --servers 1 --agents 0 \
        --servers-memory 6g \
        -p "8080:80@loadbalancer" \
        --registry-config "${PROJECT_ROOT}/k8s/registries.yaml" \
        --volume "${PROJECT_ROOT}/zot/certs/rootCA.pem:/etc/ssl/certs/zot-ca.crt@server:*" \
        --k3s-arg "--kubelet-arg=image-gc-high-threshold=70@server:*" \
        --k3s-arg "--kubelet-arg=image-gc-low-threshold=50@server:*" \
        --k3s-arg "--kubelet-arg=container-log-max-size=10Mi@server:*" \
        --k3s-arg "--kubelet-arg=container-log-max-files=3@server:*"
    log_ok "Clúster k3d creado exitosamente."
fi

# 4. Asegurar contexto de kubectl
log_info "4. Verificando contexto de Kubernetes..."
kubectl config use-context k3d-munk8s >/dev/null 2>&1 || true

# 5. Aplicar manifiestos base
log_info "5. Aplicando namespaces, gateways y recursos base..."
kubectl apply -f "${PROJECT_ROOT}/k8s/00-namespaces.yaml"
kubectl apply -f "${PROJECT_ROOT}/k8s/01-traefik-config.yaml"
kubectl apply -f "${PROJECT_ROOT}/k8s/02-gateway.yaml"
kubectl apply -f "${PROJECT_ROOT}/k8s/03-gatewayclass.yaml"
kubectl apply -f "${PROJECT_ROOT}/k8s/11-httproute.yaml"

# 6. Desplegar Operator SDK (Valkey y Grafana)
log_info "6. Desplegando Operator SDK (v10 con dashboard corregido y Top dinámico)..."
(cd "${PROJECT_ROOT}/operator" && make deploy IMG=zot:5000/munk8s/operator:v10 >/dev/null)
kubectl rollout status deployment/munk8s-operator-controller-manager -n munk8s-operator-system --timeout=90s

# 7. Desplegar Custom Resources gestionados por el Operator
log_info "7. Desplegando Custom Resources (Valkey y Grafana)..."
kubectl apply -f "${PROJECT_ROOT}/k8s/50-valkey-cr.yaml"
kubectl apply -f "${PROJECT_ROOT}/k8s/51-grafana-cr.yaml"

# 8. Sincronizar ConfigMap de dashboards de Grafana
log_info "8. Sincronizando ConfigMap del Dashboard en Grafana..."
kubectl create configmap grafana-dashboards -n data \
    --from-file=munk8s.json="${PROJECT_ROOT}/operator/helm-charts/grafanainstance/dashboards/munk8s.json" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null

# 9. Desplegar Mensajería y Servicios de Aplicación
log_info "9. Desplegando RabbitMQ, gRPC, APIs y Consumer..."
kubectl apply -f "${PROJECT_ROOT}/k8s/30-rabbitmq.yaml"
kubectl apply -f "${PROJECT_ROOT}/k8s/21-grpc-server.yaml"
kubectl apply -f "${PROJECT_ROOT}/k8s/20-go-api.yaml"
kubectl apply -f "${PROJECT_ROOT}/k8s/10-rust-api.yaml"
kubectl apply -f "${PROJECT_ROOT}/k8s/12-rust-hpa.yaml"
kubectl apply -f "${PROJECT_ROOT}/k8s/40-consumer.yaml"

# 9b. Desplegar Falco (Seguridad en Tiempo Real)
docker exec --privileged k3d-munk8s-server-0 mount -t tracefs nodev /sys/kernel/tracing 2>/dev/null || true
if ! helm status falco -n falco >/dev/null 2>&1; then
    log_info "Desplegando Falco (Seguridad)..."
    helm upgrade --install falco falcosecurity/falco --namespace falco --create-namespace -f "${PROJECT_ROOT}/k8s/falco-values.yaml" >/dev/null
fi

# 10. Esperar a que todos los Deployments estén listos
log_info "10. Esperando despliegue completo de pods..."
kubectl rollout status daemonset/falco -n falco --timeout=120s
kubectl rollout status deployment/valkey -n data --timeout=120s
kubectl rollout status deployment/grafana -n data --timeout=120s
kubectl rollout status deployment/rabbitmq -n messaging --timeout=120s
kubectl rollout status deployment/grpc-server -n apps --timeout=120s
kubectl rollout status deployment/go-api -n apps --timeout=120s
kubectl rollout status deployment/rust-api -n apps --timeout=120s
kubectl rollout status deployment/consumer -n apps --timeout=120s
log_ok "Todos los pods están en estado Running y Ready."

# 11. Habilitar Port-Forward para Grafana en segundo plano si no está activo
log_info "11. Configurando acceso local a Grafana..."
pkill -f "kubectl port-forward -n data svc/grafana 3000:3000" 2>/dev/null || true
nohup kubectl port-forward -n data svc/grafana 3000:3000 >/dev/null 2>&1 &
sleep 2

# 12. Prueba de humo (Smoke Test end-to-end)
log_info "12. Ejecutando prueba de conectividad de extremo a extremo..."
TEST_STATUS=$(curl -s -X POST "http://localhost:8080/grpc-${CARNET}" \
    -H "Content-Type: application/json" \
    -d '{"country":"RUS","warplanes_in_air":25,"warships_in_water":15,"timestamp":"2026-10-21T12:00:00Z"}' \
    | grep -o '"status":"ok"' || true)

if [ "${TEST_STATUS}" = '"status":"ok"' ]; then
    log_ok "Flujo completo verificado: Gateway -> Rust -> Go -> gRPC -> RabbitMQ -> Consumer -> Valkey."
else
    log_warn "La prueba de humo no devolvió status ok. Revisa los logs con 'kubectl logs -n apps deploy/rust-api'."
fi

echo ""
echo -e "${GREEN}================================================================${NC}"
echo -e "${GREEN}   ¡SISTEMA LISTO PARA CALIFICACIÓN!                           ${NC}"
echo -e "${GREEN}================================================================${NC}"
echo -e "📊 ${BLUE}Grafana URL:${NC}       http://localhost:3000"
echo -e "   ${YELLOW}Usuario:${NC}          admin"
echo -e "   ${YELLOW}Contraseña:${NC}       munk8s-admin"
echo -e "   ${YELLOW}Dashboard:${NC}        http://localhost:3000/d/mumnk8s-main/m-u-m-n-k8s-monitoring-dashboard"
echo ""
echo -e "🚀 ${BLUE}Tráfico Locust (Prueba Ligera de Fondo para Calificación):${NC}"
echo -e "   CARNET=${CARNET} locust -f locust/locustfile.py --host http://localhost:8080 --headless -u 5 -r 5 -t 15m"
echo ""
echo -e "📈 ${BLUE}Verificar HPA en Vivo:${NC}"
echo -e "   kubectl get hpa -n apps -w"
echo ""
echo -e "🔍 ${BLUE}Consultar Datos en Valkey:${NC}"
echo -e "   kubectl exec -n data deploy/valkey -- valkey-cli zrange top:planes 0 -1 withscores"
echo -e "   kubectl exec -n data deploy/valkey -- valkey-cli hgetall stats"
echo -e "${GREEN}================================================================${NC}"
