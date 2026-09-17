// +build ignore

// Definimos los tipos básicos manualmente para evitar depender de linux/types.h
// y asm/types.h, los cuales causan problemas de rutas en diferentes versiones de Ubuntu.
typedef unsigned char __u8;
typedef short int __s16;
typedef short unsigned int __u16;
typedef int __s32;
typedef unsigned int __u32;
typedef long long int __s64;
typedef long long unsigned int __u64;

#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>

// Definimos manualmente el contexto del tracepoint sys_enter_kill.
// Esta es la forma estándar y portable de acceder a los argumentos
// del tracepoint sin depender de cabeceras privadas del kernel.
struct sys_enter_kill_args {
    __u64 pad;          // common fields (type, flags, preempt_count, pid, __syscall_nr)
    long  pid;          // args[0]: PID del proceso objetivo
    long  sig;          // args[1]: Señal a enviar
};

// Estructura del evento que enviaremos al espacio de usuario (Go)
struct kill_event {
    __u32 sender_pid;   // PID del proceso que llamó a sys_kill (emisor)
    __u32 target_pid;   // PID del proceso objetivo (receptor / contenedor)
    __s32 signal;       // Señal enviada (9=SIGKILL, 15=SIGTERM)
};

// RingBuffer para enviar eventos al espacio de usuario de forma eficiente
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 256 * 1024); // 256 KB
} events SEC(".maps");

// Se engancha al tracepoint justo ANTES de ejecutar sys_kill
SEC("tracepoint/syscalls/sys_enter_kill")
int trace_sys_enter_kill(struct sys_enter_kill_args *ctx) {
    __u32 target_pid = (__u32)ctx->pid;
    __s32 signal     = (__s32)ctx->sig;

    // Solo nos interesan señales de terminación: SIGKILL=9 o SIGTERM=15
    if (signal != 9 && signal != 15) {
        return 0;
    }

    // Reservar espacio en el RingBuffer
    struct kill_event *e = bpf_ringbuf_reserve(&events, sizeof(struct kill_event), 0);
    if (!e) {
        return 0;
    }

    // Llenar los datos del evento
    e->sender_pid = (__u32)(bpf_get_current_pid_tgid() >> 32);
    e->target_pid = target_pid;
    e->signal     = signal;

    // Enviar el evento al espacio de usuario (Daemon de Go)
    bpf_ringbuf_submit(e, 0);
    return 0;
}

char LICENSE[] SEC("license") = "GPL";
