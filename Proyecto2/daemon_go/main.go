package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

// Ruta del archivo generado por tu módulo en C
const procFilePath = "/proc/continfo_pr2_so1_202302232"

// Estructura para almacenar la información limpia de cada contenedor
type ContenedorData struct {
	ID         string
	PID        int
	VSZ        int
	RSS        int
	MemPercent int
	CPU        int
}

func main() {
	fmt.Println("========== Iniciando Daemon (Fase 1) ==========")

	// 1. Obtener la relación PID -> ID del contenedor desde Docker
	dockerMap, err := mapearContenedores()
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
		fmt.Printf("ID: %s | PID: %d | Mem(%%): %d | CPU: %d\n", c.ID, c.PID, c.MemPercent, c.CPU)
	}
}

// mapearContenedores devuelve un diccionario donde la llave es el PID y el valor es el ID del contenedor
func mapearContenedores() (map[int]string, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}

	lista, err := cli.ContainerList(context.Background(), container.ListOptions{})
	if err != nil {
		return nil, err
	}

	mapa := make(map[int]string)
	for _, cont := range lista {
		info, err := cli.ContainerInspect(context.Background(), cont.ID)
		if err == nil && info.State != nil {
			// Extraer solo los primeros 12 caracteres del ID para mayor legibilidad
			idCorto := cont.ID[:12]
			mapa[info.State.Pid] = idCorto
		}
	}
	return mapa, nil
}

// leerYFiltrarProc lee el archivo del kernel, descarta texto inútil y extrae métricas
func leerYFiltrarProc(ruta string, dockerMap map[int]string) ([]ContenedorData, error) {
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
				if idContenedor, existe := dockerMap[pid]; existe {
					vsz, _ := strconv.Atoi(columnas[2])
					rss, _ := strconv.Atoi(columnas[3])
					memPercent, _ := strconv.Atoi(columnas[4])
					cpuVal, _ := strconv.Atoi(columnas[5])

					// Almacenar el dato estructurado
					datos := ContenedorData{
						ID:         idContenedor,
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
