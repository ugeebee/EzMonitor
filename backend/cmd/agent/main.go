package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"github.com/ugeebee/EzMonitor/backend/internal/collector"
	"github.com/ugeebee/EzMonitor/backend/internal/models"
)

const configFilename = "agent_config.json"
const defaultServerURL = "http://localhost:8080/api/v1/ingest"
const defaultWSURL = "ws://localhost:8080/api/v1/agent/pair/ws"

type AgentConfig struct {
	InstanceID  int64  `json:"instance_id"`
	Token       string `json:"token"`
	Fingerprint string `json:"fingerprint"`
}

func generateOTP() string {
	var b [3]byte
	_, _ = rand.Read(b[:])
	n := int(b[0])<<16 | int(b[1])<<8 | int(b[2])
	return fmt.Sprintf("%06d", n%1000000)
}

func pairWithServer(wsURL string, fingerprint string) (int64, string, error) {
	log.Printf("Connecting to EzMonitor server at %s...", wsURL)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return 0, "", fmt.Errorf("failed to connect to server WebSocket at %s: %w\n(Ensure backend server is running)", wsURL, err)
	}
	defer conn.Close()

	otp := generateOTP()
	cpuModel := collector.GetCPUName()
	ramConfig := collector.GetTotalRAMConfig()
	gpuModel := collector.GetGPUName()

	// 1. Send init message over WebSocket
	initMsg := models.AgentInitMessage{
		Type:        models.WSMsgTypeInit,
		Fingerprint: fingerprint,
		OTP:         otp,
		CPUModel:    cpuModel,
		RAMConfig:   ramConfig,
		GPUModel:    gpuModel,
	}

	if err := conn.WriteJSON(initMsg); err != nil {
		return 0, "", fmt.Errorf("failed to send handshake to server: %w", err)
	}

	fmt.Println("==================================================")
	fmt.Println("             EzMonitor Agent Setup                ")
	fmt.Println("==================================================")
	fmt.Printf("\nHardware Fingerprint : %s", fingerprint)
	fmt.Printf("\nPairing OTP Code     : %s  (Valid for 5 minutes)", otp)
	fmt.Printf("\nDetected CPU         : %s", cpuModel)
	fmt.Printf("\nDetected RAM         : %s", ramConfig)
	fmt.Printf("\nDetected GPU         : %s\n\n", gpuModel)
	fmt.Println("--------------------------------------------------")
	fmt.Println("1. Open the EzMonitor Dashboard.")
	fmt.Println("2. Go to your Subgroup -> 'Pair Instance'.")
	fmt.Println("3. Enter the Fingerprint and OTP above to authorize.")
	fmt.Println("--------------------------------------------------")
	fmt.Println("[Status] Waiting for admin approval over WebSocket...")

	// 2. Await server response with 5-minute timeout
	conn.SetReadDeadline(time.Now().Add(5 * time.Minute))

	for {
		var rawMsg map[string]interface{}
		if err := conn.ReadJSON(&rawMsg); err != nil {
			return 0, "", fmt.Errorf("connection closed while waiting for pairing: %w", err)
		}

		msgType, _ := rawMsg["type"].(string)

		switch msgType {
		case "waiting":
			// Acknowledged by server
			continue

		case models.WSMsgTypePaired:
			// Admin verified OTP!
			instanceIDFloat, _ := rawMsg["instance_id"].(float64)
			instanceID := int64(instanceIDFloat)
			token, _ := rawMsg["token"].(string)
			label, _ := rawMsg["label"].(string)

			if instanceID == 0 || token == "" {
				return 0, "", fmt.Errorf("received invalid credentials payload from server")
			}

			fmt.Printf("\n[Success] Pairing verified by Admin!")
			fmt.Printf("\nAssigned Instance ID : %d", instanceID)
			fmt.Printf("\nAssigned Label       : %s\n", label)

			return instanceID, token, nil

		case models.WSMsgTypeTimeout:
			errStr, _ := rawMsg["error"].(string)
			return 0, "", fmt.Errorf("pairing timed out: %s", errStr)

		case models.WSMsgTypeError:
			errStr, _ := rawMsg["error"].(string)
			return 0, "", fmt.Errorf("server pairing error: %s", errStr)
		}
	}
}

func loadOrSetupConfig(wsURL string) AgentConfig {
	var cfg AgentConfig
	currentFingerprint := collector.GenerateFingerprint()

	data, err := os.ReadFile(configFilename)
	if err == nil {
		if err := json.Unmarshal(data, &cfg); err == nil && cfg.InstanceID != 0 && cfg.Token != "" {
			if cfg.Fingerprint == currentFingerprint {
				return cfg
			}

			fmt.Println("WARNING: Hardware change detected (New MAC, Hostname, or OS ID).")
			fmt.Println("Previous configuration invalidated. Instance must be re-paired.")
			fmt.Println()
		}
	}

	// Unpaired: initiate WebSocket OTP pairing with server
	instanceID, token, err := pairWithServer(wsURL, currentFingerprint)
	if err != nil {
		log.Fatalf("\nPairing aborted: %v", err)
	}

	cfg = AgentConfig{
		InstanceID:  instanceID,
		Token:       token,
		Fingerprint: currentFingerprint,
	}

	fileData, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(configFilename, fileData, 0600); err != nil {
		log.Fatalf("Failed to save configuration to disk: %v", err)
	}

	fmt.Printf("\nSetup complete! Configuration saved to %s.\n", configFilename)
	fmt.Println("==================================================")

	return cfg
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "EzMonitor Agent - Lightweight Metrics Scraper\n\n")
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  agent [flags]\n\n")
		fmt.Fprintf(os.Stderr, "Available Flags:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nNotes:\n")
		fmt.Fprintf(os.Stderr, "  - On first run, the agent generates a fingerprint and OTP and connects via WebSocket.\n")
		fmt.Fprintf(os.Stderr, "  - Admin enters the fingerprint and OTP in the web dashboard to pair the instance.\n")
		fmt.Fprintf(os.Stderr, "  - Credentials are automatically saved to %s.\n", configFilename)
	}

	batchSize := flag.Int("batch-size", 5, "Number of metrics to batch before flushing")
	wsURL := flag.String("ws-url", defaultWSURL, "WebSocket pairing URL for the backend server")
	serverURL := flag.String("server-url", defaultServerURL, "Ingest URL for flushing metric batches")
	flag.Parse()

	cfg := loadOrSetupConfig(*wsURL)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("\nShutting down gracefully...")
		cancel()
	}()

	agent := collector.NewAgent(cfg.InstanceID, cfg.Token, *serverURL, *batchSize)

	log.Printf("Starting EzMonitor Agent (Instance ID: %d)", cfg.InstanceID)
	if err := agent.Start(ctx); err != nil {
		log.Fatalf("Agent encountered an error: %v", err)
	}
}
