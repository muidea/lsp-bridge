package main

import (
	"log"
	"os"

	"lsp-bridge/internal/bridge"
)

func main() {
	logger := log.New(os.Stderr, "lsp-bridge: ", log.LstdFlags|log.Lshortfile)
	if err := bridge.Run(logger); err != nil {
		logger.Fatalf("server exited: %v", err)
	}
}
