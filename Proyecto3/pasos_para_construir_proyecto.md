Bloque 4: Operator de Grafana (25 pts)

Hay dos riesgos técnicos y los ataco con este orden:

Grafana no lee Valkey de forma nativa. Necesita el plugin redis-datasource. Lo horneo en una imagen propia que subimos a Zot, en vez de descargarlo en cada arranque del pod. Así el pod no depende de internet, arranca rápido y todo viene de Zot.
No voy a escribirte el JSON del dashboard de memoria. El formato exacto de las queries del plugin Redis es fácil de equivocar. Primero levantamos Grafana con el datasource provisionado, armas los paneles en la UI, exportas el JSON y lo metes al chart. El resultado sigue siendo 100% declarativo.

Tu país asignado es RUS: el carnet termina en 2.

Paso 27: Imagen de Grafana con el plugin Redis
bash
cd ~/<TU_REPO>/proyecto3
mkdir -p grafana-image
cat > grafana-image/Dockerfile <<'EOF'
ARG GRAFANA_VERSION=11.2.0
FROM grafana/grafana:${GRAFANA_VERSION}
RUN grafana cli plugins install redis-datasource
EOF
docker build -t zot:5000/munk8s/grafana:11.2.0-redis grafana-image/
docker push zot:5000/munk8s/grafana:11.2.0-redis
./scripts/cleanup.sh

✅ Verificación 27: el build termina sin error (descarga el plugin, necesita internet) y curl -s https://zot:5000/v2/_catalog incluye munk8s/grafana.

Paso 28: Segundo kind en el mismo Operator
bash
cd operator
operator-sdk create api --group observability --version v1alpha1 --kind GrafanaInstance
cd helm-charts/grafanainstance
rm -rf templates/* values.yaml
mkdir dashboards

✅ Verificación 28: cat ../../watches.yaml ahora lista dos entradas: ValkeyInstance y GrafanaInstance.

Paso 29: Helm chart de Grafana

Estás dentro de helm-charts/grafanainstance.

bash
cat > values.yaml <<'EOF'
image:
  repository: zot:5000/munk8s/grafana
  tag: "11.2.0-redis"
  pullPolicy: IfNotPresent
adminPassword: "munk8s-admin"
datasource:
  valkeyAddress: "valkey.data.svc.cluster.local:6379"
resources:
  requests: { cpu: 50m, memory: 128Mi }
  limits:   { cpu: 500m, memory: 384Mi }
EOF

cat > templates/secret.yaml <<'EOF'
apiVersion: v1
kind: Secret
metadata:
  name: {{ .Release.Name }}-admin
  labels:
    app.kubernetes.io/name: grafana
    app.kubernetes.io/instance: {{ .Release.Name }}
stringData:
  admin-user: admin
  admin-password: {{ .Values.adminPassword | quote }}
EOF

cat > templates/configmap-provisioning.yaml <<'EOF'
apiVersion: v1
kind: ConfigMap
metadata:
  name: {{ .Release.Name }}-provisioning
  labels:
    app.kubernetes.io/name: grafana
    app.kubernetes.io/instance: {{ .Release.Name }}
data:
  datasources.yaml: |
    apiVersion: 1
    datasources:
    - name: Valkey
      uid: valkey
      type: redis-datasource
      access: proxy
      url: {{ .Values.datasource.valkeyAddress }}
      isDefault: true
      jsonData:
        client: standalone
        poolSize: 5
        timeout: 10
        pingInterval: 0
        pipelineWindow: 0
  dashboards.yaml: |
    apiVersion: 1
    providers:
    - name: munk8s
      folder: M.U.M.N.K8s
      type: file
      allowUiUpdates: true
      options:
        path: /var/lib/grafana/dashboards
EOF

cat > templates/configmap-dashboards.yaml <<'EOF'
apiVersion: v1
kind: ConfigMap
metadata:
  name: {{ .Release.Name }}-dashboards
  labels:
    app.kubernetes.io/name: grafana
    app.kubernetes.io/instance: {{ .Release.Name }}
data:
{{- range $path, $_ := .Files.Glob "dashboards/*.json" }}
  {{ base $path }}: |-
{{ $.Files.Get $path | indent 4 }}
{{- end }}
EOF

cat > templates/service.yaml <<'EOF'
apiVersion: v1
kind: Service
metadata:
  name: {{ .Release.Name }}
  labels:
    app.kubernetes.io/name: grafana
    app.kubernetes.io/instance: {{ .Release.Name }}
spec:
  selector:
    app.kubernetes.io/name: grafana
    app.kubernetes.io/instance: {{ .Release.Name }}
  ports:
  - name: http
    port: 3000
    targetPort: 3000
EOF

cat > templates/deployment.yaml <<'EOF'
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ .Release.Name }}
  labels:
    app.kubernetes.io/name: grafana
    app.kubernetes.io/instance: {{ .Release.Name }}
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: grafana
      app.kubernetes.io/instance: {{ .Release.Name }}
  template:
    metadata:
      labels:
        app.kubernetes.io/name: grafana
        app.kubernetes.io/instance: {{ .Release.Name }}
      annotations:
        checksum/provisioning: {{ include (print $.Template.BasePath "/configmap-provisioning.yaml") . | sha256sum }}
        checksum/dashboards: {{ include (print $.Template.BasePath "/configmap-dashboards.yaml") . | sha256sum }}
    spec:
      containers:
      - name: grafana
        image: "{{ .Values.image.repository }}:{{ .Values.image.tag }}"
        imagePullPolicy: {{ .Values.image.pullPolicy }}
        env:
        - name: GF_SECURITY_ADMIN_USER
          valueFrom: { secretKeyRef: { name: {{ .Release.Name }}-admin, key: admin-user } }
        - name: GF_SECURITY_ADMIN_PASSWORD
          valueFrom: { secretKeyRef: { name: {{ .Release.Name }}-admin, key: admin-password } }
        - { name: GF_ANALYTICS_REPORTING_ENABLED, value: "false" }
        - { name: GF_ANALYTICS_CHECK_FOR_UPDATES, value: "false" }
        - { name: GOMEMLIMIT, value: "256MiB" }
        ports:
        - containerPort: 3000
        readinessProbe:
          httpGet: { path: /api/health, port: 3000 }
          initialDelaySeconds: 10
          periodSeconds: 10
        resources:
          {{- toYaml .Values.resources | nindent 10 }}
        volumeMounts:
        - { name: prov-datasources, mountPath: /etc/grafana/provisioning/datasources }
        - { name: prov-dashboards, mountPath: /etc/grafana/provisioning/dashboards }
        - { name: dashboards, mountPath: /var/lib/grafana/dashboards }
      volumes:
      - name: prov-datasources
        configMap:
          name: {{ .Release.Name }}-provisioning
          items: [{ key: datasources.yaml, path: datasources.yaml }]
      - name: prov-dashboards
        configMap:
          name: {{ .Release.Name }}-provisioning
          items: [{ key: dashboards.yaml, path: dashboards.yaml }]
      - name: dashboards
        configMap:
          name: {{ .Release.Name }}-dashboards
EOF
cd ../..
helm template prueba helm-charts/grafanainstance | head -50

Decisiones que debes poder explicar:

Sin PVC: el datasource y los dashboards se provisionan desde ConfigMaps, así que Grafana no guarda estado propio que valga la pena persistir. Ahorras disco.
Anotaciones checksum/*: si cambias el datasource o el dashboard en el CR, el pod se reinicia solo y toma la nueva configuración.
La contraseña va en el CR en texto plano: es aceptable en un laboratorio local, pero menciónalo en el manual como limitación.

✅ Verificación 29: helm template renderiza Secret, 2 ConfigMaps, Service y Deployment sin error.

Paso 30: Recompilar y redesplegar el Operator (v2)
bash
export IMG=zot:5000/munk8s/operator:v2
make docker-build IMG=$IMG
make docker-push IMG=$IMG
make deploy IMG=$IMG
kubectl rollout status deploy/munk8s-operator-controller-manager -n munk8s-operator-system
cd .. && ./scripts/cleanup.sh && cd operator

✅ Verificación 30:

bash
kubectl get crd | grep munk8s      # deben salir valkeyinstances y grafanainstances

Revisa que el Valkey existente siga 1/1 Running. El redespliegue no debe haberlo tocado.

Paso 31: Crear el Custom Resource de Grafana
bash
cat > ../k8s/51-grafana-cr.yaml <<'EOF'
apiVersion: observability.munk8s.io/v1alpha1
kind: GrafanaInstance
metadata:
  name: grafana
  namespace: data
spec:
  adminPassword: "munk8s-admin"
  datasource:
    valkeyAddress: "valkey.data.svc.cluster.local:6379"
EOF
kubectl apply -f ../k8s/51-grafana-cr.yaml
kubectl get deploy,svc,cm,secret,pods -n data
kubectl logs -n munk8s-operator-system deploy/munk8s-operator-controller-manager --tail=15

Si el CR queda con error de permisos en el log (forbidden), pásame esa línea. El chart_role.yaml del paso 22 ya cubre los recursos que usa este chart.

Abre Grafana:

bash
kubectl port-forward -n data svc/grafana 3000:3000

Entra a http://localhost:3000 con admin / munk8s-admin.

✅ Verificación 31:

El pod de Grafana queda 1/1 Running.
En Connections → Data sources aparece Valkey. Pulsa Save & test y debe decir Data source is working.

Si el datasource falla, para aquí y mándame el error exacto. Es el riesgo principal de este bloque.

Paso 32: Construir el dashboard en la UI y exportarlo

Crea un dashboard nuevo y agrega los paneles con el datasource Valkey. Los datos ya existen, así que verás resultados de inmediato. Esta es la correspondencia con las claves del paso 25:

Panel requerido	Tipo	Query (comando Redis)
Máx. y mín. de aviones, máx. y mín. de barcos (4 paneles)	Stat	HGET stats max_planes, min_planes, max_ships, min_ships
Moda de aviones y de barcos	Stat	HGET stats mode_planes, mode_ships
Top países con más aviones y barcos	Bar gauge	ZRANGE top:planes y top:ships en orden descendente (ZREVRANGE, con scores)
Nombre del país asignado	Stat o Text	RUS
Total de reportes del país	Stat	HGET reports:count RUS
Serie temporal de RUS (aviones y barcos)	Time series	XRANGE series:RUS - + (campos planes y ships)

Puntos a vigilar:

Top países: en el panel Bar gauge, usa Value options → Calculation: Last y limita a 5. Si el comando de rango del plugin ofrece opciones de reversa o de rango por score, úsalas.
Serie temporal: si el plugin devuelve los campos del stream como columnas, usa la transformación Convert field type sobre planes y ships para que sean numéricos.
No puedo garantizar los nombres exactos de los campos en la UI de tu versión del plugin. Si un panel no te deja armar su query, mándame una captura y lo resolvemos. El plan B que permite el enunciado es un pequeño exporter en Go.
Pon el título “M.U.M.N.K8s MONITORING DASHBOARD” y fija el refresco en 5 s.

Exportar: Dashboard settings → JSON Model, copia todo el JSON y guárdalo:

bash
cat > helm-charts/grafanainstance/dashboards/munk8s.json
# pega el JSON, Ctrl+D

Como el datasource se aprovisiona con uid: valkey, el JSON lo referencia correctamente sin cambios.

Después redespliega el Operator como v3, para que el chart lleve el dashboard:

bash
export IMG=zot:5000/munk8s/operator:v3
make docker-build IMG=$IMG && make docker-push IMG=$IMG && make deploy IMG=$IMG
cd .. && ./scripts/cleanup.sh && cd operator
kubectl rollout status deploy/grafana -n data

✅ Verificación 32: tras el reinicio, en Dashboards → M.U.M.N.K8s aparece el dashboard sin que lo importes a mano. Esa es la prueba de que el provisioning declarativo funciona. Con Locust corriendo, los valores y la serie temporal deben moverse.

Cumplimiento del enunciado
Requisito	Estado
Operator administra Valkey y Grafana como CR independientes	✅ tras el paso 30
Helm chart de Grafana (Deployment, Service, datasource, provisioning de dashboards)	✅ Paso 29
Conexión a Valkey vía plugin	✅ tras la verificación 31
Creación declarativa mediante CR, dentro del clúster	✅ Paso 31
Dashboard con los 11 paneles requeridos	⏳ Paso 32
Imágenes (incluida la de Grafana) desde Zot	✅
Qué pegarme para continuar
bash
kubectl get pods -n data
kubectl get crd | grep munk8s
free -h

Y, sobre todo, el resultado del Save & test del datasource (¿working o error?). Con eso paso a HPA, pruebas 1 vs 2 réplicas, OCI Artifact y el manual. Si te sobra energía, también quedan Falco y Dapr, pero como extras y solo después de lo obligatorio.