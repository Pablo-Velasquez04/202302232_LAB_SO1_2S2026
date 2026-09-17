//go:build ebpf

package main

// Este archivo le indica a `go generate` que compile el programa eBPF en C
// y genere los stubs de Go para cargar y usar el objeto BPF compilado.
//
// Para compilar, ejecuta desde la carpeta daemon_go/:
//   go generate ./...
//
// Requisitos en el sistema (Ubuntu):
//   sudo apt install clang llvm libbpf-dev linux-headers-$(uname -r)

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags "-O2 -g -Wall -target bpf" killProbe ../ebpf/kill_probe.c
