// cmd/client/main.go
//go:build windows

package main

import (
    "encoding/json"
    "log"
    "os"
    "time"

    "shmem-webview/internal/config"
    "shmem-webview/internal/ipc"
)

func main() {
    log.SetOutput(os.Stdout)
    log.Println("Go client starting...")

    cfg, err := config.Load("config.json")
    if err != nil {
        log.Fatalf("Failed to load config: %v", err)
    }

    shm, err := ipc.OpenSharedMemory(cfg, false) // client は create=false
    if err != nil {
        log.Fatalf("OpenSharedMemory failed: %v", err)
    }
    defer shm.Close()

    seq := 0
    ticker := time.NewTicker(1 * time.Second)
    defer ticker.Stop()

    for range ticker.C {
        seq++
        msg := map[string]any{
            "seq":  seq,
            "time": time.Now().Format(time.RFC3339),
        }
        data, err := json.Marshal(msg)
        if err != nil {
            log.Println("JSON marshal error:", err)
            continue
        }

        ok, err := shm.TryWrite(data, cfg.PollTimeoutMs)
        if err != nil {
            log.Println("Write error:", err)
            continue
        }
        if !ok {
            // mutex が取れなかった（timeout）→ 今回はスキップ
            log.Println("Write skipped: mutex busy (timeout)")
            continue
        }

        log.Printf("Wrote: %s", string(data))
    }
}
