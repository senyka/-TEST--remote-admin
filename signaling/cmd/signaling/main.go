package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/yourorg/dwservice-clone/signaling/internal/handler"
	"github.com/yourorg/dwservice-clone/signaling/internal/store"
)

func main() {
	// Parse flags
	addr := flag.String("addr", ":8443", "HTTPS listen address")
	certFile := flag.String("cert", "cert.pem", "TLS certificate file")
	keyFile := flag.String("key", "key.pem", "TLS key file")
	dbDSN := flag.String("db", "postgres://user:pass@localhost/dwservice?sslmode=disable", "PostgreSQL DSN")
	redisAddr := flag.String("redis", "localhost:6379", "Redis address")
	flag.Parse()

	// Initialize stores
	dbStore, err := store.NewPostgresStore(*dbDSN)
	if err != nil {
		log.Fatalf("Failed to connect to PostgreSQL: %v", err)
	}
	defer dbStore.Close()

	redisStore, err := store.NewRedisStore(*redisAddr)
	if err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}
	defer redisStore.Close()

	// Create handler with dependencies
	h := handler.NewHandler(dbStore, redisStore)

	// Setup HTTP routes
	http.HandleFunc("/ws", h.WSHandler)
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Graceful shutdown
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan
		log.Println("Shutting down...")
		os.Exit(0)
	}()

	// Start server
	log.Printf("Starting signaling server on %s", *addr)
	log.Printf("Open https://localhost%s to access the dashboard", *addr)
	if err := http.ListenAndServeTLS(*addr, *certFile, *keyFile, nil); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
