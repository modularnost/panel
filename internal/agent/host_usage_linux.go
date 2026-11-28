package agent

import (
	"bufio"
	"context"
	"io"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func ReadHostUsage(ctx context.Context) HostUsage {
	var usage HostUsage
	if mem, err := os.Open("/proc/meminfo"); err == nil {
		usage.MemoryTotal, usage.MemoryAvailable = hostMemory(mem)
		_ = mem.Close()
	}
	if path := os.Getenv("PANEL_HOST_DISK_PATH"); path != "" {
		var stat syscall.Statfs_t
		if err := syscall.Statfs(path, &stat); err == nil && stat.Blocks > 0 {
			size := stat.Frsize
			if size <= 0 {
				size = stat.Bsize
			}
			if size > 0 && stat.Bavail <= stat.Blocks {
				total, available := stat.Blocks*uint64(size), stat.Bavail*uint64(size)
				usage.DiskTotal, usage.DiskAvailable = &total, &available
			}
		}
	}
	usage.CPUPercent = hostCPU(ctx)
	return usage
}

func hostMemory(r io.Reader) (total, available *uint64) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() && (total == nil || available == nil) {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 3 || fields[2] != "kB" || (fields[0] != "MemTotal:" && fields[0] != "MemAvailable:") {
			continue
		}
		kb, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil || kb > ^uint64(0)/1024 {
			continue
		}
		bytes := kb * 1024
		if fields[0] == "MemTotal:" {
			total = &bytes
		} else {
			available = &bytes
		}
	}
	if total == nil || *total == 0 || available == nil || *available > *total {
		return nil, nil
	}
	return total, available
}

func hostCPU(ctx context.Context) *float64 {
	before, beforeIdle, ok := readCPU()
	if !ok {
		return nil
	}
	timer := time.NewTimer(200 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil
	case <-timer.C:
	}
	after, afterIdle, ok := readCPU()
	if !ok || after <= before || afterIdle < beforeIdle || afterIdle-beforeIdle > after-before {
		return nil
	}
	busy := 100 * float64((after-before)-(afterIdle-beforeIdle)) / float64(after-before)
	return &busy
}

func readCPU() (total, idle uint64, ok bool) {
	file, err := os.Open("/proc/stat")
	if err != nil {
		return 0, 0, false
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		return 0, 0, false
	}
	return cpuTicks(scanner.Text())
}

func cpuTicks(line string) (total, idle uint64, ok bool) {
	fields := strings.Fields(line)
	if len(fields) < 5 || fields[0] != "cpu" {
		return 0, 0, false
	}
	// Guest ticks are already included in user and nice.
	for i, value := range fields[1:min(len(fields), 9)] {
		ticks, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return 0, 0, false
		}
		total += ticks
		if i == 3 || i == 4 {
			idle += ticks
		}
	}
	return total, idle, true
}
