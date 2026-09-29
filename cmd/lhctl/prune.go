package main

import (
	"fmt"
	"os"

	"github.com/divmora/localharness/internal/conversation"
)

func runPrune(dataDir string) {
	convMgr, err := conversation.NewManager(dataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot create conversation manager: %v\n", err)
		os.Exit(1)
	}

	count, err := convMgr.PruneEmpty()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to prune empty conversations: %v\n", err)
		os.Exit(1)
	}

	if count == 0 {
		fmt.Println("No empty conversations found.")
	} else if count == 1 {
		fmt.Println("Pruned 1 empty conversation.")
	} else {
		fmt.Printf("Pruned %d empty conversations.\n", count)
	}
}
