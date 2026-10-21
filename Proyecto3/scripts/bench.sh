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
