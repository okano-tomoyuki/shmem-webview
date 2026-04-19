// cmd/server/main.go
//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/debug"
	"golang.org/x/sys/windows/svc/eventlog"

	"shmem-webview/internal/config"
	"shmem-webview/internal/ipc"
)

const serviceName = "MyGoIpcService"

// ======== WebSocket Hub ========

type WebSocketHub struct {
	clients map[*websocket.Conn]bool
	mu      sync.Mutex
}

func NewWebSocketHub() *WebSocketHub {
	return &WebSocketHub{
		clients: make(map[*websocket.Conn]bool),
	}
}

func (h *WebSocketHub) Register(conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[conn] = true
}

func (h *WebSocketHub) Unregister(conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients, conn)
}

func (h *WebSocketHub) Broadcast(msg string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for c := range h.clients {
		if err := c.WriteMessage(websocket.TextMessage, []byte(msg)); err != nil {
			log.Println("WS send error:", err)
			c.Close()
			delete(h.clients, c)
		}
	}
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func websocketHandler(hub *WebSocketHub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Println("Upgrade error:", err)
			return
		}
		hub.Register(conn)
		log.Println("Client connected")

		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				break
			}
		}

		hub.Unregister(conn)
		conn.Close()
		log.Println("Client disconnected")
	}
}

// ======== サービス本体ロジック ========

func runCore(stopCh <-chan struct{}, cfg *config.ServerConfig) {
	// 共有メモリ（server は create=true）
	shm, err := ipc.OpenSharedMemory(cfg, true)
	if err != nil {
		log.Println("SharedMemory init error:", err)
		return
	}
	defer shm.Close()

	hub := NewWebSocketHub()

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", websocketHandler(hub))

	addr := fmt.Sprintf("0.0.0.0:%d", cfg.WsPort)
	srv := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	go func() {
		log.Printf("WebSocket server on ws://%s/ws", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Println("HTTP server error:", err)
		}
	}()

	ticker := time.NewTicker(time.Duration(cfg.ShmemPollMs) * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-stopCh:
			log.Println("Stopping core loop...")
			_ = srv.Close()
			return

		case <-ticker.C:
			data, err := shm.Read(cfg.PollTimeoutMs)
			if err != nil {
				log.Println("Read error:", err)
				continue
			}
			if data == nil {
				// timeout / データなし → 何もしない
				continue
			}

			text := string(data)

			var obj any
			if err := json.Unmarshal(data, &obj); err == nil {
				log.Println("[SHM] JSON:", obj)
			} else {
				log.Println("[SHM] RAW:", text)
			}

			hub.Broadcast(text)
		}
	}
}

// ======== EventLog Writer / Windows サービス部分はそのまま ========

type EventLogWriter struct {
	elog *eventlog.Log
}

func (w *EventLogWriter) Write(p []byte) (n int, err error) {
	msg := string(p)
	err = w.elog.Info(1, msg)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

type myService struct {
	cfg *config.ServerConfig
}

func (m *myService) Execute(args []string, r <-chan svc.ChangeRequest, s chan<- svc.Status) (bool, uint32) {
	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown
	s <- svc.Status{State: svc.StartPending}

	stopCh := make(chan struct{})
	go runCore(stopCh, m.cfg)

	s <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}

	for c := range r {
		switch c.Cmd {
		case svc.Interrogate:
			s <- c.CurrentStatus
		case svc.Stop, svc.Shutdown:
			close(stopCh)
			s <- svc.Status{State: svc.StopPending}
			return false, 0
		}
	}
	return false, 0
}

func main() {
	cfg, err := config.Load("config.json")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	isService, err := svc.IsWindowsService()
	if err != nil {
		log.Fatalf("IsWindowsService: %v", err)
	}

	if !isService {
		// コンソールモード
		log.SetOutput(os.Stdout)
		log.Println("Running in console mode")
		stopCh := make(chan struct{})
		go runCore(stopCh, cfg)

		log.Println("Press Enter to exit...")
		fmt.Scanln()
		close(stopCh)
		return
	}

	elog, err := eventlog.Open(serviceName)
	if err == nil {
		log.SetOutput(&EventLogWriter{elog})
		defer elog.Close()
	} else {
		log.SetOutput(os.Stdout)
	}

	run := svc.Run
	if !isService {
		run = debug.Run
	}

	if err := run(serviceName, &myService{cfg: cfg}); err != nil {
		log.Fatalf("Service failed: %v", err)
	}
}
