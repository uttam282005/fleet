package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"fleet/internal/api"
	"fleet/internal/config"
	"fleet/internal/device"
)

func main() {
	cfg := config.Load()

	store, err := device.NewStore(cfg.DBPath)
	if err != nil {
		log.Fatalf("failed to initialize device store: %v", err)
	}
	defer store.Close()

	handler := api.NewHandler(store)
	router := api.NewRouter(handler)

	srv := &http.Server{
		Addr:    cfg.Addr,
		Handler: router,
	}

	go func() {
		log.Printf("Fleet server listening on %s (DB: %s, Online Window: %v)", cfg.Addr, cfg.DBPath, cfg.OnlineWindow)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Println("Shutting down Fleet server...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("server forced to shutdown: %v", err)
	}
	log.Println("Fleet server stopped cleanly.")
}
