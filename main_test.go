package main

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sync"
	"testing"
)

func TestMain(t *testing.T) {
	numWorkers := runtime.NumCPU()
	dir_ch := make(chan string, numWorkers)
	entries, err := os.ReadDir(`C:\Users\Anthony`)
	if err != nil {
		t.Error(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case dir := <-dir_ch:
					fmt.Println(dir)
				case <-ctx.Done():
					if len(dir_ch) == 0 {
						return
					}
				}
			}
		}()
	}

	for _, entry := range entries {
		dir_ch <- entry.Name()
	}
	cancel()
	wg.Wait()
	close(dir_ch)
}
