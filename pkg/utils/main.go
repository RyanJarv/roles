package utils

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"github.com/dlsniper/debugger"
	"io"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

const (
	Red   Color = "\033[31m"
	Green Color = "\033[32m"
	Cyan  Color = "\033[36m"
	Gray  Color = "\033[37m"

	ErrorLogLevel LogLevel = iota
	InfoLogLevel
	DebugLogLevel
)

type Color string

func (c Color) Color(s ...string) string {
	return string(c) + strings.Join(s, " ") + "\033[0m"
}

type LogLevel int

func NewContext(parentCtx context.Context) *Context {
	ctx := Context{
		Context: parentCtx,
		Error:   log.New(os.Stderr, Red.Color("[ERROR] "), 0),
		Info:    log.New(os.Stdout, Green.Color("[INFO] "), 0),
		Debug:   log.New(os.Stdout, Gray.Color("[DEBUG] "), 0),
	}

	ctx.Debug.SetOutput(io.Discard)
	return &ctx
}

type Context struct {
	context.Context
	LogLevel LogLevel
	Error    *log.Logger
	Info     *log.Logger
	Debug    *log.Logger
}

func (ctx *Context) SetLoggingLevel(level LogLevel) Context {
	ctx.LogLevel = level

	if int(level) >= int(ErrorLogLevel) {
		ctx.Error = log.New(os.Stderr, Red.Color("[ERROR] "), 0)
	} else {
		ctx.Error.SetOutput(io.Discard)
	}

	if int(level) >= int(InfoLogLevel) {
		ctx.Info = log.New(os.Stderr, Green.Color("[INFO] "), 0)
	} else {
		ctx.Info.SetOutput(io.Discard)
	}

	if int(level) >= int(DebugLogLevel) {
		ctx.Debug = log.New(os.Stderr, Gray.Color("[DEBUG] "), 0)
	} else {
		ctx.Info.SetOutput(io.Discard)
	}
	return *ctx
}

func (ctx *Context) WithCancel() (*Context, context.CancelFunc) {
	var cancel context.CancelFunc
	ctx.Context, cancel = context.WithCancel(ctx.Context)
	newCtx := &Context{
		Context: ctx,
		Info:    ctx.Info,
		Debug:   ctx.Debug,
		Error:   ctx.Error,
	}

	newCtx.SetLoggingLevel(ctx.LogLevel)
	return newCtx, cancel
}

func (ctx Context) IsRunning(msg ...string) bool {
	select {
	case <-ctx.Done():
		if len(msg) != 0 {
			ctx.Info.Println(msg)
		}
		return false
	default:
		return true
	}
}

func (ctx Context) IsDone(msg ...string) bool {
	return !ctx.IsRunning(msg...)
}

func (ctx Context) Sleep(delay time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(delay):
	}
}

func SetDebugLabels(labels ...string) {
	debugger.SetLabels(func() []string {
		return labels
	})
}

func ExpandPath(path string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return path, nil
	}
	if path == "~" {
		path = home
	} else if strings.HasPrefix(path, "~/") {
		path = filepath.Join(home, path[2:])
	}
	return filepath.Abs(path)
}

func SigTermChan() chan os.Signal {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	return sigs
}

// GetInput reads the contents of each file in the given directory.
func GetInput(paths ...string) (map[string]Info, error) {
	var files []string

	for _, path := range paths {
		path, err := ExpandPath(path)
		if err != nil {
			return nil, err
		}

		f, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		if f.IsDir() {
			dir, err := os.ReadDir(path)
			if err != nil {
				return nil, err
			}

			for _, f := range dir {
				if f.IsDir() || !strings.HasSuffix(f.Name(), ".list") {
					continue
				}

				files = append(files, filepath.Join(path, f.Name()))
			}
		} else {
			files = append(files, path)
		}
	}

	results := map[string]Info{}

	for _, p := range files {
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}

		input := GetInputFromPath(string(data))
		for k, v := range input {
			results[k] = v
		}
	}

	return results, nil
}

func GetInputFromPath(list string) map[string]Info {
	resp := map[string]Info{}
	s := bufio.NewScanner(strings.NewReader(list))
	s.Split(bufio.ScanLines)
	for s.Scan() {
		p := strings.Split(s.Text(), "#")
		value := strings.TrimSpace(p[0])

		if value == "" {
			continue
		}
		resp[value] = Info{
			Comment: strings.Join(p[1:], "#"),
		}
	}
	return resp
}

var letterRunes = []rune("abcdefghijklmnopqrstuvwxyz")

func RandStringRunes(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}
	return string(b)
}
func Load(name string, account string) (map[string]Info, error) {
	all, err := loadAll(name)
	if err != nil {
		return nil, err
	}

	if _, ok := all[account]; !ok {
		all[account] = map[string]Info{}
	}

	return all[account], nil

}

func loadAll(name string) (map[string]map[string]Info, error) {
	path, err := ExpandPath(fmt.Sprintf("~/.roles/%s.json", name))
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if _, s := os.Stat(path); os.IsNotExist(s) {
		if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
			return nil, err
		}
	}

	var data []byte
	data, err = os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	results := map[string]map[string]Info{}
	if err := json.Unmarshal(data, &results); err != nil {
		return nil, err
	}

	return results, nil
}

func Save(ctx *Context, name string, account string, results map[string]Info) {
	if len(results) == 0 {
		ctx.Info.Printf("no results to save, refusing to overwrite data")
	}

	path, err := ExpandPath(fmt.Sprintf("~/.roles/%s.json", name))
	if err != nil {
		ctx.Error.Fatalf("expanding path: %s", err)
	}

	all, err := loadAll(name)
	if err != nil {
		ctx.Error.Fatalf("loading previous: %s", err)
	}

	all[account] = results

	data, err := json.Marshal(all)
	if err != nil {
		ctx.Error.Fatalf("marshalling data: %s", err)
	}

	if err := os.WriteFile(path, data, 0o600); err != nil {
		ctx.Error.Fatalf("writing data: %s", err)
	}
}

func RunOnSigterm(ctx *Context, f func(*Context)) {
	signalChannel := make(chan os.Signal, 2)
	signal.Notify(signalChannel, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-signalChannel
		switch sig {
		case os.Interrupt:
			ctx.Info.Println("Received SIGINT")
			f(ctx)
		case syscall.SIGTERM:
			ctx.Info.Println("Received SIGTERM")
			f(ctx)
		}

		ctx.Debug.Println("shutdown cleanly")
		os.Exit(0)
	}()
}
