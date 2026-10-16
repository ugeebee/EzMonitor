package collector

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

type CPUState struct {
	Total float64
	Idle  float64
}

type NetState struct {
	RxBytes uint64
	TxBytes uint64
}

func ReadCPUTicks() CPUState {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return CPUState{}
	}
	lines := strings.Split(string(data), "\n")
	fields := strings.Fields(lines[0])

	var total, idle float64
	for i, val := range fields {
		if i == 0 {
			continue
		}
		tick, _ := strconv.ParseFloat(val, 64)
		total += tick
		if i == 4 || i == 5 {
			idle += tick
		}
	}
	return CPUState{Total: total, Idle: idle}
}

func CalculateCPUUtil(prev, current CPUState) float64 {
	totalDelta := current.Total - prev.Total
	idleDelta := current.Idle - prev.Idle
	if totalDelta > 0 {
		return (1.0 - (idleDelta / totalDelta)) * 100.0
	}
	return 0.0
}

func ReadNetBytes() NetState {
	data, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return NetState{}
	}

	lines := strings.Split(string(data), "\n")
	var totalRx, totalTx uint64

	for _, line := range lines {
		if !strings.Contains(line, ":") {
			continue
		}
		parts := strings.Split(line, ":")
		ifName := strings.TrimSpace(parts[0])

		if ifName == "lo" {
			continue
		}

		fields := strings.Fields(parts[1])
		if len(fields) >= 9 {
			rx, _ := strconv.ParseUint(fields[0], 10, 64)
			tx, _ := strconv.ParseUint(fields[8], 10, 64)
			totalRx += rx
			totalTx += tx
		}
	}
	return NetState{RxBytes: totalRx, TxBytes: totalTx}
}

func CalculateNetworkSpeeds(prev, curr NetState) (float64, float64) {
	var downDelta, upDelta float64
	if curr.RxBytes >= prev.RxBytes {
		downDelta = float64(curr.RxBytes - prev.RxBytes)
	}
	if curr.TxBytes >= prev.TxBytes {
		upDelta = float64(curr.TxBytes - prev.TxBytes)
	}
	return downDelta / 1024.0, upDelta / 1024.0
}

func GetRAMUtil() float64 {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0.0
	}
	var total, available float64
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if fields[0] == "MemTotal:" {
			total, _ = strconv.ParseFloat(fields[1], 64)
		} else if fields[0] == "MemAvailable:" {
			available, _ = strconv.ParseFloat(fields[1], 64)
		}
	}
	if total > 0 {
		return ((total - available) / total) * 100.0
	}
	return 0.0
}

func GetUptime() int64 {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) > 0 {
		uptime, _ := strconv.ParseFloat(fields[0], 64)
		return int64(uptime)
	}
	return 0
}

func GetGPUUtil() float64 {
	cmd := exec.Command("nvidia-smi", "--query-gpu=utilization.gpu", "--format=csv,noheader,nounits")
	out, err := cmd.Output()
	if err != nil {
		return 0.0
	}
	val, _ := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	return val
}

func GetCPUName() string {
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return "Unknown CPU"
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "model name") {
			parts := strings.Split(line, ":")
			if len(parts) > 1 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return "Unknown CPU"
}

func GetTotalRAMConfig() string {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return "Unknown RAM"
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				kb, _ := strconv.ParseInt(fields[1], 10, 64)
				return fmt.Sprintf("%.1f GB", float64(kb)/1024.0/1024.0)
			}
		}
	}
	return "Unknown RAM"
}

func GetGPUName() string {
	cmd := exec.Command("nvidia-smi", "--query-gpu=name", "--format=csv,noheader")
	out, err := cmd.Output()
	if err != nil {
		return "Integrated / Non-NVIDIA GPU"
	}
	return strings.TrimSpace(string(out))
}
