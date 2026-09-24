package main

import (
	"log"
	"net/http"

	"fleet/internal/api"
	"fleet/internal/device"
)

func main() {
	dbPath := "fleet.db"
	store, err := device.NewStore(dbPath)
	if err != nil {
		log.Fatalf("failed to initialize device store: %v", err)
	}
	defer store.Close()

	handler := api.NewHandler(store)
	router := api.NewRouter(handler)

	addr := ":8080"
	log.Printf("Fleet server listening on %s", addr)
	if err := http.ListenAndServe(addr, router); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server exited with error: %v", err)
	}
}
