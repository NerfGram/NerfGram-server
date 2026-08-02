//go:build !darwin && !linux

package loadharness

func validateProcessCapacity(int) error { return nil }
