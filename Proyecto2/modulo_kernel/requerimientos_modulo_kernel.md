Hola estoy editando este archivo jajajaj
MÓDULO DE KERNEL
Deberá crear un módulo que capture las métricas necesarias para el análisis de la memoria
y los contenedores activos en el sistema.
La información debe ser capturada y guardada en la carpeta /proc:

1. Capturar en MB o KB (a discreción del estudiante):
   ● Total de memoria RAM
   ● Memoria RAM libre
   ● Memoria RAM en uso
2. Todos los procesos relacionados a los contenedores generados por el script así
   como los procesos generales del sistema deberán contar con:
   ● PID
   ● Nombre
   ● Línea de comando que se ejecutó o ID del contenedor
   ● VSZ (Tamaño de la memoria virtual en KB)
   ● RSS (Tamaño de memoria física en KB)
   ● Porcentaje de Memoria utilizada
   ● Porcentaje de CPU utilizado
   Sugerencias:
   ● Utilizar la estructura task_struct (del kernel de Linux) para filtrar correctamente los
   procesos relacionados con los contenedores y extraer la información necesaria.
   ● En dado caso el porcentaje de CPU sea un número extremadamente grande se
   permite mantenerlo así debido a los cálculos diferenciales que retorna el kernel.
3. Los datos deberán ser guardados en sus respectivos archivos en /proc
   ● Módulo de Procesos de Contenedores: /proc/continfo*pr2_so1*#CARNET

Nota sobre el CPU: El documento aclara que si el porcentaje de CPU resulta ser un número extremadamente grande, está permitido dejarlo así, ya que esto se debe a los cálculos diferenciales que retorna el kernel.

En resumen, vamos a crear un código en C que se "inyecta" en tu Ubuntu Server, extrae la RAM y busca en el task_struct a tus contenedores, empaquetando todo eso en un archivo de texto dentro de /proc
