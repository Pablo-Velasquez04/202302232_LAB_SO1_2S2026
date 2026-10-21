Revisión del dashboard: 9 de 11 paneles correctos y los dos Top siguen mal

Hay un gran avance: la serie temporal ya funciona. Pero tu resumen dice que los Top muestran nombres y van ordenados, y tu captura no lo confirma.

Panel por panel
Panel	Valor	Veredicto
Máx. aviones / barcos	50 / 30	✅ Coincide con los rangos
Mín. aviones / barcos	0 / 0	✅
Moda aviones / barcos	18 / 17	✅ Plausible, y puede quedarse estable con muchos datos
País asignado	RUS, carnet 202302232	✅
Total reportes RUS	54415	✅ Subió desde 47474, así que se está actualizando
Top aviones	5 barras	❌ Cada barra dice “Value” y no van ordenadas
Top barcos	5 barras	❌ Mismo problema
Serie temporal RUS	Dos líneas con leyenda	✅ Funciona, con una duda menor
Los números de los Top ahora sí cuadran

Antes te dije que no cuadraban. Ahora, con 54 415 reportes de RUS:

54 415 × 25 (promedio de aviones) ≈ 1.36 M. Las barras van de 1.347 M a 1.359 M.
54 415 × 15 (promedio de barcos) ≈ 816 k. Las barras van de 806 k a 820 k.

Los datos son correctos y la sospecha anterior quedó descartada. Falla solo la presentación.

Por qué los Top siguen mal
Sin nombre de país. Cada barra dice “Value”, y el enunciado pide un top por país (su ejemplo es “1. USA 850”). Sin etiqueta, nadie sabe qué barra es qué país.
Sin ordenar. Van 1350118, 1359374, 1356999, 1347623, 1357245. Un top debe ir de mayor a menor.
Cada barra tiene un color distinto, y eso delata que Grafana las trata como 5 series separadas. Las transformaciones seriesToRows + sortBy que describes deberían fusionarlas en una sola tabla con una columna de país, y eso no se ve en la captura.

Posibles causas, en orden de probabilidad:

La captura es del dashboard anterior y el v8 aún no se cargó. Haz kubectl rollout restart deploy/grafana -n data, espera a que quede 1/1, y recarga con Ctrl+Shift+R.
Las transformaciones no están surtiendo efecto, porque no se fusionan las 5 queries. Abre el panel → Table view y comprueba si ves una sola tabla con columnas de país y valor, o 5 tablas separadas.

Si siguen separadas, arreglo directo: como son 5 queries (A a E), usa Overrides → Fields returned by query → A → Display name = RUS (y así B, C, D, E con su país). Eso pone el nombre sin depender de transformaciones. Para ordenarlas, el bar gauge no ordena entre series distintas, así que lo limpio es una sola tabla fusionada (Transform → Merge, luego Sort by). Si no lo logras en 20 min, avísame y lo resolvemos cambiando el consumer para guardar los acumulados en un hash. Eso da una sola query HGETALL que ya devuelve país y valor en una sola tabla.

La serie temporal: funciona, con dos observaciones
El panel ya dibuja planes y ships de RUS, con leyenda correcta (“Aviones en Aire – RUS” y “Barcos en Mar – RUS”). Cumple lo que pide el enunciado.
A la derecha se ve un bloque denso y “ruidoso”: son miles de reportes por minuto y cada uno es un punto, así que parece una mancha. Es normal. Con Locust apagado, la gráfica será más legible.
Duda menor: a la izquierda hay una línea plana exactamente en 25 y 15, que son justo los promedios teóricos. Puede ser una interpolación entre un punto viejo y el bloque nuevo, pero no estoy seguro. Mira el Table view de ese panel y comprueba que las primeras filas tengan valores reales. Si son valores aleatorios normales, es solo cosmética. Si todos valen 25 y 15, hay un dato espurio.
Para el manual: el stream guarda unas 5000 entradas, así que bajo carga solo ves los últimos minutos. Documéntalo como limitación.
Antes de dar el dashboard por cerrado
Arregla los Top (nombre y orden), con tope de 20-30 min.
Prueba de persistencia, la que más importa:
bash
   kubectl delete pod -n data -l app.kubernetes.io/name=grafana
   kubectl rollout status deploy/grafana -n data

Recarga con Ctrl+Shift+R. El dashboard debe volver idéntico, con nombres y orden. Si vuelve con “Value”, el JSON del chart no tiene tus cambios.
3. Captura final con Locust corriendo, como evidencia.
4. Commit: git add proyecto3 && git commit -m "p3: dashboard completo".

Siguiente paso

Hoy es 21 y la entrega es mañana. Después de los Top, sigue con el paso 33 (benchmark 1 vs 2 réplicas), el 34 (HPA) y el 35 (OCI Artifact) del mensaje anterior. Corre pkill -f locust antes del benchmark, y ./scripts/pull-config.sh antes si ya cambiaste el locustfile.py.

Pégame la captura de los Top corregidos (o el Table view si siguen mal) y el resultado de:

bash
cat docs/bench/summary.md
kubectl get hpa -n apps
free -h