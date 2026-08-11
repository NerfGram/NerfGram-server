//go:build !windows

package hoststats

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func memStats() (used, total int64, err error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()

	var totalKB, availKB int64
	haveTotal, haveAvail := false, false
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "MemTotal:"):
			totalKB, err = parseMeminfoKB(line)
			haveTotal = err == nil
		case strings.HasPrefix(line, "MemAvailable:"):
			availKB, err = parseMeminfoKB(line)
			haveAvail = err == nil
		}
		if haveTotal && haveAvail {
			break
		}
	}
	if scanErr := scanner.Err(); scanErr != nil {
		return 0, 0, scanErr
	}
	if !haveTotal || !haveAvail {
		return 0, 0, fmt.Errorf("hoststats: MemTotal/MemAvailable not found in /proc/meminfo")
	}
	total = totalKB * 1024
	used = total - availKB*1024
	if used < 0 {
		used = 0
	}
	return used, total, nil
}

func parseMeminfoKB(line string) (int64, error) {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return 0, fmt.Errorf("hoststats: malformed /proc/meminfo line %q", line)
	}
	return strconv.ParseInt(fields[1], 10, 64)
}
