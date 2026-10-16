package collector

import (
	"crypto/sha256"
	"fmt"
	"net"
	"os"
	"strings"
)

func getFileContent(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func getPrimaryMAC() string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, i := range interfaces {
		if i.Flags&net.FlagUp != 0 && i.Flags&net.FlagLoopback == 0 && len(i.HardwareAddr) > 0 {
			return i.HardwareAddr.String()
		}
	}
	return ""
}

func GenerateFingerprint() string {
	machineID := getFileContent("/etc/machine-id")
	productUUID := getFileContent("/sys/class/dmi/id/product_uuid")
	macAddress := getPrimaryMAC()
	hostname, _ := os.Hostname()

	rawString := fmt.Sprintf("%s|%s|%s|%s", machineID, productUUID, macAddress, hostname)
	hash := sha256.Sum256([]byte(rawString))
	return fmt.Sprintf("%x", hash)[:16]
}
