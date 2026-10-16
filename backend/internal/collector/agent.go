package collector

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/ugeebee/EzMonitor/backend/internal/models"
)

const DefaultServerURL = "http://localhost:8080/api/v1/ingest"

type Agent struct {
	InstanceID int
	Token      string
	ServerURL  string
	BatchSize  int
	httpClient *http.Client
}

func NewAgent(id int, token, url string, batchSize int) *Agent {
	if url == "" {
		url = DefaultServerURL
	}
	if batchSize <= 0 {
		batchSize = 5
	}
	return &Agent{
		InstanceID: id,
		Token:      token,
		ServerURL:  url,
		BatchSize:  batchSize,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

func (a *Agent) Start(ctx context.Context) error {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	var batch []models.MetricSample
	prevCPU := ReadCPUTicks()
	prevNet := ReadNetBytes()

	for {
		select {
		case <-ctx.Done():
			if len(batch) > 0 {
				a.flush(batch)
			}
			return nil
		case <-ticker.C:
			currCPU := ReadCPUTicks()
			currNet := ReadNetBytes()

			downSpeed, upSpeed := CalculateNetworkSpeeds(prevNet, currNet)

			sample := models.MetricSample{
				RecordedAt: time.Now().UTC().Truncate(time.Second),
				CPUUtil:    CalculateCPUUtil(prevCPU, currCPU),
				GPUUtil:    GetGPUUtil(),
				RAMUtil:    GetRAMUtil(),
				Uptime:     GetUptime(),
				UpSpeed:    upSpeed,
				DownSpeed:  downSpeed,
			}
			prevCPU = currCPU
			prevNet = currNet

			batch = append(batch, sample)
			log.Printf("Scraped | CPU: %.2f%% | RAM: %.2f%% | Down: %.1f KB/s | Up: %.1f KB/s",
				sample.CPUUtil, sample.RAMUtil, sample.DownSpeed, sample.UpSpeed)

			if len(batch) >= a.BatchSize {
				a.flush(batch)
				batch = nil
			}
		}
	}
}

func (a *Agent) flush(samples []models.MetricSample) {
	reqData := models.IngestBatchRequest{
		InstanceID: a.InstanceID,
		Token:      a.Token,
		Samples:    samples,
	}

	payload, err := json.Marshal(reqData)
	if err != nil {
		log.Printf("Error encoding batch JSON: %v", err)
		return
	}

	req, err := http.NewRequest("POST", a.ServerURL, bytes.NewBuffer(payload))
	if err != nil {
		log.Printf("Error creating HTTP request: %v", err)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	if a.Token != "" {
		req.Header.Set("Authorization", "Bearer "+a.Token)
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		log.Printf("Failed to flush batch to %s: %v", a.ServerURL, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		log.Printf("Server rejected batch (HTTP %d). Check server logs for constraint/auth error.", resp.StatusCode)
	} else {
		log.Printf("Successfully flushed %d sample(s) to server.", len(samples))
	}
}
