package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type registerPayload struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type heartbeatPayload struct {
	Status         string  `json:"status"`
	CPUUsage       float64 `json:"cpu_usage"`
	SignalStrength float64 `json:"signal_strength"`
}

func main() {
	baseURL := flag.String("url", "http://localhost:8080", "server base URL")
	n := flag.Int("n", 5, "number of devices to simulate")
	interval := flag.Duration("interval", 5*time.Second, "heartbeat interval")
	deviceID := flag.String("id", "", "optional single device ID to run (e.g. device-03)")
	flag.Parse()

	client := &http.Client{Timeout: 5 * time.Second}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("\n[SIM] Shutting down simulator...")
		cancel()
	}()

	// Single-device mode
	if *deviceID != "" {
		runSingleDevice(ctx, client, *baseURL, *deviceID, fmt.Sprintf("Device %s", *deviceID), *interval)
		return
	}

	// Multi-device mode
	if *n < 1 {
		log.Fatalf("device count -n must be at least 1")
	}

	var wg sync.WaitGroup
	deviceCancels := make(map[int]context.CancelFunc)
	deviceIDs := make(map[int]string)
	var mu sync.Mutex

	fmt.Printf("[SIM] Starting Fleet Simulator with %d devices targeting %s (heartbeat every %v)\n", *n, *baseURL, *interval)

	for i := 1; i <= *n; i++ {
		id := fmt.Sprintf("device-%02d", i)
		name := fmt.Sprintf("Lab Sensor %02d", i)

		registerDevice(client, *baseURL, id, name)

		devCtx, devCancel := context.WithCancel(ctx)
		mu.Lock()
		deviceCancels[i] = devCancel
		deviceIDs[i] = id
		mu.Unlock()

		wg.Add(1)
		go func(idx int, dID string, dCtx context.Context) {
			defer wg.Done()
			runDeviceLoop(dCtx, client, *baseURL, dID, *interval)
		}(i, id, devCtx)
	}

	fmt.Println("[SIM] All devices registered and heartbeats running.")
	fmt.Println("[SIM] Interactive command:")
	fmt.Printf("      Type a device number (1-%d) and press Enter to stop that device.\n", *n)
	fmt.Println("      Press Ctrl+C to terminate the simulator.")

	// Interactive stdin reader to simulate killing an individual device
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			text := strings.TrimSpace(scanner.Text())
			if text == "" {
				continue
			}
			num, err := strconv.Atoi(text)
			if err != nil || num < 1 || num > *n {
				fmt.Printf("[SIM] Invalid input '%s'. Enter a device number between 1 and %d.\n", text, *n)
				continue
			}

			mu.Lock()
			devCancel, exists := deviceCancels[num]
			id := deviceIDs[num]
			if exists && devCancel != nil {
				devCancel()
				delete(deviceCancels, num)
				fmt.Printf("\n>>> [SIM] Device [%d] %s STOPPED! It should transition to OFFLINE in ~30s. <<<\n\n", num, id)
			} else {
				fmt.Printf("[SIM] Device [%d] is already stopped.\n", num)
			}
			mu.Unlock()
		}
	}()

	<-ctx.Done()
	wg.Wait()
	fmt.Println("[SIM] Simulator exited cleanly.")
}

func runSingleDevice(ctx context.Context, client *http.Client, baseURL, id, name string, interval time.Duration) {
	fmt.Printf("[SIM] Starting single device %s (%s) targeting %s\n", id, name, baseURL)
	registerDevice(client, baseURL, id, name)
	runDeviceLoop(ctx, client, baseURL, id, interval)
	fmt.Println("[SIM] Device stopped.")
}

func registerDevice(client *http.Client, baseURL, id, name string) {
	url := fmt.Sprintf("%s/devices", baseURL)
	payload, _ := json.Marshal(registerPayload{ID: id, Name: name})

	resp, err := client.Post(url, "application/json", bytes.NewReader(payload))
	if err != nil {
		log.Printf("[SIM] Failed to connect to server at %s: %v", url, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusCreated {
		log.Printf("[SIM] Registered device: %s (%s)", id, name)
	} else if resp.StatusCode == http.StatusConflict {
		log.Printf("[SIM] Device %s already registered (ready)", id)
	} else {
		log.Printf("[SIM] Unexpected status registering %s: %d", id, resp.StatusCode)
	}
}

func runDeviceLoop(ctx context.Context, client *http.Client, baseURL, id string, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Send initial heartbeat immediately
	sendHeartbeat(client, baseURL, id)

	for {
		select {
		case <-ctx.Done():
			log.Printf("[SIM] [%s] Heartbeat loop terminated", id)
			return
		case <-ticker.C:
			sendHeartbeat(client, baseURL, id)
		}
	}
}

func sendHeartbeat(client *http.Client, baseURL, id string) {
	url := fmt.Sprintf("%s/devices/%s/heartbeat", baseURL, id)

	// Generate realistic simulated metrics
	cpu := float64(rand.Intn(600)+150) / 10.0      // 15.0% - 75.0%
	signal := -1 * float64(rand.Intn(40)+45)        // -45 to -85 dBm

	payload, _ := json.Marshal(heartbeatPayload{
		Status:         "OK",
		CPUUsage:       cpu,
		SignalStrength: signal,
	})

	resp, err := client.Post(url, "application/json", bytes.NewReader(payload))
	if err != nil {
		log.Printf("[SIM] [%s] Heartbeat send failed: %v", id, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("[SIM] [%s] Heartbeat received non-200 status: %d", id, resp.StatusCode)
	} else {
		log.Printf("[SIM] [%s] Heartbeat OK (CPU: %.1f%%, Signal: %.0f dBm)", id, cpu, signal)
	}
}
