//go:build !ebpf

package main

import "github.com/redis/go-redis/v9"

// iniciarSondaEBPF es un stub que se usa cuando el Daemon se compila
// sin la sonda eBPF (sin el build tag 'ebpf').
// Para activar la sonda real, ejecuta:
//   go generate ./...                          (requiere clang/llvm)
//   go build -tags ebpf -o daemon .            (compila con soporte eBPF)
//   sudo ./daemon                              (requiere privilegios de root)
func iniciarSondaEBPF(rdb *redis.Client) {
	_ = rdb
	// En modo sin eBPF, el Daemon funciona pero no auditará sys_kill
	// ni confirmará eliminaciones vía kernel. Las métricas se guardan
	// en Valkey igual, pero sin el flujo eBPF->Valkey.
}
