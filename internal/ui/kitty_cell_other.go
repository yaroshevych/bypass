//go:build !darwin && !linux

package ui

func previewCellSize(uintptr) (float64, float64) { return 1, 2 }
