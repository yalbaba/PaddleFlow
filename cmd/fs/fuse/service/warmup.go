package service

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/panjf2000/ants/v2"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"

	"github.com/PaddlePaddle/PaddleFlow/pkg/common/utils"
)

var pool *ants.Pool

func warmup(ctx *cli.Context) error {
	fname := ctx.String("file")
	paths := ctx.Args().Slice()
	threads := int(ctx.Uint("threads"))
	warmType := ctx.String("type")
	recursive := ctx.Bool("recursive")
	pool, _ = ants.NewPool(threads)
	return warmup_(fname, paths, threads, warmType, recursive)
}

func warmup_(fname string, paths []string, threads int, warmType string, recursive bool) error {
	now := time.Now()
	fd, err := os.Open(fname)
	if err != nil {
		log.Errorf("Failed to open file %s: %s", fname, err)
		return err
	}
	defer func() {
		_ = fd.Close()
	}()
	scanner := bufio.NewScanner(fd)
	for scanner.Scan() {
		if p := strings.TrimSpace(scanner.Text()); p != "" {
			paths = append(paths, p)
		}
	}
	if err = scanner.Err(); err != nil {
		log.Errorf("Reading file %s failed with error: %s", fname, err)
		return err
	}
	if len(paths) == 0 {
		log.Infof("Nothing to warm up")
		return nil
	}

	progress, bar := utils.NewDynProgressBar("warming up paths: ", false, int64(len(paths)))
	for _, path := range paths {
		path_ := path
		if recursive && strings.HasSuffix(path_, "/") {
			concurrentRecursiveWalk(path_)
		}
		_ = pool.Submit(func() {
			if strings.HasSuffix(path_, "/") {
				warmupDir(path_)
			} else {
				if warmType == "meta" {
					warmupMeta(path_)
				} else if warmType == "data" {
					warmupData(path_)
				} else {
					log.Fatal("type of warmup must meta or data")
				}
			}
			bar.IncrBy(1)
		})
	}
	progress.Wait()
	fmt.Printf("spend time %v \n", time.Since(now))
	return nil
}

func warmupMeta(fileName string) {
	_, err := os.Stat(fileName)
	if err != nil {
		log.Errorf("Stat file %s with error: %v", fileName, err)
	}
}

func warmupDir(dirName string) {
	_, err := os.ReadDir(dirName)
	if err != nil {
		log.Errorf("ReadDir %s with error: %v", dirName, err)
	}
}

func warmupData(fileName string) {
	fh, err := os.Open(fileName)
	if err != nil {
		log.Errorf("Open file %s with error: %v", fileName, err)
		return
	}
	_, err = ioutil.ReadAll(fh)
	if err != nil {
		log.Errorf("ReadAll file %s with error: %v", fileName, err)
	}
}

func CmdWarmup() *cli.Command {
	return &cli.Command{
		Name:      "warmup",
		Category:  "TOOL",
		Usage:     "Build cache for target directories/files",
		ArgsUsage: "[PATH ...]",
		Action:    warmup,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "file",
				Required: true,
				Aliases:  []string{"f"},
				Usage:    "file containing a list of paths",
			},
			&cli.UintFlag{
				Name:    "threads",
				Aliases: []string{"p"},
				Value:   100,
				Usage:   "number of concurrent workers",
			},
			&cli.StringFlag{
				Name:    "type",
				Aliases: []string{"t"},
				Value:   "meta",
				Usage:   "type of warmup, e.g. meta, data",
			},
			&cli.BoolFlag{
				Name:    "recursive",
				Aliases: []string{"r"},
				Value:   false,
				Usage:   "enable recursive preheating",
			},
		},
	}
}

func concurrentRecursiveWalk(root string) error {
	// 启动多个 goroutine 并发递归遍历和处理任务
	err := walkAndProcess(root)
	if err != nil {
		return err
	}
	return nil

}

func walkAndProcess(path string) error {
	dirEntries, err := os.ReadDir(path)
	if err != nil {
		fmt.Printf("warmup path[%s] fail with err %v \n", path, err)
		return err
	}
	fmt.Printf("warmup path[%v] success \n", path)
	var wg sync.WaitGroup
	poolDir, _ := ants.NewPool(10)
	for _, entry := range dirEntries {
		fullPath := filepath.Join(path, entry.Name())
		entry_ := entry
		if entry_.IsDir() {
			wg.Add(1)
			// 递归调用
			_ = poolDir.Submit(func() {
				defer wg.Done()
				walkAndProcess(fullPath)
			})
		}
	}
	wg.Wait()
	return nil
}
