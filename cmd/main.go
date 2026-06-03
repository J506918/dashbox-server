package main

import (
	"log"
	"os"

	"dashbox/internal/api"
	"dashbox/internal/db"
	"dashbox/internal/ws"
)

func main() {
	// Database
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://dashbox:dashbox@localhost:5432/dashbox?sslmode=disable"
	}
	database, err := db.Connect(dsn)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	if err := db.Migrate(database); err != nil {
		log.Fatalf("Failed to migrate: %v", err)
	}

	// Clear stale online flags from previous run (connections don't survive restart)
	db.MarkAllOffline(database)

	// WebSocket hub
	hub := ws.NewHub(database)
	go hub.Run()

	// API router
	router := api.NewRouter(database, hub)

	// Start
	addr := os.Getenv("LISTEN_ADDR")
	if addr == "" {
		addr = ":8443"
	}
	log.Printf("DashBox server starting on %s", addr)
	log.Fatal(router.RunTLS(addr, "cert.pem", "key.pem"))
}
