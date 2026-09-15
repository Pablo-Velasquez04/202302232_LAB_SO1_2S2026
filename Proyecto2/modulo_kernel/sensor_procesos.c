#include <linux/module.h>
#include <linux/proc_fs.h>
#include <linux/seq_file.h>
#include <linux/sched.h>     
#include <linux/sched/signal.h> 
#include <linux/mm.h>        
#include <linux/sysinfo.h>   

MODULE_LICENSE("GPL");
MODULE_AUTHOR("PabloVelasquez");
MODULE_DESCRIPTION("Modulo de Kernel - Lector de Memoria y Procesos");
MODULE_VERSION("1.0");

#define PROC_NAME "continfo_pr2_so1_202302232"

static int escribir_archivo(struct seq_file *m, void *v) {
    struct sysinfo si;
    unsigned long total_ram_mb, free_ram_mb, used_ram_mb;
    
    // Variables para procesos
    struct task_struct *task;
    struct mm_struct *mm;
    unsigned long vsz, rss, cpu_time, mem_porcentaje;

    // 1. Lógica de Memoria Global
    si_meminfo(&si);
    total_ram_mb = (si.totalram * si.mem_unit) / (1024 * 1024);
    free_ram_mb = (si.freeram * si.mem_unit) / (1024 * 1024);
    used_ram_mb = total_ram_mb - free_ram_mb;

    seq_printf(m, "=== METRICAS DEL SISTEMA ===\n");
    seq_printf(m, "Total RAM: %lu MB\n", total_ram_mb);
    seq_printf(m, "Free RAM: %lu MB\n", free_ram_mb);
    seq_printf(m, "Used RAM: %lu MB\n\n", used_ram_mb);
    
    // 2. Lógica de Procesos
    seq_printf(m, "=== METRICAS DE PROCESOS ===\n");
    // Cabeceras solicitadas (PID, Nombre, VSZ, RSS, Memoria, CPU)
    seq_printf(m, "PID\tNombre\t\tVSZ(KB)\tRSS(KB)\tMem(%%)\tCPU\n");

    for_each_process(task) {
        // Obtenemos el descriptor de memoria del proceso
        mm = get_task_mm(task);
        if (mm) {
            // Convertir páginas a KB haciendo un desplazamiento de bits (PAGE_SHIFT - 10)
            vsz = (mm->total_vm << (PAGE_SHIFT - 10)); 
            rss = (get_mm_rss(mm) << (PAGE_SHIFT - 10)); 
            
            // Cálculo de porcentaje de memoria RAM utilizada por el proceso
            mem_porcentaje = (get_mm_rss(mm) * 100) / si.totalram;

            // Uso de CPU (suma de tiempo en espacio de usuario y espacio de kernel)
            // Se mantiene como un valor absoluto grande, según lo permitido en las instrucciones
            cpu_time = task->utime + task->stime;

            // task->comm contiene el nombre del comando/proceso
            seq_printf(m, "%d\t%-15s\t%lu\t%lu\t%lu\t%lu\n",
                       task->pid, task->comm, vsz, rss, mem_porcentaje, cpu_time);

            // Importante: liberar la referencia de memoria para evitar fugas (memory leaks)
            mmput(mm); 
        }
    }
    
    return 0;
}

static int al_abrir(struct inode *inode, struct file *file) {
    return single_open(file, escribir_archivo, NULL);
}

static const struct proc_ops operaciones = {
    .proc_open    = al_abrir,
    .proc_read    = seq_read,
    .proc_lseek   = seq_lseek,
    .proc_release = single_release,
};

static int __init modulo_init(void) {
    proc_create(PROC_NAME, 0, NULL, &operaciones);
    printk(KERN_INFO "Modulo cargado: /proc/%s creado exitosamente.\n", PROC_NAME);
    return 0;
}

static void __exit modulo_exit(void) {
    remove_proc_entry(PROC_NAME, NULL);
    printk(KERN_INFO "Modulo removido: /proc/%s eliminado.\n", PROC_NAME);
}

module_init(modulo_init);
module_exit(modulo_exit);