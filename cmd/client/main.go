// cmd/client/main.go
//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"time"

	"shmem-webview/internal/config"
	"shmem-webview/internal/ipc"
)

func main() {
	fmt.Println("Go client starting...")

	// 設定ファイル読み込み
	cfg, err := config.Load("config.json")
	if err != nil {
		panic(err)
	}

	// 共有メモリ（client は create=false）
	shm, err := ipc.OpenSharedMemory(cfg, false)
	if err != nil {
		panic(err)
	}
	defer shm.Close()

	seq := 0

	for {
		seq++
		msg := map[string]any{
			"seq":  seq,
			"time": time.Now().Format(time.RFC3339),
		}
		payload, _ := json.Marshal(msg)

		if err := shm.Write(payload, cfg.PollTimeoutMs); err != nil {
			fmt.Println("Write error:", err)
		} else {
			fmt.Println("Wrote:", string(payload))
		}

		time.Sleep(1 * time.Second)
	}
}
