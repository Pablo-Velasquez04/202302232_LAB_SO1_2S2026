Bloque 3: Operator de Valkey y consumer conectado

Tu resumen está bien: cola en 0 con 1 consumer, 295 req/s sin fallos y 6.9 GB disponibles. Haz commit antes de seguir (git add proyecto3 && git commit -m "p3: rabbitmq, writer, consumer").

Este bloque cubre lo que más puntos da (Valkey con Operator, 20 pts) y deja el consumer escribiendo en Valkey. Grafana (25 pts) va en el siguiente. Hay un cambio respecto al plan anterior: dije que demostraras la reconciliación editando storage, pero local-path no permite redimensionar PVC, así que demostraremos con maxmemory.

Paso 19: Imagen de Valkey a Zot
bash
cd ~/<TU_REPO>/proyecto3
docker pull valkey/valkey:8-alpine
docker tag valkey/valkey:8-alpine zot:5000/munk8s/valkey:8
docker push zot:5000/munk8s/valkey:8
<>
✅ Verificación 19: curl -s https://zot:5000/v2/_catalog incluye munk8s/valkey.

Paso 20: Esqueleto del Operator (Operator SDK, plugin Helm)
bash
mkdir operator && cd operator
operator-sdk version
operator-sdk init --plugins helm --domain munk8s.io --project-name munk8s-operator
operator-sdk create api --group cache --version v1alpha1 --kind ValkeyInstance

Creamos el API sin --helm-chart para que el SDK genere helm-charts/valkeyinstance/, que es el nombre que sugiere el enunciado. Después reemplazamos su contenido.

✅ Verificación 20:

bash
ls helm-charts/valkeyinstance/
cat watches.yaml

watches.yaml debe mencionar cache.munk8s.io, ValkeyInstance y helm-charts/valkeyinstance.

Paso 21: Helm chart de Valkey

Borramos el chart de ejemplo (nginx) y escribimos el nuestro. El nombre del Service será el nombre del CR, para que el consumer lo encuentre siempre en valkey.data.svc.cluster.local.

bash
cd helm-charts/valkeyinstance
rm -rf templates/* values.yaml

cat > values.yaml <<'EOF'
image:
  repository: zot:5000/munk8s/valkey
  tag: "8"
  pullPolicy: IfNotPresent
maxmemory: 100mb
persistence:
  enabled: true
  size: 1Gi
  storageClass: ""
resources:
  requests: { cpu: 50m, memory: 64Mi }
  limits:   { cpu: 300m, memory: 256Mi }
EOF

cat > templates/pvc.yaml <<'EOF'
{{- if .Values.persistence.enabled }}
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: {{ .Release.Name }}-data
  labels:
    app.kubernetes.io/name: valkey
    app.kubernetes.io/instance: {{ .Release.Name }}
spec:
  accessModes: ["ReadWriteOnce"]
  {{- if .Values.persistence.storageClass }}
  storageClassName: {{ .Values.persistence.storageClass | quote }}
  {{- end }}
  resources:
    requests:
      storage: {{ .Values.persistence.size }}
{{- end }}
EOF

cat > templates/service.yaml <<'EOF'
apiVersion: v1
kind: Service
metadata:
  name: {{ .Release.Name }}
  labels:
    app.kubernetes.io/name: valkey
    app.kubernetes.io/instance: {{ .Release.Name }}
spec:
  selector:
    app.kubernetes.io/name: valkey
    app.kubernetes.io/instance: {{ .Release.Name }}
  ports:
  - name: valkey
    port: 6379
    targetPort: 6379
EOF

cat > templates/deployment.yaml <<'EOF'
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ .Release.Name }}
  labels:
    app.kubernetes.io/name: valkey
    app.kubernetes.io/instance: {{ .Release.Name }}
spec:
  replicas: 1
  strategy:
    type: Recreate
  selector:
    matchLabels:
      app.kubernetes.io/name: valkey
      app.kubernetes.io/instance: {{ .Release.Name }}
  template:
    metadata:
      labels:
        app.kubernetes.io/name: valkey
        app.kubernetes.io/instance: {{ .Release.Name }}
    spec:
      containers:
      - name: valkey
        image: "{{ .Values.image.repository }}:{{ .Values.image.tag }}"
        imagePullPolicy: {{ .Values.image.pullPolicy }}
        args:
        - "--appendonly"
        - "yes"
        - "--appendfsync"
        - "everysec"
        - "--save"
        - ""
        - "--maxmemory"
        - "{{ .Values.maxmemory }}"
        - "--maxmemory-policy"
        - "volatile-lru"
        - "--dir"
        - "/data"
        ports:
        - containerPort: 6379
        readinessProbe:
          exec:
            command: ["valkey-cli", "ping"]
          initialDelaySeconds: 3
          periodSeconds: 5
        resources:
          {{- toYaml .Values.resources | nindent 10 }}
        volumeMounts:
        - name: data
          mountPath: /data
      volumes:
      - name: data
        {{- if .Values.persistence.enabled }}
        persistentVolumeClaim:
          claimName: {{ .Release.Name }}-data
        {{- else }}
        emptyDir: {}
        {{- end }}
EOF
cd ../..

Decisiones de diseño que debes poder explicar en la calificación:

AOF (appendonly yes) más el PVC dan persistencia real: los datos sobreviven si el pod muere.
volatile-lru: si Valkey llega a maxmemory, solo expulsa claves con TTL (los streams de datos crudos) y nunca los agregados del dashboard. Esa es la política de expiración que recomienda el enunciado.
Recreate: el PVC es ReadWriteOnce, así que no pueden convivir dos pods.

Prueba que el chart renderiza, si tienes helm (sudo pacman -S helm):

bash
helm template prueba helm-charts/valkeyinstance | head -40

✅ Verificación 21: helm template imprime PVC, Service y Deployment sin errores. Si no tienes helm, sigue, porque el Operator lo validará en el paso 24.

Paso 22: Permisos RBAC para lo que crea el chart

El role.yaml generado cubre el CR y los Secrets de Helm, pero no los Deployments, Services ni PVC que crea el chart. Sin esto, el CR queda en error de permisos. Para no pelear con la indentación del archivo generado, añadimos un ClusterRole aparte:

bash
cat > config/rbac/chart_role.yaml <<'EOF'
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: chart-resources-role
rules:
- apiGroups: [""]
  resources: ["services", "persistentvolumeclaims", "configmaps", "secrets", "serviceaccounts"]
  verbs: ["create", "delete", "get", "list", "patch", "update", "watch"]
- apiGroups: ["apps"]
  resources: ["deployments", "statefulsets"]
  verbs: ["create", "delete", "get", "list", "patch", "update", "watch"]
EOF

cat config/rbac/role_binding.yaml     # mira el bloque "subjects" de este archivo

El role_binding.yaml generado trae un ServiceAccount llamado controller-manager con namespace system. Copiamos exactamente ese formato:

bash
cat > config/rbac/chart_role_binding.yaml <<'EOF'
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: chart-resources-rolebinding
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: chart-resources-role
subjects:
- kind: ServiceAccount
  name: controller-manager
  namespace: system
EOF

sed -i '/^resources:/a - chart_role.yaml\n- chart_role_binding.yaml' config/rbac/kustomization.yaml
cat config/rbac/kustomization.yaml

✅ Verificación 22: en kustomization.yaml, la lista resources: incluye chart_role.yaml y chart_role_binding.yaml con la misma indentación que las demás. Si el subjects de tu role_binding.yaml difiere del mío, usa el tuyo.

Paso 23: Compilar, publicar y desplegar el Operator en el clúster
bash
export IMG=zot:5000/munk8s/operator:v1
make docker-build IMG=$IMG
make docker-push IMG=$IMG
make deploy IMG=$IMG
kubectl get pods -n munk8s-operator-system -w

docker-build baja la imagen base helm-operator de quay.io, así que necesita internet. make deploy descarga kustomize la primera vez. Después limpia el caché:

bash
cd .. && ./scripts/cleanup.sh && cd operator

✅ Verificación 23:

bash
kubectl get crd | grep munk8s
kubectl get deploy -n munk8s-operator-system

Debe aparecer el CRD valkeyinstances.cache.munk8s.io y el manager en 1/1. Si queda en ImagePullBackOff, revisa que la imagen exista en el catálogo de Zot y manda kubectl describe pod -n munk8s-operator-system.

Paso 24: Crear el Custom Resource y validar la reconciliación
bash
cat > ../k8s/50-valkey-cr.yaml <<'EOF'
apiVersion: cache.munk8s.io/v1alpha1
kind: ValkeyInstance
metadata:
  name: valkey
  namespace: data
spec:
  maxmemory: 100mb
  persistence:
    size: 1Gi
EOF
kubectl apply -f ../k8s/50-valkey-cr.yaml
kubectl get deploy,svc,pvc,pods -n data
kubectl logs -n munk8s-operator-system deploy/munk8s-operator-controller-manager --tail=20

24.1 Creación: debe aparecer el Deployment valkey (1/1), el Service valkey, el PVC valkey-data (Bound) y el pod Running.

bash
kubectl exec -n data deploy/valkey -- valkey-cli ping          # PONG
kubectl exec -n data deploy/valkey -- valkey-cli config get maxmemory   # ~104857600

24.2 Actualización: cambia maxmemory en el CR y comprueba que el Operator lo aplica.

bash
kubectl patch valkeyinstance valkey -n data --type merge -p '{"spec":{"maxmemory":"150mb"}}'
sleep 20
kubectl exec -n data deploy/valkey -- valkey-cli config get maxmemory   # ~157286400

El Deployment hace un rollout porque cambió el argumento.

24.3 Auto-reparación: borra el Deployment a mano y observa que el Operator lo recrea.

bash
kubectl delete deploy valkey -n data
sleep 20 && kubectl get deploy -n data

24.4 Eliminación: esto lo harás al final, para la demo y la captura. No lo hagas ahora, porque borra los datos. Con kubectl delete valkeyinstance valkey -n data desaparecen Deployment, Service y PVC. Luego vuelve a aplicar el CR.

✅ Verificación 24: pasan 24.1, 24.2 y 24.3. Toma capturas de cada una para el manual.

Paso 25: El consumer guarda en Valkey

Cada reporte actualiza varias claves de forma atómica con un script Lua. Con 4 workers concurrentes, un “leer, comparar, escribir” desde Go tendría condiciones de carrera.

Diseño de datos:

Clave	Tipo	Contenido	Panel
stats	hash	max_planes, min_planes, max_ships, min_ships, mode_planes, mode_ships	Máx/mín/moda
freq:planes, freq:ships	zset	frecuencia de cada valor (base de la moda)	Moda
top:planes, top:ships	zset	acumulado por país	Top países
reports:count	hash	país → total de reportes	Total del país
series:<PAÍS>	stream	planes, ships, ts	Serie temporal

Los streams llevan MAXLEN ~ 5000 y TTL de 24 h. Los agregados no llevan TTL.

bash
cd ../go-services
go get github.com/redis/go-redis/v9@v9.7.0

cat > cmd/consumer/main.go <<'EOF'
package main

import (
	"context"
	"encoding/json"
	"log"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/redis/go-redis/v9"
)

type Report struct {
	Country         string `json:"country"`
	WarplanesInAir  int32  `json:"warplanes_in_air"`
	WarshipsInWater int32  `json:"warships_in_water"`
	Timestamp       string `json:"timestamp"`
}

func getenv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

// Todo el update de un reporte es atómico (Valkey ejecuta el script sin intercalar otros comandos)
const luaScript = `
local p = tonumber(ARGV[1])
local s = tonumber(ARGV[2])
local c = ARGV[3]
local function upd(field, v, isMax)
  local cur = redis.call('HGET', KEYS[1], field)
  if (not cur) or (isMax and v > tonumber(cur)) or ((not isMax) and v < tonumber(cur)) then
    redis.call('HSET', KEYS[1], field, v)
  end
end
upd('max_planes', p, true)
upd('min_planes', p, false)
upd('max_ships', s, true)
upd('min_ships', s, false)
redis.call('ZINCRBY', KEYS[2], 1, p)
redis.call('ZINCRBY', KEYS[3], 1, s)
redis.call('HSET', KEYS[1], 'mode_planes', redis.call('ZREVRANGE', KEYS[2], 0, 0)[1])
redis.call('HSET', KEYS[1], 'mode_ships', redis.call('ZREVRANGE', KEYS[3], 0, 0)[1])
redis.call('ZINCRBY', KEYS[4], p, c)
redis.call('ZINCRBY', KEYS[5], s, c)
redis.call('HINCRBY', KEYS[6], c, 1)
redis.call('XADD', KEYS[7], 'MAXLEN', '~', 5000, '*', 'planes', p, 'ships', s, 'ts', ARGV[4])
redis.call('EXPIRE', KEYS[7], 86400)
return 1
`

var (
	script = redis.NewScript(luaScript)
	rdb    *redis.Client
	total  atomic.Int64
)

func process(r Report) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := script.Run(ctx, rdb,
		[]string{"stats", "freq:planes", "freq:ships", "top:planes", "top:ships", "reports:count", "series:" + r.Country},
		r.WarplanesInAir, r.WarshipsInWater, r.Country, r.Timestamp).Err()
	if err != nil {
		log.Printf("valkey: %v", err)
		time.Sleep(200 * time.Millisecond) // evita un bucle de reintentos a toda velocidad
		return err
	}
	if n := total.Add(1); n%500 == 1 {
		log.Printf("guardado #%d pais=%s aviones=%d barcos=%d", n, r.Country, r.WarplanesInAir, r.WarshipsInWater)
	}
	return nil
}

func main() {
	rdb = redis.NewClient(&redis.Options{
		Addr:     getenv("VALKEY_ADDR", "valkey.data.svc.cluster.local:6379"),
		PoolSize: 16,
	})

	queue := getenv("QUEUE", "war-reports")
	u := url.URL{
		Scheme: "amqp",
		User:   url.UserPassword(getenv("RABBIT_USER", "guest"), getenv("RABBIT_PASS", "guest")),
		Host:   getenv("RABBIT_HOST", "rabbitmq.messaging.svc.cluster.local:5672"),
		Path:   "/",
	}
	var conn *amqp.Connection
	var err error
	for i := 0; i < 30; i++ {
		if conn, err = amqp.Dial(u.String()); err == nil {
			break
		}
		log.Printf("esperando RabbitMQ (%d/30): %v", i+1, err)
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		log.Fatal(err)
	}
	ch, err := conn.Channel()
	if err != nil {
		log.Fatal(err)
	}
	if _, err = ch.QueueDeclare(queue, true, false, false, false, nil); err != nil {
		log.Fatal(err)
	}
	if err = ch.Qos(50, 0, false); err != nil {
		log.Fatal(err)
	}
	msgs, err := ch.Consume(queue, "", false, false, false, false, nil)
	if err != nil {
		log.Fatal(err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for d := range msgs {
				var r Report
				if err := json.Unmarshal(d.Body, &r); err != nil {
					d.Nack(false, false) // mensaje corrupto: se descarta
					continue
				}
				if err := process(r); err != nil {
					d.Nack(false, true) // Valkey falló: se devuelve a la cola
					continue
				}
				d.Ack(false)
			}
		}()
	}
	log.Printf("consumer leyendo %q -> Valkey, 4 workers", queue)
	wg.Wait()
	log.Fatal("canal de consumo cerrado")
}
EOF

go mod tidy
go build ./... && echo "COMPILA OK"
cd .. && ./scripts/build-go.sh v3 consumer && ./scripts/cleanup.sh

Actualiza el Deployment del consumer (v3 y la variable de Valkey):

bash
cat > k8s/40-consumer.yaml <<'EOF'
apiVersion: apps/v1
kind: Deployment
metadata: { name: consumer, namespace: apps }
spec:
  replicas: 1
  selector: { matchLabels: { app: consumer } }
  template:
    metadata: { labels: { app: consumer } }
    spec:
      containers:
      - name: consumer
        image: zot:5000/munk8s/go-consumer:v3
        env:
        - { name: GOMEMLIMIT, value: "60MiB" }
        - { name: RABBIT_HOST, value: "rabbitmq.messaging.svc.cluster.local:5672" }
        - { name: VALKEY_ADDR, value: "valkey.data.svc.cluster.local:6379" }
        - name: RABBIT_USER
          valueFrom: { secretKeyRef: { name: rabbit-cred, key: user } }
        - name: RABBIT_PASS
          valueFrom: { secretKeyRef: { name: rabbit-cred, key: pass } }
        resources:
          requests: { cpu: 50m, memory: 32Mi }
          limits:   { cpu: 300m, memory: 96Mi }
EOF
kubectl apply -f k8s/40-consumer.yaml
kubectl rollout status deploy/consumer -n apps
kubectl logs -n apps deploy/consumer --tail=5

✅ Verificación 25: el log dice “consumer leyendo ... -> Valkey” y no hay errores valkey:.

Paso 26: Prueba de extremo a extremo con persistencia
bash
CARNET=$CARNET locust -f locust/locustfile.py --host http://localhost:8080 --headless -u 20 -r 10 -t 30s

kubectl exec -n data deploy/valkey -- valkey-cli hgetall stats
kubectl exec -n data deploy/valkey -- valkey-cli zrevrange top:planes 0 -1 withscores
kubectl exec -n data deploy/valkey -- valkey-cli hgetall reports:count
kubectl exec -n data deploy/valkey -- valkey-cli xlen series:RUS
kubectl exec -n messaging deploy/rabbitmq -- rabbitmqctl list_queues name messages consumers

Prueba de persistencia (mata el pod de Valkey y comprueba que los datos sobreviven):

bash
kubectl delete pod -n data -l app.kubernetes.io/name=valkey
kubectl rollout status deploy/valkey -n data
kubectl exec -n data deploy/valkey -- valkey-cli hgetall reports:count    # los totales siguen ahí

✅ Verificación 26:

stats muestra max_planes cerca de 50, min_planes cerca de 0 y las modas.
reports:count suma aproximadamente los requests de Locust (cada país con ~20%).
La cola de RabbitMQ queda en 0.
Tras matar el pod, los datos siguen.
Si la cola se queda acumulando, revisa kubectl logs -n apps deploy/consumer.
Cumplimiento del enunciado
Requisito	Estado
Operator con Operator SDK (Helm-based), en el clúster, imagen en Zot	✅ Pasos 20-23
CRD ValkeyInstance	✅
Helm chart encapsulado con Deployment, Service y PVC	✅ Paso 21
Reconciliación crear/actualizar/eliminar	✅ Paso 24 (la eliminación queda para la demo)
Persistencia (PVC) y conectividad Consumer ↔ Service de Valkey	✅ Paso 26
Consumer almacena en Valkey	✅ Paso 25
TTL / expiración para evitar saturación	✅ streams con TTL y volatile-lru
Operator de Grafana, dashboard completo	⏳ Siguiente bloque
HPA, 1 vs 2 réplicas, OCI Artifact	⏳ Después de Grafana

Dos datos útiles para lo que sigue:

Con tu carnet 202302232, el último dígito es 2, así que tu país asignado es RUS (2,3 = RUS). Confírmalo, porque el dashboard se construye alrededor de él.
La rúbrica menciona pruebas con “1 y 2 réplicas para Go Writers y Valkey”. Valkey está en 1 réplica fija. Vale la pena preguntarle a tu auxiliar qué esperan ahí antes de la calificación. Es probable que baste con comparar el rendimiento con 1 y 2 réplicas de los Go Writers.
Qué pegarme para continuar
bash
kubectl get pods -A | grep -v kube-system
kubectl exec -n data deploy/valkey -- valkey-cli hgetall stats
kubectl exec -n data deploy/valkey -- valkey-cli hgetall reports:count
free -h

Con eso paso al Operator de Grafana (GrafanaInstance): chart con datasource hacia Valkey, el plugin Redis y el dashboard con los 11 paneles. Esa es la parte con más riesgo técnico, y conviene atacarla mañana temprano.