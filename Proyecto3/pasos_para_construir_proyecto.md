Paso 16: RabbitMQ en el namespace messaging

Todas las imágenes deben venir de Zot, así que primero la subimos:

bash
cd ~/<TU_REPO>/proyecto3
docker pull rabbitmq:3.13-management-alpine
docker tag rabbitmq:3.13-management-alpine zot:5000/munk8s/rabbitmq:3.13
docker push zot:5000/munk8s/rabbitmq:3.13

Crea las credenciales como Secret, sin dejarlas en el repo. El Writer y el Consumer están en apps, por eso va un Secret en cada namespace:

bash
export RABBIT_PASS=$(openssl rand -hex 8)
for ns in messaging apps; do
  kubectl create secret generic rabbit-cred -n $ns \
    --from-literal=user=munk8s --from-literal=pass=$RABBIT_PASS
done

Manifiesto:

bash
cat > k8s/30-rabbitmq.yaml <<'EOF'
apiVersion: v1
kind: ConfigMap
metadata: { name: rabbit-conf, namespace: messaging }
data:
  20-memory.conf: |
    vm_memory_high_watermark.absolute = 400MiB
---
apiVersion: apps/v1
kind: Deployment
metadata: { name: rabbitmq, namespace: messaging }
spec:
  replicas: 1
  strategy: { type: Recreate }
  selector: { matchLabels: { app: rabbitmq } }
  template:
    metadata: { labels: { app: rabbitmq } }
    spec:
      containers:
      - name: rabbitmq
        image: zot:5000/munk8s/rabbitmq:3.13
        ports:
        - { name: amqp, containerPort: 5672 }
        - { name: mgmt, containerPort: 15672 }
        env:
        - name: RABBITMQ_DEFAULT_USER
          valueFrom: { secretKeyRef: { name: rabbit-cred, key: user } }
        - name: RABBITMQ_DEFAULT_PASS
          valueFrom: { secretKeyRef: { name: rabbit-cred, key: pass } }
        - { name: RABBITMQ_SERVER_ADDITIONAL_ERL_ARGS, value: "+S 2:2" }
        volumeMounts:
        - { name: conf, mountPath: /etc/rabbitmq/conf.d/20-memory.conf, subPath: 20-memory.conf }
        resources:
          requests: { cpu: 100m, memory: 384Mi }
          limits:   { cpu: "1",  memory: 640Mi }
        readinessProbe:
          exec: { command: ["rabbitmq-diagnostics", "-q", "ping"] }
          initialDelaySeconds: 20
          periodSeconds: 10
          timeoutSeconds: 10
      volumes:
      - name: conf
        configMap: { name: rabbit-conf }
---
apiVersion: v1
kind: Service
metadata: { name: rabbitmq, namespace: messaging }
spec:
  selector: { app: rabbitmq }
  ports:
  - { name: amqp, port: 5672, targetPort: 5672 }
  - { name: mgmt, port: 15672, targetPort: 15672 }
EOF
kubectl apply -f k8s/30-rabbitmq.yaml
kubectl get pods -n messaging -w

✅ Verificación 16: el pod queda 1/1 Running (tarda 30-60 s) y esto responde sin error:

bash
kubectl exec -n messaging deploy/rabbitmq -- rabbitmqctl list_queues
Paso 17: Container Writer y gRPC Server actualizado

El Deployment 2 pasa a tener 2 containers, como pide el enunciado: grpc-server recibe por gRPC y le pasa el mensaje por localhost al writer, que publica en RabbitMQ.

17.1 Writer. Usa un pool de 8 canales AMQP. Cada petición, que ya corre en su propia goroutine, toma un canal del pool, publica y lo devuelve. Eso resuelve la concurrencia sin compartir un canal entre hilos, algo que AMQP no permite.

bash
cd go-services
mkdir -p cmd/writer cmd/consumer
cat > cmd/writer/main.go <<'EOF'
package main

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

func getenv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func main() {
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
	// Si se pierde la conexión, salimos y Kubernetes reinicia el container
	go func() { log.Fatalf("conexion RabbitMQ cerrada: %v", <-conn.NotifyClose(make(chan *amqp.Error, 1))) }()

	const poolSize = 8
	pool := make(chan *amqp.Channel, poolSize)
	for i := 0; i < poolSize; i++ {
		ch, err := conn.Channel()
		if err != nil {
			log.Fatal(err)
		}
		if i == 0 { // cola durable
			if _, err = ch.QueueDeclare(queue, true, false, false, false, nil); err != nil {
				log.Fatal(err)
			}
		}
		pool <- ch
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /publish", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
		if err != nil || !json.Valid(body) {
			http.Error(w, "json invalido", http.StatusBadRequest)
			return
		}
		var ch *amqp.Channel
		select {
		case ch = <-pool:
		case <-r.Context().Done():
			http.Error(w, "cancelado", http.StatusServiceUnavailable)
			return
		}
		defer func() { pool <- ch }()

		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		err = ch.PublishWithContext(ctx, "", queue, false, false, amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent,
			Body:         body,
		})
		if err != nil {
			log.Printf("error publicando: %v", err)
			if ch.IsClosed() {
				log.Fatal("canal cerrado, reiniciando")
			}
			http.Error(w, "rabbitmq no disponible", http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	})

	log.Printf("writer -> cola %q, escuchando en 127.0.0.1:8083", queue)
	log.Fatal(http.ListenAndServe("127.0.0.1:8083", mux))
}
EOF

17.2 gRPC Server. Reemplaza el main.go anterior, ahora con la llamada al Writer:

bash
cat > cmd/grpc-server/main.go <<'EOF'
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	pb "munk8s/proto"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type server struct {
	pb.UnimplementedWarReportServiceServer
	n         atomic.Int64
	hc        *http.Client
	writerURL string
}

func (s *server) SendReport(ctx context.Context, r *pb.WarReportRequest) (*pb.WarReportResponse, error) {
	if n := s.n.Add(1); n%100 == 1 {
		log.Printf("reporte #%d pais=%s aviones=%d barcos=%d", n, r.Country, r.WarplanesInAir, r.WarshipsInWater)
	}
	b, _ := json.Marshal(map[string]any{
		"country":           strings.ToUpper(r.Country.String()),
		"warplanes_in_air":  r.WarplanesInAir,
		"warships_in_water": r.WarshipsInWater,
		"timestamp":         r.Timestamp,
	})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, s.writerURL, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.hc.Do(req)
	if err != nil {
		return nil, status.Error(codes.Unavailable, "writer no disponible")
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 300 {
		return nil, status.Error(codes.Unavailable, "writer rechazo el mensaje")
	}
	return &pb.WarReportResponse{Status: "ok"}, nil
}

func main() {
	wurl := os.Getenv("WRITER_URL")
	if wurl == "" {
		wurl = "http://127.0.0.1:8083/publish"
	}
	s := &server{
		hc: &http.Client{Timeout: 3 * time.Second,
			Transport: &http.Transport{MaxIdleConnsPerHost: 100}},
		writerURL: wurl,
	}
	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatal(err)
	}
	g := grpc.NewServer()
	pb.RegisterWarReportServiceServer(g, s)
	log.Println("grpc-server escuchando en :50051")
	log.Fatal(g.Serve(lis))
}
EOF

17.3 Dependencias y compilación:

bash
go get github.com/rabbitmq/amqp091-go@v1.10.0
go mod tidy
go build ./... && echo "COMPILA OK"

17.4 Build y push (los tres con tag v2):

bash
cd .. && ./scripts/build-go.sh v2 grpc-server writer && ./scripts/cleanup.sh

17.5 Deployment 2 con 2 containers (reemplaza el archivo anterior):

bash
cat > k8s/21-grpc-server.yaml <<'EOF'
apiVersion: apps/v1
kind: Deployment
metadata: { name: grpc-server, namespace: apps }
spec:
  replicas: 1
  selector: { matchLabels: { app: grpc-server } }
  template:
    metadata: { labels: { app: grpc-server } }
    spec:
      containers:
      - name: grpc-server
        image: zot:5000/munk8s/go-grpc-server:v2
        ports: [{ containerPort: 50051 }]
        env:
        - { name: GOMEMLIMIT, value: "60MiB" }
        resources:
          requests: { cpu: 50m, memory: 32Mi }
          limits:   { cpu: 300m, memory: 96Mi }
      - name: writer
        image: zot:5000/munk8s/go-writer:v2
        env:
        - { name: GOMEMLIMIT, value: "60MiB" }
        - { name: RABBIT_HOST, value: "rabbitmq.messaging.svc.cluster.local:5672" }
        - name: RABBIT_USER
          valueFrom: { secretKeyRef: { name: rabbit-cred, key: user } }
        - name: RABBIT_PASS
          valueFrom: { secretKeyRef: { name: rabbit-cred, key: pass } }
        resources:
          requests: { cpu: 50m, memory: 32Mi }
          limits:   { cpu: 300m, memory: 96Mi }
---
apiVersion: v1
kind: Service
metadata: { name: grpc-server, namespace: apps }
spec:
  clusterIP: None
  selector: { app: grpc-server }
  ports: [{ port: 50051, targetPort: 50051 }]
EOF
kubectl apply -f k8s/21-grpc-server.yaml
kubectl get pods -n apps -w

✅ Verificación 17. grpc-server debe quedar 2/2 Running. Luego manda 3 mensajes manuales y mira la cola, que debe mostrar 3 mensajes porque aún no hay consumer:

bash
for i in 1 2 3; do curl -s -X POST http://localhost:8080/grpc-$CARNET -H 'Content-Type: application/json' \
  -d '{"country":"GTM","warplanes_in_air":10,"warships_in_water":5,"timestamp":"2026-03-12T20:15:30Z"}'; echo; done
kubectl exec -n messaging deploy/rabbitmq -- rabbitmqctl list_queues name messages consumers

Debe mostrar war-reports 3 0. Si el pod del Writer reinicia, mira kubectl logs -n apps deploy/grpc-server -c writer.

No corras Locust todavía. Sin consumer, la cola crece y se come la RAM de RabbitMQ. Primero el paso 18.

Paso 18: Consumer en Go (por ahora solo procesa y cuenta)

Cuando exista el Operator de Valkey, solo cambiaremos la función process. Usa ack manual, prefetch de 50 y 4 workers concurrentes.

bash
cd go-services
cat > cmd/consumer/main.go <<'EOF'
package main

import (
	"encoding/json"
	"log"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
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

var total atomic.Int64

// TODO (con el Operator): escribir en Valkey
func process(r Report) error {
	if n := total.Add(1); n%100 == 1 {
		log.Printf("procesado #%d pais=%s aviones=%d barcos=%d", n, r.Country, r.WarplanesInAir, r.WarshipsInWater)
	}
	return nil
}

func main() {
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
					d.Nack(false, true) // reintentar
					continue
				}
				d.Ack(false)
			}
		}()
	}
	log.Printf("consumer leyendo cola %q con 4 workers", queue)
	wg.Wait()
	log.Fatal("canal de consumo cerrado")
}
EOF
go build ./... && echo "COMPILA OK"
cd .. && ./scripts/build-go.sh v2 consumer && ./scripts/cleanup.sh

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
        image: zot:5000/munk8s/go-consumer:v2
        env:
        - { name: GOMEMLIMIT, value: "60MiB" }
        - { name: RABBIT_HOST, value: "rabbitmq.messaging.svc.cluster.local:5672" }
        - name: RABBIT_USER
          valueFrom: { secretKeyRef: { name: rabbit-cred, key: user } }
        - name: RABBIT_PASS
          valueFrom: { secretKeyRef: { name: rabbit-cred, key: pass } }
        resources:
          requests: { cpu: 50m, memory: 32Mi }
          limits:   { cpu: 300m, memory: 96Mi }
EOF
kubectl apply -f k8s/40-consumer.yaml
kubectl logs -n apps deploy/consumer --tail=5

✅ Verificación 18:

kubectl logs -n apps deploy/consumer muestra “consumer leyendo cola”.
La cola se vació: kubectl exec -n messaging deploy/rabbitmq -- rabbitmqctl list_queues name messages consumers → war-reports 0 1.
Ahora sí, carga ligera con Locust:
bash
CARNET=$CARNET locust -f locust/locustfile.py --host http://localhost:8080 --headless -u 20 -r 10 -t 30s
kubectl exec -n messaging deploy/rabbitmq -- rabbitmqctl list_queues name messages consumers
kubectl top pods -A
free -h

Locust debe terminar con 0 fallos y la cola debe quedar en 0 o cerca de 0.

Cumplimiento del enunciado
Requisito	Estado
RabbitMQ como broker principal, imagen desde Zot	✅ Paso 16
Deployment 2: gRPC Server + Writer (2 containers)	✅ Paso 17
Writer publica en RabbitMQ con concurrencia	✅ pool de canales
Consumer lee la cola y procesa	✅ Paso 18 (falta guardar en Valkey)
Valkey con Operator, Grafana con Operator	⏳ Mañana 21
HPA de Rust, pruebas 1 vs 2 réplicas, OCI Artifact	⏳ Después del Operator
Para el siguiente mensaje

Pégame:

bash
kubectl get pods -A
kubectl exec -n messaging deploy/rabbitmq -- rabbitmqctl list_queues name messages consumers
free -h

y las últimas líneas de Locust (Fails, Median, req/s). Con eso empezamos el Operator de Valkey, que es lo que más puntos da. Si hoy te sobra energía, ya puedes ir leyendo la guía de Operator SDK para Helm que cita el enunciado.