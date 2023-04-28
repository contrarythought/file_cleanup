package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

type Config struct {
	ExtSet   map[string]bool `json:"extset"`
	DayLimit uint            `json:"daylimit"`
	Root     string          `json:"root"`
}

func DefaultConfig() *Config {
	return &Config{
		ExtSet: map[string]bool{
			".pdf":  true,
			".doc":  true,
			".docx": true,
			".exe":  true,
			".xls":  true,
			".xlsx": true,
			".txt":  true,
			".go":   true,
		},
		DayLimit: 30,
		Root:     `input root path`,
	}
}

func GetConfig(file *os.File) (*Config, error) {
	var c Config
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func getNumWorkers() int {
	return runtime.NumCPU()
}

type FileTree struct {
	FileSizeMapping map[string]struct {
		Size int64
		Days int
	}
	mu sync.Mutex
}

func NewFileTree() *FileTree {
	return &FileTree{
		FileSizeMapping: make(map[string]struct {
			Size int64
			Days int
		}),
	}
}

func (f *FileTree) AddFile(con *Config, fi *fs.FileInfo, entryPath string) {
	// if file extension is part of the extension we want, add it to the map
	if _, in := con.ExtSet[filepath.Ext(entryPath)]; in {
		f.mu.Lock()
		f.FileSizeMapping[entryPath] = struct {
			Size int64
			Days int
		}{
			Size: (*fi).Size(),
			Days: int(time.Since((*fi).ModTime()).Hours() / 24),
		}
		f.mu.Unlock()
	}
}

func (f *FileTree) PrintItems() {
	for item, stats := range f.FileSizeMapping {
		fmt.Println(item, "=>", stats.Size, "=>", stats.Days, "days")
	}
}

func crawlDir(path string, fileTree *FileTree, con *Config) error {
	var wg sync.WaitGroup

	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		entryPath := filepath.Join(path, entry.Name())
		if entry.IsDir() {
			wg.Add(1)
			go func(entryPath string) {
				defer wg.Done()
				crawlDir(entryPath, fileTree, con)
			}(entryPath)
			wg.Wait()
		} else {
			fi, err := os.Stat(entryPath)
			if err != nil {
				return err
			}

			fileTree.AddFile(con, &fi, entryPath)
		}
	}
	return nil
}

func FindAndGather(con *Config, fileTree *FileTree) error {
	numWorkers := getNumWorkers()
	dir_ch := make(chan string, numWorkers)
	ctx, cancel := context.WithCancel(context.Background())

	var wg sync.WaitGroup

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case dir := <-dir_ch:
					if err := crawlDir(dir, fileTree, con); err != nil {
						panic(err)
					}
				case <-ctx.Done():
					if len(dir_ch) == 0 {
						return
					}
				}
			}
		}()
	}

	entries, err := os.ReadDir(con.Root)
	if err != nil {
		cancel()
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			dir_ch <- filepath.Join(con.Root, entry.Name())
		} else {
			entryPath := filepath.Join(con.Root, entry.Name())
			fi, err := os.Stat(entryPath)
			if err != nil {
				cancel()
				return err
			}

			fileTree.AddFile(con, &fi, entryPath)
		}
	}

	cancel()
	wg.Wait()
	close(dir_ch)
	return nil
}

func main() {
	var c *Config
	if len(os.Args) == 2 {
		configFile, err := os.Open("config.json")
		if err != nil {
			log.Fatal(err)
		}
		defer configFile.Close()

		c, err = GetConfig(configFile)
		if err != nil {
			log.Fatal(err)
		}
	} else if len(os.Args) == 1 {
		c = DefaultConfig()
	} else {
		fmt.Println("usage <opt: config file>")
		os.Exit(0)
	}

	fileTree := NewFileTree()

	start := time.Now()
	if err := FindAndGather(c, fileTree); err != nil {
		log.Fatal(err)
	}
	finish := time.Since(start)

	fileTree.PrintItems()

	fmt.Println("duration:", finish.Seconds(), "seconds")
}
