Diseño de este bloque. El enunciado pide que el Go Deployment 1 tenga 2 containers (API REST + cliente gRPC). Para probar el camino gRPC completo hoy, también levantamos el Deployment 2 con el gRPC Server en modo básico (responde ok). En el siguiente bloque le agregamos el container Writer que publica en RabbitMQ.

Locust → Gateway → Rust → [pod go-api: api → grpc-client] ──gRPC──► [pod grpc-server]
Paso 10: Herramientas gRPC y módulo Go (ya lo hice, solo comprueba que este todo este paso 10)
bash
cd ~/<TU_REPO>/proyecto3
sudo pacman -S --needed protobuf
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.34.2
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1
export PATH=$PATH:$(go env GOPATH)/bin     # agrégalo también a tu .zshrc/.bashrc

mkdir -p go-services/{proto,cmd/api,cmd/grpc-client,cmd/grpc-server}
cd go-services
go mod init munk8s

Aquí va el .proto del enunciado, sin cambios:

bash
cat > proto/war.proto <<'EOF'
syntax = "proto3";
package wartweets;
option go_package = "./proto";

// Mensaje que se enviará
message WarReportRequest {
  Countries country = 1;
  int32 warplanes_in_air = 2;
  int32 warships_in_water = 3;
  string timestamp = 4;
}

// Lista de los únicos posibles países aceptados por el proyecto
enum Countries {
  countries_unknown = 0;
  usa = 1;
  rus = 2;
  chn = 3;
  esp = 4;
  gtm = 5;
}

// Respuesta del servidor
message WarReportResponse {
  string status = 1;
}

// Servicio gRPC
service WarReportService {
  rpc SendReport (WarReportRequest) returns (WarReportResponse);
}
EOF

protoc --go_out=. --go_opt=paths=source_relative \
       --go-grpc_out=. --go-grpc_opt=paths=source_relative proto/war.proto
ls proto/

✅ Verificación 10: ls proto/ muestra war.proto war.pb.go war_grpc.pb.go. Si alguna versión de los plugins no existe, avísame.

Paso 11: Código de los 3 servicios Go

El net/http de Go ya atiende cada petición en su propia goroutine. En el cliente gRPC agregamos además un semáforo (máximo 256 llamadas en vuelo) para que un pico de Locust no te dispare la RAM. Eso responde a “manejo de concurrencia” de la rúbrica.

11.1 Container 1 del Deployment 1: api (recibe de Rust) (ya lo hice, solo comprueba que este bien)

bash
cat > cmd/api/main.go <<'EOF'
package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"time"
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

func main() {
	clientURL := getenv("CLIENT_URL", "http://127.0.0.1:8082/send")
	hc := &http.Client{
		Timeout:   5 * time.Second,
		Transport: &http.Transport{MaxIdleConnsPerHost: 100},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	mux.HandleFunc("POST /report", func(w http.ResponseWriter, r *http.Request) {
		var rep Report
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&rep); err != nil {
			http.Error(w, "json invalido", http.StatusBadRequest)
			return
		}
		if len(rep.Country) != 3 || rep.WarplanesInAir < 0 || rep.WarshipsInWater < 0 {
			http.Error(w, "datos invalidos", http.StatusBadRequest)
			return
		}
		b, _ := json.Marshal(rep)
		resp, err := hc.Post(clientURL, "application/json", bytes.NewReader(b))
		if err != nil {
			http.Error(w, "grpc-client no disponible", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	})

	log.Println("api escuchando en :8081")
	log.Fatal(http.ListenAndServe(":8081", mux))
}
EOF

(De aquí en adelante aun no lo eh hecho)
11.2 Container 2 del Deployment 1: grpc-client

Escucha solo en 127.0.0.1, porque se comunica con api dentro del mismo pod. Usa round_robin para repartir entre las réplicas del gRPC Server (esto importa para tu comparación de 1 vs 2 réplicas):

bash
cat > cmd/grpc-client/main.go <<'EOF'
package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	pb "munk8s/proto"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type in struct {
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

func main() {
	addr := getenv("GRPC_SERVER_ADDR", "grpc-server:50051")
	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(`{"loadBalancingConfig":[{"round_robin":{}}]}`))
	if err != nil {
		log.Fatalf("grpc: %v", err)
	}
	defer conn.Close()
	client := pb.NewWarReportServiceClient(conn)

	sem := make(chan struct{}, 256) // máximo de llamadas gRPC en vuelo

	mux := http.NewServeMux()
	mux.HandleFunc("POST /send", func(w http.ResponseWriter, r *http.Request) {
		var m in
		if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
			http.Error(w, "json invalido", http.StatusBadRequest)
			return
		}
		code, ok := pb.Countries_value[strings.ToLower(m.Country)]
		if !ok || code == 0 {
			http.Error(w, "pais no aceptado", http.StatusBadRequest)
			return
		}
		select {
		case sem <- struct{}{}:
			defer func() { <-sem }()
		case <-r.Context().Done():
			http.Error(w, "cancelado", http.StatusServiceUnavailable)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		resp, err := client.SendReport(ctx, &pb.WarReportRequest{
			Country:         pb.Countries(code),
			WarplanesInAir:  m.WarplanesInAir,
			WarshipsInWater: m.WarshipsInWater,
			Timestamp:       m.Timestamp,
		})
		if err != nil {
			log.Printf("error gRPC: %v", err)
			http.Error(w, "grpc-server no disponible", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": resp.Status})
	})

	log.Printf("grpc-client -> %s, escuchando en 127.0.0.1:8082", addr)
	log.Fatal(http.ListenAndServe("127.0.0.1:8082", mux))
}
EOF

11.3 Deployment 2: grpc-server (versión básica por ahora)

bash
cat > cmd/grpc-server/main.go <<'EOF'
package main

import (
	"context"
	"log"
	"net"
	"sync/atomic"

	pb "munk8s/proto"

	"google.golang.org/grpc"
)

type server struct {
	pb.UnimplementedWarReportServiceServer
	n atomic.Int64
}

func (s *server) SendReport(ctx context.Context, r *pb.WarReportRequest) (*pb.WarReportResponse, error) {
	// Se loguea 1 de cada 100 para no inundar los logs
	if n := s.n.Add(1); n%100 == 1 {
		log.Printf("reporte #%d pais=%s aviones=%d barcos=%d",
			n, r.Country, r.WarplanesInAir, r.WarshipsInWater)
	}
	// SIGUIENTE BLOQUE: aquí se reenvía al container Writer (RabbitMQ)
	return &pb.WarReportResponse{Status: "ok"}, nil
}

func main() {
	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatal(err)
	}
	g := grpc.NewServer()
	pb.RegisterWarReportServiceServer(g, &server{})
	log.Println("grpc-server escuchando en :50051")
	log.Fatal(g.Serve(lis))
}
EOF

go get google.golang.org/grpc@v1.67.1
go mod tidy
go build ./... && echo "COMPILA OK"

✅ Verificación 11: imprime COMPILA OK.

Paso 12: Dockerfile único, build y push a Zot

Un solo Dockerfile sirve para todos los servicios Go (se elige con SERVICE), así la caché de módulos se comparte y ahorras disco.

bash
cat > Dockerfile <<'EOF'
ARG GO_VERSION
FROM golang:${GO_VERSION}-alpine AS build
ARG SERVICE
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/app ./cmd/${SERVICE}

FROM alpine:3.20
RUN adduser -D app
COPY --from=build /out/app /usr/local/bin/app
USER app
CMD ["app"]
EOF
printf '.git\n' > .dockerignore

cat > ../scripts/build-go.sh <<'EOF'
#!/usr/bin/env bash
# uso: ./scripts/build-go.sh <tag> [servicios...]
set -e
cd "$(dirname "$0")/../go-services"
TAG=${1:?uso: build-go.sh <tag> [servicios...]}; shift
[ $# -eq 0 ] && set -- api grpc-client grpc-server
GO_VERSION=$(go env GOVERSION | sed 's/^go//')
for s in "$@"; do
  docker build --build-arg GO_VERSION=$GO_VERSION --build-arg SERVICE=$s \
    -t zot:5000/munk8s/go-$s:$TAG .
  docker push zot:5000/munk8s/go-$s:$TAG
done
EOF
chmod +x ../scripts/build-go.sh

cd .. && ./scripts/build-go.sh v1 && ./scripts/cleanup.sh
curl -s https://zot:5000/v2/_catalog

✅ Verificación 12: el catálogo muestra munk8s/go-api, munk8s/go-grpc-client, munk8s/go-grpc-server y munk8s/rust-api.

Regla de tags: cada vez que reconstruyas una imagen, sube el tag (v2, v3...) y actualiza el YAML. El nodo k3s cachea por tag y, si reusas v1, seguirá usando la imagen vieja.

Paso 13: Desplegar Go Deployment 1 y gRPC Server
bash
cat > k8s/20-go-api.yaml <<'EOF'
apiVersion: apps/v1
kind: Deployment
metadata: { name: go-api, namespace: apps }
spec:
  replicas: 1
  selector: { matchLabels: { app: go-api } }
  template:
    metadata: { labels: { app: go-api } }
    spec:
      containers:
      - name: api
        image: zot:5000/munk8s/go-api:v1
        ports: [{ containerPort: 8081 }]
        env:
        - { name: GOMEMLIMIT, value: "60MiB" }   # el GC de Go respeta el límite del pod
        resources:
          requests: { cpu: 50m, memory: 32Mi }
          limits:   { cpu: 300m, memory: 96Mi }
        readinessProbe:
          httpGet: { path: /health, port: 8081 }
          initialDelaySeconds: 2
      - name: grpc-client
        image: zot:5000/munk8s/go-grpc-client:v1
        env:
        - { name: GRPC_SERVER_ADDR, value: "grpc-server:50051" }
        - { name: GOMEMLIMIT, value: "60MiB" }
        resources:
          requests: { cpu: 50m, memory: 32Mi }
          limits:   { cpu: 300m, memory: 96Mi }
---
apiVersion: v1
kind: Service
metadata: { name: go-api, namespace: apps }
spec:
  selector: { app: go-api }
  ports: [{ port: 8081, targetPort: 8081 }]
EOF

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
        image: zot:5000/munk8s/go-grpc-server:v1
        ports: [{ containerPort: 50051 }]
        env:
        - { name: GOMEMLIMIT, value: "60MiB" }
        resources:
          requests: { cpu: 50m, memory: 32Mi }
          limits:   { cpu: 300m, memory: 96Mi }
---
# Headless: el DNS devuelve la IP de cada réplica, así gRPC reparte la carga (round_robin)
apiVersion: v1
kind: Service
metadata: { name: grpc-server, namespace: apps }
spec:
  clusterIP: None
  selector: { app: grpc-server }
  ports: [{ port: 50051, targetPort: 50051 }]
EOF

kubectl apply -f k8s/20-go-api.yaml -f k8s/21-grpc-server.yaml
kubectl get pods -n apps -w

✅ Verificación 13: go-api muestra 2/2 Running (los dos containers), y grpc-server y rust-api están 1/1 Running.

Si alguno falla, mándame la salida de kubectl describe pod <pod> -n apps | tail -20 y kubectl logs <pod> -n apps --all-containers.

Paso 14: Locust
bash
cat > locust/locustfile.py <<'EOF'
import os
import random
from datetime import datetime, timezone

from locust import HttpUser, task, between

CARNET = os.environ["CARNET"]
COUNTRIES = ["USA", "RUS", "CHN", "ESP", "GTM"]


class ReportUser(HttpUser):
    wait_time = between(0.01, 0.1)

    @task
    def send_report(self):
        payload = {
            "country": random.choice(COUNTRIES),
            "warplanes_in_air": random.randint(0, 50),
            "warships_in_water": random.randint(0, 30),
            "timestamp": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
        }
        self.client.post(f"/grpc-{CARNET}", json=payload)
EOF
Paso 15: Prueba del pipeline completo

15.1 Una petición manual:

bash
curl -i -X POST http://localhost:8080/grpc-$CARNET \
  -H 'Content-Type: application/json' \
  -d '{"country":"ESP","warplanes_in_air":42,"warships_in_water":14,"timestamp":"2026-03-12T20:15:30Z"}'

Debe responder HTTP/1.1 200 OK con {"status":"ok"}. El 502 de antes ya no debería aparecer.

15.2 Prueba negativa (valida que el enum rechaza países fuera de la lista):

bash
curl -i -X POST http://localhost:8080/grpc-$CARNET -H 'Content-Type: application/json' \
  -d '{"country":"FRA","warplanes_in_air":1,"warships_in_water":1,"timestamp":"2026-03-12T20:15:30Z"}'

Debe dar 400.

15.3 Locust, carga ligera de 30 segundos:

bash
CARNET=$CARNET locust -f locust/locustfile.py --host http://localhost:8080 \
  --headless -u 10 -r 5 -t 30s

15.4 Revisa recursos y logs:

bash
kubectl logs -n apps deploy/grpc-server --tail=5
kubectl top pods -n apps
free -h

✅ Verificación 15:

15.1 da 200 y 15.2 da 400.
Locust termina con 0 fallos (Fails en 0).
kubectl logs de grpc-server muestra líneas reporte #....
kubectl top pods muestra cada pod muy por debajo de sus límites.
Cumplimiento del enunciado hasta aquí
Requisito	Estado
Locust con el JSON especificado y rangos 0-50 / 0-30	✅ Paso 14
Gateway API con /grpc-#carnet	✅
Rust recibe de Locust y envía a Go	✅
Go Deployment 1: 2 containers (API REST + gRPC Client)	✅
gRPC con el .proto propuesto y manejo de concurrencia	✅
gRPC Server (Deployment 2)	🔄 Recibe, aún sin Writer
Imágenes publicadas y consumidas desde Zot	✅
requests y limits en todos los deployments	✅
Lo que sigue (hoy, si te alcanza el ánimo)
RabbitMQ en el namespace messaging, con límite de 512 MB y una cola durable.
Container Writer en el Deployment 2 (el gRPC Server le pasa el mensaje por localhost y el Writer publica en RabbitMQ con goroutines).
Consumer en Go, primero imprimiendo los mensajes. Se conecta a Valkey cuando exista el Operator mañana.

Pégame la salida de:

bash
kubectl get pods -n apps
curl -s -o /dev/null -w "%{http_code}\n" -X POST http://localhost:8080/grpc-$CARNET -H 'Content-Type: application/json' -d '{"country":"ESP","warplanes_in_air":42,"warships_in_water":14,"timestamp":"2026-03-12T20:15:30Z"}'

y de las últimas líneas del resumen de Locust (Fails y Median).