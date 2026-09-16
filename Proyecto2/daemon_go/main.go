package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

// Ruta del archivo generado por tu módulo en C
const procFilePath = "/proc/continfo_pr2_so1_202302232"

// Estructura para almacenar la información de Docker
type DockerInfo struct {
	ID    string
	Name  string
	Image string
}

// Estructura para almacenar la información limpia de cada contenedor
type ContenedorData struct {
	ID         string
	Name       string
	Image      string
	PID        int
	VSZ        int
	RSS        int
	MemPercent int
	CPU        int
}

func main() {
	fmt.Println("========== Iniciando Daemon (Fase 1 y 2) ==========")

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		fmt.Printf("Error al crear cliente Docker: %v\n", err)
		return
	}

	// 1. Obtener la relación PID -> ID del contenedor desde Docker
	dockerMap, err := mapearContenedores(cli)
	if err != nil {
		fmt.Printf("Error al conectar con Docker: %v\n", err)
		return
	}
	fmt.Printf("Se encontraron %d contenedores activos en Docker.\n", len(dockerMap))

	// 2. Leer archivo /proc y cruzar los datos
	contenedores, err := leerYFiltrarProc(procFilePath, dockerMap)
	if err != nil {
		fmt.Printf("Error al leer el archivo /proc: %v\n", err)
		return
	}

	// 3. Mostrar los resultados procesados
	fmt.Println("\n========== Contenedores Filtrados y Parseados ==========")
	for _, c := range contenedores {
		fmt.Printf("ID: %s | Nombre: %s | PID: %d | RSS: %d KB | CPU: %d\n", c.ID, c.Name, c.PID, c.RSS, c.CPU)
	}

	// 4. Fase 2: Gestionar contenedores según las reglas
	fmt.Println("\n========== Fase 2: Gestión de Contenedores ==========")
	gestionarContenedores(cli, contenedores)
}

// mapearContenedores devuelve un diccionario donde la llave es el PID y el valor es la info de Docker
func mapearContenedores(cli *client.Client) (map[int]DockerInfo, error) {
	lista, err := cli.ContainerList(context.Background(), container.ListOptions{})
	if err != nil {
		return nil, err
	}

	mapa := make(map[int]DockerInfo)
	for _, cont := range lista {
		info, err := cli.ContainerInspect(context.Background(), cont.ID)
		if err == nil && info.State != nil {
			// Extraer solo los primeros 12 caracteres del ID para mayor legibilidad
			idCorto := cont.ID[:12]
			mapa[info.State.Pid] = DockerInfo{
				ID:    idCorto,
				Name:  strings.TrimPrefix(info.Name, "/"),
				Image: info.Config.Image,
			}
		}
	}
	return mapa, nil
}

// leerYFiltrarProc lee el archivo del kernel, descarta texto inútil y extrae métricas
func leerYFiltrarProc(ruta string, dockerMap map[int]DockerInfo) ([]ContenedorData, error) {
	archivo, err := os.Open(ruta)
	if err != nil {
		return nil, err
	}
	defer archivo.Close()

	var contenedoresFiltrados []ContenedorData
	scanner := bufio.NewScanner(archivo)

	// Bandera para saber cuándo empezamos a leer las métricas de procesos
	leyendoProcesos := false

	for scanner.Scan() {
		linea := strings.TrimSpace(scanner.Text())

		// Ignorar líneas vacías
		if linea == "" {
			continue
		}

		// Detectar inicio de tabla de procesos y saltar encabezados
		if strings.Contains(linea, "PID\tNombre") {
			leyendoProcesos = true
			continue
		}

		if leyendoProcesos {
			// Dividir la línea por espacios o tabulaciones
			columnas := strings.Fields(linea)
			if len(columnas) >= 6 {
				pidStr := columnas[0]
				pid, _ := strconv.Atoi(pidStr)

				// Verificar si el PID existe en nuestro mapa de Docker
				if dInfo, existe := dockerMap[pid]; existe {
					vsz, _ := strconv.Atoi(columnas[2])
					rss, _ := strconv.Atoi(columnas[3])
					memPercent, _ := strconv.Atoi(columnas[4])
					cpuVal, _ := strconv.Atoi(columnas[5])

					// Almacenar el dato estructurado
					datos := ContenedorData{
						ID:         dInfo.ID,
						Name:       dInfo.Name,
						Image:      dInfo.Image,
						PID:        pid,
						VSZ:        vsz,
						RSS:        rss,
						MemPercent: memPercent,
						CPU:        cpuVal,
					}
					contenedoresFiltrados = append(contenedoresFiltrados, datos)
				}
			}
		}
	}

	return contenedoresFiltrados, scanner.Err()
}

// gestionarContenedores aplica el algoritmo de selección y eliminación según reglas
func gestionarContenedores(cli *client.Client, contenedores []ContenedorData) {
	var bajoConsumo []ContenedorData
	var altoConsumo []ContenedorData
	var intrusos []ContenedorData

	for _, c := range contenedores {
		img := strings.ToLower(c.Image)
		name := strings.ToLower(c.Name)

		if strings.Contains(img, "bajo-consumo") || strings.Contains(name, "bajo-consumo") {
			bajoConsumo = append(bajoConsumo, c)
		} else if strings.Contains(img, "alto-cpu") || strings.Contains(img, "go-client") || strings.Contains(name, "alto-cpu") || strings.Contains(name, "go-client") {
			altoConsumo = append(altoConsumo, c)
		} else if strings.Contains(img, "intruso") || strings.Contains(name, "intruso") {
			intrusos = append(intrusos, c)
		}
	}

	// Función para ordenar (mayor consumo primero)
	ordenar := func(lista []ContenedorData) {
		sort.Slice(lista, func(i, j int) bool {
			// El ordenamiento prioriza: Memoria -> RSS -> VSZ -> CPU
			if lista[i].MemPercent != lista[j].MemPercent {
				return lista[i].MemPercent > lista[j].MemPercent
			}
			if lista[i].RSS != lista[j].RSS {
				return lista[i].RSS > lista[j].RSS
			}
			if lista[i].VSZ != lista[j].VSZ {
				return lista[i].VSZ > lista[j].VSZ
			}
			return lista[i].CPU > lista[j].CPU
		})
	}

	ordenar(bajoConsumo)
	ordenar(altoConsumo)

	matar := func(c ContenedorData, razon string) {
		fmt.Printf("-> Matando contenedor %s (ID: %s) - Razón: %s\n", c.Name, c.ID, razon)
		err := cli.ContainerKill(context.Background(), c.ID, "SIGKILL")
		if err != nil {
			fmt.Printf("   Error matando %s: %v\n", c.ID, err)
		} else {
			fmt.Printf("   Contenedor %s terminado exitosamente.\n", c.ID)
		}
	}

	// Regla: Siempre deben existir 3 contenedores de bajo consumo
	if len(bajoConsumo) > 3 {
		for i := 0; i < len(bajoConsumo)-3; i++ {
			matar(bajoConsumo[i], "Exceso de bajo consumo (optimización de recursos)")
		}
	}

	// Regla: Siempre deben existir 2 contenedores de alto consumo
	if len(altoConsumo) > 2 {
		for i := 0; i < len(altoConsumo)-2; i++ {
			matar(altoConsumo[i], "Exceso de alto consumo (optimización de recursos)")
		}
	}

	// Regla: Los intrusos deben eliminarse
	for _, c := range intrusos {
		matar(c, "Contenedor intruso detectado")
	}
}
