//go:build ebpf

package main

// Este archivo le indica a `go generate` que compile el programa eBPF en C
// y genere los stubs de Go para cargar y usar el objeto BPF compilado.
//
// Para compilar, ejecuta desde la carpeta daemon_go/:
//   go generate -tags ebpf ./...
//
// Requisitos en el sistema (Ubuntu):
//   sudo apt install clang llvm libbpf-dev linux-headers-$(uname -r)
//
// Si aparece el error 'asm/types.h not found', es porque clang necesita
// las cabeceras de la arquitectura. El flag -I/usr/include/x86_64-linux-gnu
// resuelve esto en Ubuntu x86_64.

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -strip "" -cc clang -cflags "-O2 -g -Wall -target bpf -I/usr/include/x86_64-linux-gnu" killProbe ../ebpf/kill_probe.c
