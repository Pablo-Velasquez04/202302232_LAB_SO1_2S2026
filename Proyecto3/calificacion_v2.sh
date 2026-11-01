#!/usr/bin/env bash
# ==============================================================================
# PROYECTO 3: M.U.M.N.K8s - GUIA DE CALIFICACION V2
# Estudiante: Carnet 202302232 | Pais: RUS | Endpoint: /grpc-202302232
# ==============================================================================

set -e

SEPARATOR="=============================================================================="
SUB_SEPARATOR="------------------------------------------------------------------------------"

PAUSE_MODE="false"

pause_step() {
    if [ "$PAUSE_MODE" = "true" ]; then
        echo ""
        read -r -p "Presione ENTER para continuar al siguiente paso (o Ctrl+C para salir)..." _
        echo ""
    fi
}

run_cmd() {
    local cmd="$1"
    echo ""
    echo "[COMANDO]: $cmd"
    echo "$SUB_SEPARATOR"
    eval "$cmd" || true
    echo "$SUB_SEPARATOR"
}

print_header() {
    local section_id="$1"
    local title="$2"
    echo ""
    echo "$SEPARATOR"
    echo "$section_id - $title"
    echo "$SEPARATOR"
}

sec_1_1() {
    print_header "1.1" "ARQUITECTURA GENERAL, DESPLIEGUE (GATEWAY API)"
    echo "Descripcion: Verificar nodos del cluster y enrutamiento con Gateway API."
    
    run_cmd "kubectl get nodes -o wide"
    run_cmd "kubectl get gateway,httproute -n apps"
    run_cmd "kubectl describe httproute grpc-route -n apps | grep -E 'Hostnames|Rules|Path|Backend'"
    
    echo ""
    echo "[PUNTOS A MOSTRAR]:"
    echo "1. Cluster Kubernetes k3d activo con nodos en estado Ready."
    echo "2. Gateway 'munk8s-gateway' (clase traefik) en estado Programmed=True."
    echo "3. HTTPRoute 'grpc-route' configurado para enrutar el prefijo /grpc-202302232."
    pause_step
}

sec_1_2() {
    print_header "1.2" "API REST EN RUST"
    echo "Descripcion: Verificar despliegue y autoescalado HPA de la API en Rust."
    
    run_cmd "kubectl get hpa,deployments -n apps"
    
    echo ""
    echo "[PUNTOS A MOSTRAR]:"
    echo "1. Deployment 'rust-api' activo y disponible en el namespace apps."
    echo "2. HPA 'rust-api' configurado con objetivo de CPU al 30% y rango de 1 a 3 replicas."
    pause_step
}

sec_1_3() {
    print_header "1.3" "SERVICIOS EN GO (API/GRPC CLIENT, GRPC SERVER/WRITERS)"
    echo "Descripcion: Verificar comunicacion entre servicios y flujo gRPC/REST."
    
    run_cmd "kubectl logs deployment/rust-api -n apps --tail=10"
    run_cmd "kubectl logs deployment/go-api -n apps -c grpc-client --tail=10"
    run_cmd "kubectl logs deployment/grpc-server -n apps -c grpc-server --tail=10"
    run_cmd "kubectl logs deployment/grpc-server -n apps -c writer --tail=10"
    
    echo ""
    echo "[PUNTOS A MOSTRAR]:"
    echo "1. Rust recibe las peticiones HTTP REST del Gateway API."
    echo "2. Go API (grpc-client) establece sesion hacia grpc-server:50051."
    echo "3. Go Server (grpc-server) recibe y deserializa las estructuras gRPC."
    echo "4. Go Writer encola los mensajes recibidos hacia RabbitMQ."
    pause_step
}

sec_1_4() {
    print_header "1.4" "ARQUITECTURA GENERAL, DESPLIEGUE EN ZOT"
    echo "Descripcion: Repositorio privado de imagenes OCI Zot fuera del cluster."
    
    run_cmd "curl -k -s https://localhost:5000/v2/_catalog"
    
    echo ""
    echo "[PUNTOS A MOSTRAR]:"
    echo "1. En navegador: Pestaña con https://localhost:5000/v2/_catalog"
    echo "2. Registro Zot corriendo en puerto 5000 con protocolo TLS/HTTPS."
    echo "3. Catalogo de imagenes almacenadas (rust-api, go-api, grpc-server, consumer, operator, valkey, grafana, locust-config)."
    pause_step
}

sec_1_5() {
    print_header "1.5" "MESSAGE BROKER Y CONSUMIDOR RABBITMQ"
    echo "Descripcion: Verificar broker RabbitMQ y consumidor de eventos."
    
    run_cmd "kubectl get pods -n messaging"
    run_cmd "kubectl get pods -n apps -l app=consumer"
    run_cmd "kubectl logs deployment/consumer -n apps --tail=10"
    
    echo ""
    echo "[PUNTOS A MOSTRAR]:"
    echo "1. Pod de RabbitMQ activo y saludable en namespace messaging."
    echo "2. Pod 'consumer' en namespace apps leyendo mensajes de la cola 'war-reports'."
    echo "3. Logs en tiempo real mostrando el procesamiento continuo de reportes."
    pause_step
}

sec_1_6() {
    print_header "1.6" "ALMACENAMIENTO (VALKEY)"
    echo "Descripcion: Verificar Operator SDK, CRD ValkeyInstance y persistencia."
    
    run_cmd "kubectl get crd | grep munk8s"
    run_cmd "kubectl get valkeyinstances -n data"
    run_cmd "kubectl get pvc -n data"
    run_cmd "kubectl exec -n data deploy/valkey -- valkey-cli DBSIZE"
    run_cmd "kubectl exec -n data deploy/valkey -- valkey-cli zrange top:planes 0 4 withscores"
    
    echo ""
    echo "[PUNTOS A MOSTRAR]:"
    echo "1. CRD 'valkeyinstances.cache.munk8s.io' administrado por el Operator."
    echo "2. Instancia Valkey desplegada y reconciliada por el Operator en namespace data."
    echo "3. Persistencia mediante PersistentVolumeClaim 'valkey-data' (Bound)."
    echo "4. Comando 'valkey-cli DBSIZE' evidenciando los registros guardados."
    pause_step
}

sec_1_7() {
    print_header "1.7" "GRAFANA"
    echo "Descripcion: Dashboard desplegado por el Operator y metricas en vivo."
    
    run_cmd "kubectl get grafanainstances -n data"
    run_cmd "kubectl get pods -n data -l app.kubernetes.io/name=grafana"
    
    echo ""
    echo "[PUNTOS A MOSTRAR ]:"
    echo "1. En navegador: http://localhost:3000/d/mumnk8s-main/m-u-m-n-k8s-monitoring-dashboard"
    echo "   Credenciales: Usuario: admin | Contraseña: munk8s-admin"
    echo "2. Grafana desplegado como Custom Resource 'GrafanaInstance' por el Operator."
    echo "3. 11 paneles activos con metricas de aviones, barcos, modas, tops y pais RUS."
    pause_step
}

sec_1_8() {
    print_header "1.8" "FALCO"
    echo "Descripcion: Seguridad en tiempo real y deteccion de intrusiones con Falco."
    
    run_cmd "kubectl get pods -n falco"
    
    echo ""
    echo "[SIMULACION DE AMENAZA EN VIVO]:"
    echo "Ejecutando lectura no autorizada de archivo sensible (/app/secretos y /etc/shadow) dentro de un pod..."
    kubectl exec -n data deploy/valkey -- sh -c "mkdir -p /app && echo 'datos_militares_confidenciales_2026' > /app/secretos && cat /app/secretos" > /dev/null 2>&1 || true
    kubectl exec -n data deploy/valkey -- cat /etc/shadow > /dev/null 2>&1 || true
    
    echo "Consultando logs de Falco para verificar la intercepcion de la alerta..."
    run_cmd "kubectl logs -n falco -l app.kubernetes.io/name=falco -c falco --tail=10 | grep -E 'ALERTA_SEGURIDAD_FALCO|Sensitive file'"
    
    echo ""
    echo "[PUNTOS A MOSTRAR]:"
    echo "1. DaemonSet de Falco desplegado y corriendo en el namespace 'falco' usando driver modern_ebpf."
    echo "2. Regla personalizada cargada para detectar intentos de lectura de /etc/shadow y /app/secretos."
    echo "3. Alerta interceptada y mostrada en tiempo real en la salida de los logs de Falco."
    pause_step
}

sec_2_1() {
    print_header "2.1" "PRUEBAS DE CARGA, HPA Y ANALISIS DE REPLICAS"
    echo "Descripcion: Resultados comparativos 1 replica vs 2 replicas documentados."
    
    run_cmd "cat docs/bench/summary.md"
    
    echo ""
    echo "[PUNTOS A MOSTRAR]:"
    echo "1. Tabla comparativa de pruebas de rendimiento realizadas con Locust (20, 50, 100 usuarios)."
    echo "2. Metricas documentadas: throughput (req/s), latencias p50, p95 y p99 con 0 fallos."
    echo "3. Analisis tecnico: comportamiento del cuello de botella en entorno local de recursos compartidos."
    pause_step
}

sec_2_2() {
    print_header "2.2" "MANUAL TECNICO Y GUIA DE INSTALACION"
    echo "Descripcion: Documentacion tecnica completa del proyecto."
    
    run_cmd "ls -lh Documentacion/ManualTecnico.md"
    run_cmd "head -n 25 Documentacion/ManualTecnico.md"
    
    echo ""
    echo "[PUNTOS A MOSTRAR]:"
    echo "1. Archivo Documentacion/ManualTecnico.md en formato Markdown con todas las secciones tecnicas."
    echo "2. Detalle de arquitectura, Gateway API, gRPC, RabbitMQ, Valkey, Operator SDK, Falco y guias de ejecucion."
    pause_step
}

# ------------------------------------------------------------------------------
# Logica de ejecucion segun argumentos
# ------------------------------------------------------------------------------

AUTO_MODE="false"

case "$1" in
    "--all")
        AUTO_MODE="true"
        sec_1_1
        sec_1_2
        sec_1_3
        sec_1_4
        sec_1_5
        sec_1_6
        sec_1_7
        sec_1_8
        sec_2_1
        sec_2_2
        ;;
    "1.1") sec_1_1 ;;
    "1.2") sec_1_2 ;;
    "1.3") sec_1_3 ;;
    "1.4") sec_1_4 ;;
    "1.5") sec_1_5 ;;
    "1.6") sec_1_6 ;;
    "1.7") sec_1_7 ;;
    "1.8") sec_1_8 ;;
    "2.1") sec_2_1 ;;
    "2.2") sec_2_2 ;;
    *)
        PAUSE_MODE="true"
        echo "$SEPARATOR"
        echo "EJECUTANDO PRESENTACION COMPLETA PASO A PASO (GUIA V2)"
        echo "$SEPARATOR"
        echo "Para ejecutar una seccion especifica, use: ./calificacion_v2.sh <numero>"
        echo "Ejemplo: ./calificacion_v2.sh 1.1"
        echo "Para ejecutar todo sin pausas: ./calificacion_v2.sh --all"
        echo "$SEPARATOR"
        
        sec_1_1
        sec_1_2
        sec_1_3
        sec_1_4
        sec_1_5
        sec_1_6
        sec_1_7
        sec_1_8
        sec_2_1
        sec_2_2
        
        echo ""
        echo "$SEPARATOR"
        echo "PRESENTACION FINALIZADA SATISFACTORIAMENTE"
        echo "$SEPARATOR"
        ;;
esac
