//go:build !windows

package hoststats

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// cpuSampler computes CPU busy% from successive cumulative /proc/stat
// snapshots.
type cpuSampler struct {
	prevTotal uint64
	prevIdle  uint64
	have      bool
}

func (c *cpuSampler) sample() float64 {
	total, idle, err := readProcStatCPU()
	if err != nil {
		return 0
	}
	if !c.have {
		c.prevTotal, c.prevIdle = total, idle
		c.have = true
		return 0
	}
	deltaTotal := total - c.prevTotal
	deltaIdle := idle - c.prevIdle
	c.prevTotal, c.prevIdle = total, idle
	if deltaTotal == 0 {
		return 0
	}
	pct := (1 - float64(deltaIdle)/float64(deltaTotal)) * 100
	switch {
	case pct < 0:
		pct = 0
	case pct > 100:
		pct = 100
	}
	return pct
}

func readProcStatCPU() (total, idle uint64, err error) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, 0, err
	}
	line, _, _ := strings.Cut(string(data), "\n")
	fields := strings.Fields(line)
	if len(fields) < 5 || fields[0] != "cpu" {
		return 0, 0, fmt.Errorf("hoststats: unexpected /proc/stat format")
	}
	values := make([]uint64, 0, len(fields)-1)
	for _, f := range fields[1:] {
		v, err := strconv.ParseUint(f, 10, 64)
		if err != nil {
			return 0, 0, err
		}
		values = append(values, v)
		total += v
	}
	idle = values[3]
	if len(values) > 4 {
		idle += values[4]
	}
	return total, idle, nil
}
