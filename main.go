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
	"sort"
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
		DayLimit: 365,
		Root:     `C:\Users\Anthony\OneDrive\Documents`,
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

type fileStat struct {
	size int64
	days int
	name string
}

type stats []fileStat

func (s stats) Len() int {
	return len(s)
}

func (s stats) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func (s stats) Less(i, j int) bool {
	return s[i].days < s[j].days
}

type FileTree struct {
	fileSizeMapping map[string]fileStat
	mu              sync.Mutex
}

func NewFileTree() *FileTree {
	return &FileTree{
		fileSizeMapping: make(map[string]fileStat),
	}
}

func (f *FileTree) AddFile(con *Config, fi *fs.FileInfo, entryPath string) {
	// if file extension is part of the extension we want, add it to the map
	if _, in := con.ExtSet[filepath.Ext(entryPath)]; in {
		days := time.Since((*fi).ModTime()).Hours() / 24
		if days >= float64(con.DayLimit) {
			f.mu.Lock()
			f.fileSizeMapping[entryPath] = fileStat{
				size: (*fi).Size(),
				days: int(days),
			}
			f.mu.Unlock()
		}
	}
}

func (f *FileTree) filter() stats {
	var s stats

	for item, stat := range f.fileSizeMapping {
		s = append(s, fileStat{
			name: item,
			size: stat.size,
			days: stat.days,
		})
	}

	sort.Sort(s)
	return s
}

func (f *FileTree) FilterAndDisplay() {
	orderedStats := f.filter()

	for _, stat := range orderedStats {
		fmt.Println(stat.name, "=>", stat.size, "bytes ", stat.days, "days")
	}
}

func (f *FileTree) PrintItems() {
	for item, stats := range f.fileSizeMapping {
		fmt.Println(item, "=>", stats.size, "=>", stats.days, "days")
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

const (
	OLD_FILE_DIR = `C:\old_files`
)

func checkForOldDir() error {
	entries, err := os.ReadDir(`C:\`)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() && filepath.Join(`C:\`, entry.Name()) == OLD_FILE_DIR {
			return nil
		}
	}

	return fmt.Errorf("failed to find %s", OLD_FILE_DIR)
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

	if err := checkForOldDir(); err != nil {
		fmt.Println(err)
		if err = os.Mkdir(OLD_FILE_DIR, 0755); err != nil {
			log.Fatal(err)
		}
		fmt.Println("created", OLD_FILE_DIR)
	}

	fileTree := NewFileTree()

	start := time.Now()
	if err := FindAndGather(c, fileTree); err != nil {
		log.Fatal(err)
	}
	finish := time.Since(start)

	fileTree.FilterAndDisplay()

	fmt.Println("duration:", finish.Seconds(), "seconds")
}
