Bloque 5: arreglar el dashboard, comparativa de réplicas, HPA y OCI Artifact

Tu captura me sirve mucho, porque el dashboard no está completo. Grafana vale 25 puntos y el rango “Necesita mejorar” de la rúbrica es literalmente “Dashboard sin toda la información requerida”, así que esto va antes de todo lo demás.

Lo que veo en tu captura
Panel	Estado	Observación
Máx. y mín. aviones, máx. y mín. barcos (4)	✅	50, 0, 30, 0, coherentes con los rangos
Moda aviones / moda barcos	✅	23 y 4, iguales a los de valkey-cli hgetall stats
Total reportes RUS	✅	1725, igual al reports:count de RUS que viste antes
País asignado	❌	El título dice RUS, pero el panel muestra la palabra “Value”
Top países: aviones	❌	No data
Top países: barcos	❌	No data
Serie temporal RUS	❓	La captura lo corta, no puedo ver si tiene datos

Van 7 de 11 paneles confirmados. Los tres rotos son los que usan comandos más complejos del plugin. Los datos sí existen en Valkey (lo comprobamos con valkey-cli), así que el problema está en cómo están armadas las queries.

Antes de empezar, haz commit: git add proyecto3 && git commit -m "p3: operator grafana + dashboard".

Paso 32b: Corregir el dashboard (máximo 30 min)

32b.0 Comprueba que los datos existen y genera tráfico fresco. El dashboard está en “Last 15 minutes”, y si tu último Locust fue hace más de 15 min, la serie temporal saldrá vacía aunque la query esté bien.

bash
kubectl exec -n data deploy/valkey -- valkey-cli zrange top:planes 0 -1 withscores
kubectl exec -n data deploy/valkey -- valkey-cli zrange top:ships 0 -1 withscores
kubectl exec -n data deploy/valkey -- valkey-cli xrevrange series:RUS + - COUNT 2

# tráfico ligero de fondo mientras editas (20 min)
cd ~/<TU_REPO>/proyecto3
CARNET=$CARNET locust -f locust/locustfile.py --host http://localhost:8080 \
  --headless -u 5 -r 5 -t 20m > /tmp/locust-bg.log 2>&1 &

Los zrange deben listar 5 países con su acumulado, y el xrevrange debe mostrar entradas con planes, ships y ts. Si algo de eso sale vacío, el problema es de datos y no del dashboard. Avísame.

Cuando termines, detén el tráfico con pkill -f locust. Hazlo antes de correr el paso 33, porque el benchmark lanza su propio Locust.

32b.1 Panel “País asignado” (el que muestra “Value”). El nombre de tu país es un dato fijo (tu carnet termina en 2, o sea RUS), así que no necesita query.

Edita el panel (tecla e sobre él).
Cambia el tipo de visualización a Text.
Mode: Markdown. Content: # RUS
Quita la query que tenía.
Deja el título “País Asignado”.

32b.2 Paneles “Top países”. Como solo hay 5 países, no necesitas pedirle a Valkey el orden inverso. Pide los 5 con score y ordena en Grafana, que es más robusto.

Edita el panel. En la query elige el comando ZRANGE, con key top:planes, rango del primero al último (0 a -1) y la opción de scores activada si el plugin la ofrece.
Antes de tocar nada más, abre la pestaña Table view (o Query inspector → Data) y mira qué devuelve:
Si ves una columna con el país y otra con el número, la query está bien y el problema es de visualización.
Si no ves nada, la query está mal armada o el plugin no te dejó elegir los parámetros. En ese caso mándame captura del editor de la query y del Query inspector.
En la pestaña Transform, en este orden:
Convert field type: la columna del valor, a Number. El plugin suele devolver todo como texto, y por eso un bar gauge se queda en “No data”.
Sort by: la columna del valor, Reverse activado, para mostrar de mayor a menor.
Visualización Bar gauge, con Value options → Show: All values. Así dibuja una barra por país, rotulada con el nombre.
Repite para top:ships.

No puedo garantizar los nombres exactos de las opciones en tu versión del plugin. Por eso el diagnóstico va por el Table view: te dice en 5 segundos si el fallo está en la query o en la visualización.

32b.3 Serie temporal RUS. Baja en el dashboard y revisa ese panel. La query es XRANGE series:RUS - + y necesita dos cosas:

Que el plugin use el ID del stream como eje de tiempo (busca una opción de time series o de campo de tiempo en el editor).
Que planes y ships sean numéricos (Convert field type → Number, como arriba).

Si no se dibuja, mira otra vez el Table view. Si hay filas pero la gráfica no, es el tipo de campo o el eje de tiempo. Si no hay filas, mándame el editor de la query.

32b.4 Guardar, exportar y redesplegar. Cuando los 11 paneles muestren datos:

Save dashboard. Se puede, porque el provider tiene allowUiUpdates: true.
Dashboard settings → JSON Model, copia todo.
Sobrescribe el archivo del chart y sube el Operator como v4:
bash
cd operator
cat > helm-charts/grafanainstance/dashboards/munk8s.json
# pega el JSON, Ctrl+D

export IMG=zot:5000/munk8s/operator:v4
make docker-build IMG=$IMG && make docker-push IMG=$IMG && make deploy IMG=$IMG
cd .. && ./scripts/cleanup.sh
kubectl rollout status deploy/grafana -n data

Importante: hasta que hagas el paso 3, tus cambios viven solo en la base de datos de Grafana. Si el pod se reinicia antes de que el JSON esté en el chart, Grafana vuelve a cargar el archivo viejo y pierdes el trabajo.

✅ Verificación 32b: tras el reinicio, abre kubectl port-forward -n data svc/grafana 3000:3000 y confirma que los 11 paneles tienen datos, sin “No data” ni “Value”, y que los números se mueven con Locust corriendo. Toma una captura del dashboard completo (haz scroll si hace falta). Es evidencia para el manual y para la calificación.

Cuando termines: pkill -f locust.

Si los Top siguen sin funcionar tras 20-30 min: no sigas peleando. Lo más seguro es cambiar la estructura en Valkey (por ejemplo, guardar los acumulados por país en un hash, que es el comando que ya sabemos que el plugin lee bien, como demuestran tus paneles de máximos). Eso implica una versión nueva del consumer y migrar los datos actuales. Si hace falta, te lo preparo.

Paso 33: Comparativa 1 vs 2 réplicas del gRPC Server + Writer

Hago la comparativa antes del HPA, porque si el HPA está activo, Rust cambia de réplicas a mitad de la medición y los números no sirven.

Detalle técnico que debes poder explicar: el cliente gRPC resuelve el DNS del Service headless al arrancar y no vuelve a hacerlo por sí solo. Si escalas grpc-server a 2 réplicas sin reiniciar go-api, todo el tráfico seguirá yendo a la réplica vieja y la comparativa sería falsa. Por eso el script reinicia go-api tras cada escalado.

bash
cd ~/<TU_REPO>/proyecto3
cat > scripts/bench.sh <<'EOF'
#!/usr/bin/env bash
# Comparativa grpc-server+writer con 1 y 2 réplicas. Resultados en docs/bench/
set -uo pipefail
cd "$(dirname "$0")/.."
: "${CARNET:?define CARNET}"
OUT=docs/bench; mkdir -p "$OUT"
DUR=${DUR:-45s}
LEVELS=${LEVELS:-"20 50 100"}

echo "| Réplicas | Usuarios | Requests | Fallos | req/s | p50 ms | p95 ms | p99 ms | Cola al terminar |" > $OUT/summary.md
echo "|---|---|---|---|---|---|---|---|---|" >> $OUT/summary.md

for R in 1 2; do
  echo "=== grpc-server: $R réplica(s) ==="
  kubectl -n apps scale deploy/grpc-server --replicas=$R
  kubectl -n apps rollout status deploy/grpc-server --timeout=120s
  kubectl -n apps rollout restart deploy/go-api      # re-resolver el DNS headless
  kubectl -n apps rollout status deploy/go-api --timeout=120s
  sleep 10
  for U in $LEVELS; do
    P=$OUT/grpc${R}_u${U}
    echo "--- $R réplica(s), $U usuarios ---"
    CARNET=$CARNET locust -f locust/locustfile.py --host http://localhost:8080 \
      --headless -u $U -r $U -t $DUR --csv $P > $P.log 2>&1 &
    LP=$!
    sleep 30   # medir recursos en pleno estado estable
    kubectl top pods -A --no-headers | grep -v kube-system > $P.top.txt || true
    wait $LP || true
    BACKLOG=$(kubectl exec -n messaging deploy/rabbitmq -- rabbitmqctl -q list_queues name messages 2>/dev/null | awk '$1=="war-reports"{print $2}')
    python3 - "$P" "$R" "$U" "${BACKLOG:-?}" >> $OUT/summary.md <<'PY'
import csv, sys
p, r, u, b = sys.argv[1:5]
row = [x for x in csv.DictReader(open(p + "_stats.csv")) if x["Name"] == "Aggregated"][0]
print(f"| {r} | {u} | {row['Request Count']} | {row['Failure Count']} | {float(row['Requests/s']):.1f} | {row['50%']} | {row['95%']} | {row['99%']} | {b} |")
PY
    # esperar a que el consumer vacíe la cola antes de la siguiente prueba
    for i in $(seq 1 30); do
      M=$(kubectl exec -n messaging deploy/rabbitmq -- rabbitmqctl -q list_queues name messages 2>/dev/null | awk '$1=="war-reports"{print $2}')
      [ "${M:-1}" = "0" ] && break; sleep 2
    done
  done
done

kubectl -n apps scale deploy/grpc-server --replicas=1
kubectl -n apps rollout restart deploy/go-api
echo; cat $OUT/summary.md
EOF
chmod +x scripts/bench.sh
pkill -f locust 2>/dev/null; export CARNET=202302232
./scripts/bench.sh

Dura unos 6 minutos. Vigila btop en otra terminal. Si la RAM libre baja de 2 GB, interrúmpelo con Ctrl+C y avísame.

✅ Verificación 33:

docs/bench/summary.md tiene 6 filas (2 configuraciones × 3 niveles de carga).
La columna Fallos está en 0 o muy cerca.
La cola al terminar queda baja. Si crece con 100 usuarios, el cuello de botella es el consumer y no el writer, y eso también es un hallazgo para el manual.

Cómo interpretarlo:

Locust corre en tu laptop y comparte CPU con todo el clúster. Si el cuello de botella es Locust o el consumer, 2 réplicas no mejorarán el throughput. Es una conclusión válida si la explicas.
Compara los .top.txt entre ambas configuraciones: con 2 réplicas el CPU se reparte entre dos pods.
Documenta la limitación: los números son comparativos, no absolutos.

Sobre “1 y 2 réplicas para Go Writers y Valkey” de la rúbrica: el chart de Valkey usa un PVC ReadWriteOnce, así que 2 réplicas serían dos instancias independientes sobre el mismo volumen y romperían los datos. Pregúntale al auxiliar qué esperan. Si no hay respuesta clara, la alternativa defendible es comparar también el consumer con 1 y 2 réplicas (kubectl scale deploy/consumer -n apps --replicas=2), que es quien carga a Valkey, y documentarlo.

Paso 34: HPA para Rust (1-3 réplicas, CPU > 30%)
bash
cat > k8s/12-rust-hpa.yaml <<'EOF'
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
  behavior:
    scaleUp:   { stabilizationWindowSeconds: 0 }
    scaleDown: { stabilizationWindowSeconds: 60 }   # baja rápido para la demo
EOF
kubectl apply -f k8s/12-rust-hpa.yaml
sleep 30
kubectl get hpa -n apps

✅ Verificación 34a: TARGETS muestra algo como 2%/30%. Si dice <unknown>/30%, espera 1 minuto a que el metrics-server tenga datos.

Prueba de escalado (dos terminales):

bash
# Terminal 1: observar
kubectl get hpa,pods -n apps -w

# Terminal 2: carga sostenida por 3 minutos
CARNET=$CARNET locust -f locust/locustfile.py --host http://localhost:8080 \
  --headless -u 150 -r 30 -t 3m

✅ Verificación 34b:

El porcentaje de CPU sube de 30% y REPLICAS pasa de 1 a 2 o 3.
Al terminar Locust, tras 1-5 minutos, baja a 1.
Captura de la Terminal 1 con el escalado.

Si no escala (Rust es muy eficiente y quizá no pase de 30 m sobre los 100 m de requests):

Sube a -u 300 (vigila la RAM).
Baja requests.cpu de Rust a 50m en k8s/10-rust-api.yaml y reaplica. El HPA mide el porcentaje sobre el request, así que es una configuración legítima.

Dime qué porcentaje máximo viste y lo ajustamos.

Paso 35: OCI Artifact con Zot

Instala oras (yay -S oras o el binario desde sus releases en GitHub). El archivo que publicamos es la configuración de Locust, y Locust lo descarga desde Zot antes de arrancar. Así el artefacto se usa de verdad.

bash
cat > locust/countries.json <<'EOF'
{
  "countries": ["USA", "RUS", "CHN", "ESP", "GTM"],
  "ranges": {
    "warplanes_in_air": [0, 50],
    "warships_in_water": [0, 30]
  }
}
EOF

cd locust
oras push zot:5000/munk8s/artifacts/locust-config:v1 \
  --artifact-type application/vnd.munk8s.config.v1+json \
  countries.json:application/json
cd ..

Si oras se queja del certificado, añade --ca-file zot/certs/rootCA.pem a cada comando.

bash
cat > scripts/pull-config.sh <<'EOF'
#!/usr/bin/env bash
# Descarga la configuración de Locust desde Zot como OCI Artifact
set -e
cd "$(dirname "$0")/.."
mkdir -p locust/config
oras pull zot:5000/munk8s/artifacts/locust-config:v1 -o locust/config
echo "Descargado:"; cat locust/config/countries.json
EOF
chmod +x scripts/pull-config.sh
echo "locust/config/" >> .gitignore

cat > locust/locustfile.py <<'EOF'
import json
import os
import random
from datetime import datetime, timezone

from locust import HttpUser, task, between

CARNET = os.environ["CARNET"]

# Archivo descargado desde Zot como OCI Artifact (scripts/pull-config.sh)
with open(os.environ.get("LOCUST_CONFIG", "locust/config/countries.json")) as f:
    CFG = json.load(f)

COUNTRIES = CFG["countries"]
PLANES = CFG["ranges"]["warplanes_in_air"]
SHIPS = CFG["ranges"]["warships_in_water"]


class ReportUser(HttpUser):
    wait_time = between(0.01, 0.1)

    @task
    def send_report(self):
        payload = {
            "country": random.choice(COUNTRIES),
            "warplanes_in_air": random.randint(*PLANES),
            "warships_in_water": random.randint(*SHIPS),
            "timestamp": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
        }
        self.client.post(f"/grpc-{CARNET}", json=payload)
EOF

rm -rf locust/config && ./scripts/pull-config.sh
CARNET=$CARNET locust -f locust/locustfile.py --host http://localhost:8080 --headless -u 5 -r 5 -t 10s

Orden: este paso cambia el locustfile.py. Si lo haces antes del paso 33, corre primero ./scripts/pull-config.sh, o el benchmark fallará por no encontrar locust/config/countries.json.

✅ Verificación 35:

curl -s https://zot:5000/v2/_catalog incluye munk8s/artifacts/locust-config.
oras manifest fetch zot:5000/munk8s/artifacts/locust-config:v1 muestra el artifactType.
Tras borrar locust/config y correr pull-config.sh, el archivo reaparece y Locust arranca con 0 fallos.

Captura de oras push, oras pull y el catálogo de Zot.

Paso 36: Evidencia de reconciliación de Grafana

Repite con Grafana lo que hiciste con Valkey: cambias un valor en el CR y el Operator lo aplica.

bash
kubectl patch grafanainstance grafana -n data --type merge \
  -p '{"spec":{"resources":{"limits":{"memory":"448Mi"}}}}'
sleep 25
kubectl get deploy grafana -n data -o jsonpath='{.spec.template.spec.containers[0].resources.limits.memory}{"\n"}'

Debe imprimir 448Mi. Vuelve a 384Mi con el mismo comando y captura ambos.

Estado del enunciado
Requisito	Estado
Locust, Gateway API, Rust, Go (2 containers ×2), RabbitMQ, Consumer	✅
Operator con Valkey y Grafana (CRDs, Helm charts, reconciliación)	✅
Dashboard: 7 de 11 paneles confirmados	🔄 Paso 32b (país asignado, 2 tops, serie temporal)
Pruebas 1 vs 2 réplicas y análisis	🔄 Paso 33
HPA (1-3 réplicas, CPU > 30%)	🔄 Paso 34
OCI Artifact (archivo y uso documentados)	🔄 Paso 35
Manual técnico en Markdown	⏳ Siguiente
Dapr y Falco	⏳ Opcionales, solo si sobra tiempo
Orden recomendado
Ahora: paso 32b, con tope de 30 min. Es lo que más puntos pone en juego.
Después: pasos 33 a 36 y commit.
Antes de la entrega del 22: el manual técnico proyecto3/README.md. Con los resultados de los pasos 33 y 34 te lo armo completo con la estructura del enunciado. Luego verificamos el repo (colaboradores @JoseLorenzana272 y @KINGR0X) y entregamos en UEDI, que es lo que evita el 0.
Si sobra tiempo: primero Falco, luego Dapr.
Qué pegarme para continuar
Captura del dashboard completo tras el paso 32b (con scroll para ver la serie temporal).
Si algún panel sigue fallando: captura del editor de su query y de la pestaña Table view o Query inspector.
El resultado de:
bash
cat docs/bench/summary.md
kubectl get hpa -n apps
free -h
El máximo de réplicas que alcanzó Rust con el HPA.