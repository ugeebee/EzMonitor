package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/ugeebee/EzMonitor/backend/internal/collector"
)

const configFilename = "agent_config.json"
const defaultServerURL = "http://localhost:8080/api/v1/ingest"

type AgentConfig struct {
	InstanceID  int    `json:"instance_id"`
	Token       string `json:"token"`
	Fingerprint string `json:"fingerprint"`
}

func loadOrSetupConfig() AgentConfig {
	var cfg AgentConfig
	currentFingerprint := collector.GenerateFingerprint()

	data, err := os.ReadFile(configFilename)
	if err == nil {
		if err := json.Unmarshal(data, &cfg); err == nil && cfg.InstanceID != 0 && cfg.Token != "" {
			if cfg.Fingerprint == currentFingerprint {
				return cfg
			}

			fmt.Println("WARNING: Hardware change detected (New MAC, Hostname, or OS ID).")
			fmt.Println("Previous configuration invalidated. Instance must be re-registered.")
			fmt.Println()
		}
	}

	fmt.Println("==================================================")
	fmt.Println("             EzMonitor Agent Setup                ")
	fmt.Println("==================================================")

	fmt.Printf("\nHardware Fingerprint : %s", currentFingerprint)
	fmt.Printf("\nDetected CPU         : %s", collector.GetCPUName())
	fmt.Printf("\nDetected RAM         : %s", collector.GetTotalRAMConfig())
	fmt.Printf("\nDetected GPU         : %s\n\n", collector.GetGPUName())

	fmt.Println("1. Keep this terminal open.")
	fmt.Println("2. Go to the EzMonitor React UI.")
	fmt.Println("3. Register the instance using the fingerprint above.")
	fmt.Println("--------------------------------------------------")

	var instanceID int
	fmt.Print("Enter the integer Instance ID provided by the UI: ")
	if _, err := fmt.Scan(&instanceID); err != nil || instanceID == 0 {
		log.Fatalf("Invalid Instance ID. Setup aborted.")
	}

	var token string
	fmt.Print("Enter the Secret Token provided by the UI: ")
	if _, err := fmt.Scan(&token); err != nil || token == "" {
		log.Fatalf("Invalid Token. Setup aborted.")
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
		fmt.Fprintf(os.Stderr, "  - On first run, the agent will launch an interactive setup to register the instance.\n")
		fmt.Fprintf(os.Stderr, "  - Credentials are automatically saved to %s.\n", configFilename)
		fmt.Fprintf(os.Stderr, "  - If hardware changes are detected, you will be prompted to re-register.\n")
	}

	batchSize := flag.Int("batch-size", 5, "Number of metrics to batch before flushing")
	flag.Parse()

	cfg := loadOrSetupConfig()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("\nShutting down gracefully...")
		cancel()
	}()

	agent := collector.NewAgent(cfg.InstanceID, cfg.Token, defaultServerURL, *batchSize)

	log.Printf("Starting EzMonitor Agent (Instance ID: %d)", cfg.InstanceID)
	if err := agent.Start(ctx); err != nil {
		log.Fatalf("Agent encountered an error: %v", err)
	}
}
