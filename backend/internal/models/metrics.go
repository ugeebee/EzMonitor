package models

import "time"

type MetricSample struct {
	RecordedAt time.Time `json:"recorded_at"`
	CPUUtil    float64   `json:"cpu_util"`
	GPUUtil    float64   `json:"gpu_util"`
	RAMUtil    float64   `json:"ram_util"`
	Uptime     int64     `json:"uptime"`
	UpSpeed    float64   `json:"up_speed"`   // KB/s
	DownSpeed  float64   `json:"down_speed"` // KB/s
}

type IngestBatchRequest struct {
	InstanceID  int            `json:"instance_id"`
	Fingerprint string         `json:"fingerprint"`
	Token       string         `json:"token,omitempty"`
	Samples     []MetricSample `json:"samples"`
}
