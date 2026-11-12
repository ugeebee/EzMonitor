package models

import "time"

type MetricSample struct {
	RecordedAt  time.Time `json:"recorded_at"`
	CPUUtil     float64   `json:"cpu_util"`
	GPUUtil     *float64  `json:"gpu_util"`
	Uptime      int64     `json:"uptime"`
	NetUpKBPS   float64   `json:"net_up_kbps"`
	NetDownKBPS float64   `json:"net_down_kbps"`
}

type IngestBatchRequest struct {
	InstanceID  int64          `json:"i_id"`
	Fingerprint string         `json:"fingerprint"`
	Token       string         `json:"token,omitempty"`
	CPUModel    string         `json:"cpu_model,omitempty"`
	RAMConfig   string         `json:"ram_config,omitempty"`
	GPUModel    string         `json:"gpu_model,omitempty"`
	Samples     []MetricSample `json:"samples"`
}
