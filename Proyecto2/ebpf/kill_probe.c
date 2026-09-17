// +build ignore

#include <linux/bpf.h>
#include <linux/ptrace.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>

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
int trace_sys_enter_kill(struct trace_event_raw_sys_enter *ctx) {
    // Leer argumentos del tracepoint: arg[0]=pid, arg[1]=sig
    __u32 target_pid = (__u32)ctx->args[0];
    __s32 signal     = (__s32)ctx->args[1];

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
    e->sender_pid = bpf_get_current_pid_tgid() >> 32;
    e->target_pid = target_pid;
    e->signal     = signal;

    // Enviar el evento al espacio de usuario
    bpf_ringbuf_submit(e, 0);
    return 0;
}

char LICENSE[] SEC("license") = "GPL";
