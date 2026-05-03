package system

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Spittingjiu/Sing-Matrix/backend/internal/models"
)

func Status() models.SystemStatus {
	used, total := memInfo()
	pid := findProcess("sing-box")
	return models.SystemStatus{
		CPUPercent:      loadAvg(),
		MemoryUsed:      used,
		MemoryTotal:     total,
		UptimeSeconds:   uptime(),
		SingBoxRunning:  pid > 0,
		SingBoxPID:      pid,
		GeneratedAtUnix: time.Now().Unix(),
	}
}

func memInfo() (uint64, uint64) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	vals := map[string]uint64{}
	s := bufio.NewScanner(f)
	for s.Scan() {
		parts := strings.Fields(s.Text())
		if len(parts) >= 2 {
			v, _ := strconv.ParseUint(parts[1], 10, 64)
			vals[strings.TrimSuffix(parts[0], ":")] = v * 1024
		}
	}
	total := vals["MemTotal"]
	avail := vals["MemAvailable"]
	return total - avail, total
}

func uptime() uint64 {
	b, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(b))
	if len(fields) == 0 {
		return 0
	}
	f, _ := strconv.ParseFloat(fields[0], 64)
	return uint64(f)
}

func loadAvg() float64 {
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(b))
	if len(fields) == 0 {
		return 0
	}
	v, _ := strconv.ParseFloat(fields[0], 64)
	return v
}

func findProcess(name string) int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		comm, err := os.ReadFile("/proc/" + e.Name() + "/comm")
		if err == nil && strings.TrimSpace(string(comm)) == name {
			return pid
		}
	}
	return 0
}
