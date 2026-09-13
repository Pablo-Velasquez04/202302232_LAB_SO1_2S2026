<div align="center">

<img src="Imagenes/logoUSAC.png" width="150">

## UNIVERSIDAD DE SAN CARLOS DE GUATEMALA

### FACULTAD DE INGENIERÍA

### SISTEMAS OPERATIVOS 1

<br><br>

# GUÍA DE INSTALACIÓN

<br><br>

**Catedrático:** Ing. Sergio Arnaldo Méndez Aguilar

**Auxiliar:** Elian Angel Fernando Reyes Yac

**Nombre:** Pablo Daniel Velásquez Hernández

**Carnet:** 202302232

**Sección:** P

**Semestre:** Segundo Semestre 2026

<br><br>

</div>

# Guía de Instalación y Uso - Proyecto Sistemas Operativos 1

¡Bienvenido! Esta guía está diseñada paso a paso para que **cualquier persona**, incluso sin experiencia previa en Docker, Containerd o Podman, pueda levantar y probar esta arquitectura de microservicios distribuidos.

---

## 1. Requisitos Previos

Antes de empezar, necesitas tener lo siguiente preparado en tu entorno:

1. **Computadora Base (Host):** Una máquina con Linux (ej. Ubuntu) y **Docker** instalado. Aquí descargarás el código de las APIs.
2. **Tres Máquinas Virtuales (VMs):** Creadas en KVM con una distribución ligera (ej. Alpine Linux). Cada una debe tener instalada una herramienta específica:
   - **VM1:** `containerd` y `nerdctl` instalados.
   - **VM2:** `podman` y `iptables` instalados.
   - **VM3:** `docker` instalado.
3. **Red Local:** Todas las VMs y tu computadora base deben poder comunicarse por red (estar en la misma subred de KVM).

> **Nota sobre las Direcciones IP:**
> En este tutorial usaremos IPs de ejemplo. **Deberás reemplazarlas** por las IPs reales que te asigne KVM a tus máquinas virtuales:
>
> - **IP_VM1:** `192.168.122.121` (Host de API 1 y API 2)
> - **IP_VM2:** `192.168.122.41` (Host de API 3)
> - **IP_VM3:** `192.168.122.229` (Host de ZOT Registry)

---

## Paso 1: Levantar el Registro Privado (ZOT) en la VM3

En esta arquitectura no subimos nuestras aplicaciones a internet. Usamos un "almacén privado" llamado **ZOT** que guardará nuestras imágenes dentro de nuestra propia red.

1. Abre una terminal y conéctate por SSH a tu **VM3**:
   ```bash
   ssh root@192.168.122.229
   ```
2. Ejecuta el siguiente comando para encender ZOT en el puerto 5000:
   ```bash
   docker run -d -p 5000:5000 --name zot-registry ghcr.io/project-zot/zot-linux-amd64:latest
   ```
   _(`-d` significa que correrá en segundo plano, y `-p` abre los puertos)._

---

## Paso 2: Construir e Inyectar las Imágenes (Desde tu Computadora Base)

Para no saturar el almacenamiento de nuestras pequeñas VMs, vamos a "empaquetar" las aplicaciones (construir las imágenes) en tu computadora principal y se las enviaremos directamente a la memoria de la VM3.

1. Abre una terminal en tu **computadora principal (Ubuntu)** y navega a la carpeta del proyecto.
2. **Para la API 1:**
   ```bash
   cd API1
   # Construimos la imagen (el "molde" de nuestra aplicación)
   docker build -t localhost:5000/api1-202302232:latest .
   # La enviamos directo al Docker de la VM3 a través de SSH
   docker save localhost:5000/api1-202302232:latest | ssh root@192.168.122.229 'docker load'
   ```
3. **Para la API 2:**
   ```bash
   cd ../API2
   docker build -t localhost:5000/api2-202302232:latest .
   docker save localhost:5000/api2-202302232:latest | ssh root@192.168.122.229 'docker load'
   ```
4. **Para la API 3:**
   ```bash
   cd ../API3
   docker build -t localhost:5000/api3-202302232:latest .
   docker save localhost:5000/api3-202302232:latest | ssh root@192.168.122.229 'docker load'
   ```

Una vez transferidas, **vuelve a la terminal de tu VM3** (la que dejamos abierta en el Paso 1) y guárdalas permanentemente en el almacén ZOT ejecutando:

```bash
docker push localhost:5000/api1-202302232:latest
docker push localhost:5000/api2-202302232:latest
docker push localhost:5000/api3-202302232:latest
```

---

## Paso 3: Encender API 1 y API 2 en la VM1 (Containerd)

La **VM1** es la encargada de ejecutar las dos primeras APIs usando `nerdctl` (una herramienta idéntica a Docker pero para Containerd).

1. Abre una nueva terminal y conéctate por SSH a tu **VM1**:
   ```bash
   ssh root@192.168.122.121
   ```
2. Descarga (pull) las imágenes desde el ZOT de tu VM3. Como ZOT no usa certificado seguro (HTTPS), debemos usar `--insecure-registry`:
   ```bash
   sudo nerdctl pull --insecure-registry 192.168.122.229:5000/api1-202302232:latest
   sudo nerdctl pull --insecure-registry 192.168.122.229:5000/api2-202302232:latest
   ```
3. **Inicia la API 1** (Le decimos dónde encontrar a la API 2 y API 3):
   ```bash
   sudo nerdctl run -d --name api1 -p 8081:8080 \
     -e API2_URL="http://192.168.122.121:8082/health" \
     -e API3_URL="http://192.168.122.41:8083/health" \
     192.168.122.229:5000/api1-202302232:latest
   ```
4. **Inicia la API 2** (Le decimos dónde encontrar a la API 1 y API 3):
   ```bash
   sudo nerdctl run -d --name api2 -p 8082:8080 \
     -e API1_URL="http://192.168.122.121:8081/health" \
     -e API3_URL="http://192.168.122.41:8083/health" \
     192.168.122.229:5000/api2-202302232:latest
   ```

---

## Paso 4: Encender API 3 en la VM2 (Podman)

La **VM2** utiliza `podman`, que es otra alternativa moderna a Docker.
_(Nota: Si usas Alpine Linux, asegúrate de haber instalado iptables con `apk add iptables` antes de continuar)._

1. Abre una nueva terminal y conéctate por SSH a tu **VM2**:
   ```bash
   ssh root@192.168.122.41
   ```
2. Descarga la imagen de la API 3 desde la VM3 (aquí la bandera insegura se llama diferente):
   ```bash
   sudo podman pull --tls-verify=false 192.168.122.229:5000/api3-202302232:latest
   ```
3. **Inicia la API 3**:
   ```bash
   sudo podman run -d --name api3 -p 8083:8080 \
     -e API1_URL="http://192.168.122.121:8081/health" \
     -e API2_URL="http://192.168.122.121:8082/health" \
     192.168.122.229:5000/api3-202302232:latest
   ```

---

## Paso 5: Probar que todo funciona

Las APIs están ejecutándose aisladas en sus propias máquinas, pero conectadas por red.

Abre una terminal en tu computadora base y usa el comando `curl` (o abre los enlaces en tu navegador) para simular peticiones. Cada API intentará llamar a la otra usando los enlaces internos que les configuramos.

**1. Estado de Salud (Health Check) de la API 1:**

```bash
curl http://192.168.122.121:8081/health
```

_(Debe devolver: `"status":"UP"` y tus datos)._

**2. Prueba de Comunicación (VM1 -> VM1): La API 1 le habla a la API 2:**

```bash
curl http://192.168.122.121:8081/api1/202302232/call-api2
```

_(Debe devolver: `"connection": true` confirmando el enlace)._

**3. Prueba de Comunicación (VM1 -> VM2): La API 1 le habla a la API 3:**

```bash
curl http://192.168.122.121:8081/api1/202302232/call-api3
```

**4. Prueba de Comunicación de Regreso (VM2 -> VM1): La API 3 le habla a la API 1:**

```bash
curl http://192.168.122.41:8083/api3/202302232/call-api1
```

---

## ¿Qué pasa si apago mis máquinas virtuales?

Las configuraciones e imágenes **no se borran**, se guardan en el disco duro de tus máquinas virtuales permanentemente.

Si en el futuro reinicias tus VMs y quieres volver a encender tu proyecto, **NO tienes que repetir todo este manual**. Solo debes hacer lo siguiente para "despertar" tus contenedores:

1. Entra a la **VM3** y enciende ZOT: `docker start zot-registry`
2. Entra a la **VM1** y enciende las APIs: `sudo nerdctl start api1 api2`
3. Entra a la **VM2** y enciende la API: `sudo podman start api3`

Y todo volverá a estar en línea.
