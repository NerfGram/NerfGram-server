//go:build windows

package hoststats

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var procGetSystemTimes = modkernel32.NewProc("GetSystemTimes")

// cpuSampler computes CPU busy% from successive cumulative GetSystemTimes
// snapshots.
type cpuSampler struct {
	prevIdle, prevKernel, prevUser uint64
	have                           bool
}

func (c *cpuSampler) sample() float64 {
	idle, kernel, user, ok := getSystemTimes()
	if !ok {
		return 0
	}
	if !c.have {
		c.prevIdle, c.prevKernel, c.prevUser = idle, kernel, user
		c.have = true
		return 0
	}
	deltaIdle := idle - c.prevIdle
	deltaTotal := (kernel - c.prevKernel) + (user - c.prevUser)
	c.prevIdle, c.prevKernel, c.prevUser = idle, kernel, user
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

func getSystemTimes() (idle, kernel, user uint64, ok bool) {
	var idleFT, kernelFT, userFT windows.Filetime
	r, _, _ := procGetSystemTimes.Call(
		uintptr(unsafe.Pointer(&idleFT)),
		uintptr(unsafe.Pointer(&kernelFT)),
		uintptr(unsafe.Pointer(&userFT)),
	)
	if r == 0 {
		return 0, 0, 0, false
	}
	return filetimeToUint64(idleFT), filetimeToUint64(kernelFT), filetimeToUint64(userFT), true
}

func filetimeToUint64(ft windows.Filetime) uint64 {
	return uint64(ft.HighDateTime)<<32 | uint64(ft.LowDateTime)
}
