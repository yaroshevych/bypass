//go:build !darwin && !linux

package main

func disableCoreDumps() error { return nil }
