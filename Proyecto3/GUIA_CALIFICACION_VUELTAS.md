# GUIA DE EVALUACION Y DEFENSA DEL PROYECTO 3 (M.U.M.N.K8s) - VERSION 2

Datos del Estudiante:
- Carnet: 202302232
- Ultimo digito: 2
- Pais Asignado: RUS (Rusia)
- Ruta Gateway API: /grpc-202302232
- Credenciales Grafana: Usuario 'admin' | Contraseña 'munk8s-admin'
- Dashboard URL: http://localhost:3000/d/mumnk8s-main/m-u-m-n-k8s-monitoring-dashboard

Nota sobre el entorno:
Todo el sistema esta desplegado y funcionando de forma 100% local sobre Docker y k3d (Kubernetes local), Zot Registry, RabbitMQ, Valkey y Grafana.

==============================================================================
1. INDICACIONES PREVIAS Y PREPARACION ANTES DEL INGRESO
==============================================================================

Regla de la calificacion:
Todo el sistema debe estar completamente desplegado y funcionando antes de iniciar el bloque de evaluacion (5 estudiantes por bloque). No se permite compilar ni levantar servicios durante el turno.

Paso de verificacion previo (ejecutar antes de entrar):
./up.sh

Pestañas que deben estar abiertas y listas en el navegador:
- Pestaña 1 (Locust): Interfaz de carga o terminal con Locust listo para enviar reportes JSON.
- Pestaña 2 (Zot Registry): https://localhost:5000/v2/_catalog (aceptar certificado SSL autofirmado para ver el JSON).
- Pestaña 3 (Grafana): http://localhost:3000/d/mumnk8s-main/m-u-m-n-k8s-monitoring-dashboard en rango "Last 15 minutes" con auto-refresh en 5s.

Terminales preparadas:
- Terminal 1: Ubicada en la raiz del proyecto para ejecutar comandos de verificacion o el script ./calificacion_v2.sh.
- Terminal 2: Para correr la generacion de trafico activo en segundo plano:
  CARNET=202302232 locust -f locust/locustfile.py --host http://localhost:8080 --headless -u 5 -r 5 -t 30m

==============================================================================
PARTE 1: IMPLEMENTACION FUNCIONAL
==============================================================================

------------------------------------------------------------------------------
1.1 ARQUITECTURA GENERAL, DESPLIEGUE (GATEWAY API)
------------------------------------------------------------------------------
Comandos solicitados:
kubectl get nodes -o wide
kubectl get gateway,httproute -n apps

Comando de detalle complementario:
kubectl describe httproute grpc-route -n apps

Verificaciones a responder al evaluador:
1. Despliegue efectivo del cluster: Cluster local k3d con nodo control-plane en estado Ready y runtime containerd.
2. Gateway API: Objeto 'munk8s-gateway' (GatewayClass traefik) con estado Programmed=True.
3. Ruta especificada: Objeto HTTPRoute 'grpc-route' configurado para enrutar el prefijo /grpc-202302232 hacia el Service de rust-api en el puerto 8080.

------------------------------------------------------------------------------
1.2 API REST EN RUST
------------------------------------------------------------------------------
Comandos solicitados:
kubectl get hpa,deployments -n apps

Verificaciones a responder al evaluador:
1. API desarrollada en Rust compilada con Axum/Tokio para alto rendimiento.
2. Deployment 'rust-api' activo con replicas disponibles.
3. HPA 'rust-api' configurado con metrica de CPU al 30%, minimo 1 pod y maximo 3 pods.

------------------------------------------------------------------------------
1.3 SERVICIOS EN GO (API/GRPC CLIENT, GRPC SERVER/WRITERS)
------------------------------------------------------------------------------
Comandos solicitados:
kubectl logs deployment/rust-api -n apps --tail=10
kubectl logs deployment/go-api -n apps -c grpc-client --tail=10
kubectl logs deployment/grpc-server -n apps -c grpc-server --tail=10
kubectl logs deployment/grpc-server -n apps -c writer --tail=10

Verificaciones a responder al evaluador:
1. Flujo completo: Rust recibe la peticion HTTP POST via Gateway y reenvia el JSON al cliente en Go.
2. Go API (grpc-client) serializa a Protobuf y transmite por gRPC a grpc-server:50051.
3. Go gRPC Server recibe y atiende la llamada concurrente.
4. Go Writer toma los reportes y los publica de manera asincrona en la cola de RabbitMQ.

Nota tecnica sobre HTTP 405 en navegador:
Si se intenta abrir http://localhost:8080/grpc-202302232 en el navegador, este devuelve 405 Method Not Allowed porque el navegador envia una solicitud GET, mientras que el microservicio Rust esta diseñado para peticiones POST con carga util JSON.

------------------------------------------------------------------------------
1.4 ARQUITECTURA GENERAL, DESPLIEGUE EN ZOT
------------------------------------------------------------------------------
Pantalla en navegador:
https://localhost:5000/v2/_catalog

Comando de respaldo por consola:
curl -k -s https://localhost:5000/v2/_catalog | jq .

Verificaciones a responder al evaluador:
1. Zot desplegado de forma independiente en contenedor Docker con TLS en puerto 5000.
2. Contiene todas las imagenes construidas para la arquitectura:
   - munk8s/rust-api
   - munk8s/go-api
   - munk8s/go-grpc-client
   - munk8s/go-grpc-server
   - munk8s/go-writer
   - munk8s/go-consumer
   - munk8s/valkey
   - munk8s/grafana
   - munk8s/operator
   - munk8s/artifacts/locust-config (Artefacto OCI)

------------------------------------------------------------------------------
1.5 MESSAGE BROKER Y CONSUMIDOR RABBITMQ
------------------------------------------------------------------------------
Comandos solicitados:
kubectl get pods -n messaging
kubectl get pods -n apps -l app=consumer
kubectl logs deployment/consumer -n apps --tail=10

Verificaciones a responder al evaluador:
1. RabbitMQ activo en el namespace messaging como broker central de mensajeria.
2. Pod 'consumer' en namespace apps con workers concurrentes suscrito a la cola 'war-reports'.
3. Los logs muestran el procesamiento y guardado continuo hacia Valkey.

------------------------------------------------------------------------------
1.6 ALMACENAMIENTO (VALKEY)
------------------------------------------------------------------------------
Comandos solicitados:
kubectl get crd | grep munk8s
kubectl get valkeyinstances -n data
kubectl get pvc -n data
kubectl exec -n data deploy/valkey -- valkey-cli DBSIZE
kubectl exec -n data deploy/valkey -- valkey-cli zrange top:planes 0 4 withscores

Verificaciones a responder al evaluador:
1. Operator desarrollado con Operator SDK basado en Helm dentro del cluster.
2. Custom Resource Definition 'valkeyinstances.cache.munk8s.io' en uso activo.
3. Instancia 'valkey' reconciliada automaticamente por el Operator.
4. Persistencia mediante PersistentVolumeClaim 'valkey-data' (1Gi Bound).
5. Comando DBSIZE confirmando la existencia de claves acumuladas.

------------------------------------------------------------------------------
1.7 GRAFANA
------------------------------------------------------------------------------
Pantalla en navegador:
http://localhost:3000/d/mumnk8s-main/m-u-m-n-k8s-monitoring-dashboard

Comandos de respaldo por consola:
kubectl get grafanainstances -n data
kubectl get pods -n data -l app.kubernetes.io/name=grafana

Verificaciones a responder al evaluador:
1. Instancia y despliegue gestionado mediante el Custom Resource 'GrafanaInstance'.
2. Verificacion de los 11 paneles en vivo:
   - Panel 1 y 2: Maximo (50) y Minimo (0) global de aviones en el aire.
   - Panel 3 y 4: Maximo (30) y Minimo (0) global de barcos en el agua.
   - Panel 5: Moda de aviones y barcos.
   - Panel 6 y 7: Top 5 de paises con mas aviones y mas barcos acumulados.
   - Panel 8: Total de reportes del pais asignado (RUS - Rusia).
   - Panel 9 y 10: Promedios de aviones y barcos para Rusia.
   - Panel 11: Serie temporal de actividad de reportes para Rusia.

------------------------------------------------------------------------------
1.8 FALCO
------------------------------------------------------------------------------
Comandos solicitados:
kubectl get pods -n falco

Simulacion de amenaza en vivo (ejecutada desde consola):
kubectl exec -n data deploy/valkey -- sh -c "mkdir -p /app && echo 'datos_militares_confidenciales_2026' > /app/secretos && cat /app/secretos"
kubectl exec -n data deploy/valkey -- cat /etc/shadow

Verificacion de la alerta en tiempo real:
kubectl logs -n falco -l app.kubernetes.io/name=falco -c falco --tail=10 | grep -E "ALERTA_SEGURIDAD_FALCO|Sensitive file"

Verificaciones a responder al evaluador:
1. Falco instalado y operativo en el cluster utilizando Helm y el driver modern_ebpf sobre kernel de Linux.
2. Regla personalizada cargada ('Lectura de Archivo Confidencial en Pod') para auditar aperturas de /etc/shadow y /app/secretos.
3. Alerta de severidad Critical/Warning interceptada y reportada en vivo en los logs con el archivo, usuario, pod y comando ejecutado.

==============================================================================
PARTE 2: DOCUMENTACION Y ANALISIS
==============================================================================

------------------------------------------------------------------------------
2.1 PRUEBAS DE CARGA, HPA Y ANALISIS DE REPLICAS
------------------------------------------------------------------------------
Comando para mostrar la tabla resumen:
cat docs/bench/summary.md

Tabla de resultados comparativos (1 replica vs 2 replicas):
---------------------------------------------------------------------------------------------------
Replicas | Usuarios | Requests | Fallos | req/s | p50 ms | p95 ms | p99 ms | Cola al terminar
1        | 20       | 12691    | 0      | 287.5 | 11     | 22     | 36     | 0
1        | 50       | 18114    | 0      | 415.2 | 53     | 77     | 100    | 0
1        | 100      | 20197    | 0      | 454.7 | 140    | 170    | 190    | 0
2        | 20       | 12821    | 0      | 290.6 | 11     | 21     | 29     | 0
2        | 50       | 17513    | 0      | 401.5 | 56     | 74     | 88     | 0
2        | 100      | 19483    | 0      | 437.3 | 140    | 180    | 210    | 0
---------------------------------------------------------------------------------------------------

Analisis tecnico a responder:
- Con 1 replica vs 2 replicas se reducen los percentiles altos de latencia (p95 y p99) con cargas moderadas.
- En un entorno local de prueba, el throughput maximo se estabiliza cerca de 450 req/s debido a que tanto el generador Locust como los microservicios y bases de datos comparten los mismos nucleos de CPU fisica del host.

------------------------------------------------------------------------------
2.2 MANUAL TECNICO Y GUIA DE INSTALACION
------------------------------------------------------------------------------
Ubicacion del documento:
Documentacion/ManualTecnico.md

Contenido para mostrar:
- Diagrama de arquitectura general.
- Especificacion de Gateway API y HTTPRoutes.
- Implementacion de servicios gRPC y REST.
- Configuracion del broker RabbitMQ y base de datos Valkey.
- Operator SDK y automatizacion de Custom Resources.
- Seccion 18: Regla personalizada de Falco y captura de logs de alerta.
- Guia paso a paso de instalacion y ejecucion.

==============================================================================
PARTE 4: EXTRA (DAPR)
==============================================================================
- Flujo alternativo con Dapr (/dapr-#carnet): Componente opcional de la rubrica.

==============================================================================
SCRIPT AUTOMATIZADO DE PRESENTACION
==============================================================================

Para ejecutar los comandos paso a paso frente al evaluador:
./calificacion_v2.sh

Para ejecutar directamente una seccion especifica:
./calificacion_v2.sh 1.1
./calificacion_v2.sh 1.2
./calificacion_v2.sh 1.3
./calificacion_v2.sh 1.4
./calificacion_v2.sh 1.5
./calificacion_v2.sh 1.6
./calificacion_v2.sh 1.7
./calificacion_v2.sh 2.1
./calificacion_v2.sh 2.2

Para ejecutar todo de forma continua sin pausas:
./calificacion_v2.sh --all
