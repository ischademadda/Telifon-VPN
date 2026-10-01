package main

import (
	"fmt"
	"runtime/debug"
)

func init() {
	// Darwin RSS optimization: soft memory limit at 96 MB
	debug.SetMemoryLimit(96 * 1024 * 1024)
	// More frequent GC to release memory aggressively under Darwin
	debug.SetGCPercent(30)
}

func main() {
	fmt.Println("telifon-cored v1.0.0 (Phase 0 scaffolding) ready.")
}
