// +build ignore

// Incluimos solo las cabeceras de libbpf (instaladas con libbpf-dev)
// Evitamos <linux/bpf.h> y <linux/ptrace.h> que dependen de cabeceras
// de arquitectura que no siempre están en el path de clang.
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>

// Definimos manualmente el contexto del tracepoint sys_enter_kill.
// Esta es la forma estándar de evitar dependencias de cabeceras del kernel
// cuando se compila con clang para BPF en Ubuntu.
struct sys_enter_kill_args {
    unsigned short common_type;
    unsigned char  common_flags;
    unsigned char  common_preempt_count;
    int            common_pid;
    int            __syscall_nr;
    long           pid;   // args[0]: PID del proceso objetivo
    long           sig;   // args[1]: Señal a enviar
};

// Estructura del evento que enviaremos al espacio de usuario (Go)
struct kill_event {
    unsigned int sender_pid;  // PID del proceso que llamó a sys_kill (emisor)
    unsigned int target_pid;  // PID del proceso objetivo (receptor / contenedor)
    int          signal;      // Señal enviada (9=SIGKILL, 15=SIGTERM)
};

// RingBuffer para enviar eventos al espacio de usuario de forma eficiente
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 256 * 1024); // 256 KB
} events SEC(".maps");

// Se engancha al tracepoint justo ANTES de ejecutar sys_kill
SEC("tracepoint/syscalls/sys_enter_kill")
int trace_sys_enter_kill(struct sys_enter_kill_args *ctx) {
    unsigned int target_pid = (unsigned int)ctx->pid;
    int          signal     = (int)ctx->sig;

    // Solo nos interesan las señales de terminación (SIGKILL=9 o SIGTERM=15)
    if (signal != 9 && signal != 15) {
        return 0;
    }

    // Reservar espacio en el RingBuffer
    struct kill_event *e = bpf_ringbuf_reserve(&events, sizeof(struct kill_event), 0);
    if (!e) {
        return 0;
    }

    // Llenar los datos del evento
    e->sender_pid = (unsigned int)(bpf_get_current_pid_tgid() >> 32);
    e->target_pid = target_pid;
    e->signal     = signal;

    // Enviar el evento al espacio de usuario
    bpf_ringbuf_submit(e, 0);
    return 0;
}

char LICENSE[] SEC("license") = "GPL";
