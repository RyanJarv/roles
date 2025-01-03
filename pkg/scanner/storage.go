package scanner

import (
	"encoding/json"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/ryanjarv/roles/pkg/utils"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

type PrincipalStatus int

const (
	PrincipalUnknown PrincipalStatus = iota
	PrincipalExists
	PrincipalDoesNotExist
)

func NewStorage(ctx *utils.Context, name string) (*Storage, error) {
	dataPath, err := utils.ExpandPath(fmt.Sprintf("~/.roles/%s.json", name))
	if err != nil {
		return nil, fmt.Errorf("expanding path: %s", err)
	}

	storage := &Storage{
		mux:      sync.Mutex{},
		name:     name,
		data:     map[string]map[string]utils.Info{},
		dataPath: dataPath,
		lockPath: dataPath + ".lock",
	}

	utils.RunOnSigterm(ctx, func(ctx *utils.Context) {
		err := storage.Save()
		if err != nil {
			ctx.Error.Printf("saving data: %s", err)
		}

		if err := storage.Close(); err != nil {
			ctx.Error.Printf("closing storage: %s", err)
		}
	})

	if err := storage.Load(ctx); err != nil {
		return nil, fmt.Errorf("loading storage: %s", err)
	}

	return storage, nil
}

type Storage struct {
	mux      sync.Mutex
	data     map[string]map[string]utils.Info
	name     string
	dataPath string
	lockPath string
}

func (s *Storage) Load(ctx *utils.Context) error {
	if err := os.MkdirAll(filepath.Dir(s.dataPath), 0o700); err != nil {
		return err
	}
	if _, stat := os.Stat(s.dataPath); os.IsNotExist(stat) {
		if err := os.WriteFile(s.dataPath, []byte("{}"), 0o600); err != nil {
			return err
		}
	}

	var data []byte

	// Ensure other processes don't try to read/write the file at the same time
	if err := s.lockDataFile(ctx); err != nil {
		return fmt.Errorf("global lock: %s", err)
	}

	s.mux.Lock()
	data, err := os.ReadFile(s.dataPath)
	if err != nil {
		return fmt.Errorf("reading data: %s", err)
	}

	if err := json.Unmarshal(data, &s.data); err != nil {
		return fmt.Errorf("unmarshalling data: %s", err)
	}
	s.mux.Unlock()

	return nil
}

func (s *Storage) Save() error {
	path, err := utils.ExpandPath(fmt.Sprintf("~/.roles/%s.json", s.name))
	if err != nil {
		return fmt.Errorf("expanding path: %s", err)
	}

	s.mux.Lock()
	data, err := json.Marshal(s.data)
	if err != nil {
		return fmt.Errorf("marshalling data: %s", err)
	}

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("writing data: %s", err)
	}
	s.mux.Unlock()

	return nil
}

func (s *Storage) Set(principalArn string, info utils.Info) error {
	parse, err := arn.Parse(principalArn)
	if err != nil {
		return fmt.Errorf("parsing arn %s: %s", principalArn, err)
	}

	s.mux.Lock()
	if _, ok := s.data[parse.AccountID]; !ok {
		s.data[parse.AccountID] = map[string]utils.Info{}
	}
	s.data[parse.AccountID][principalArn] = info
	s.mux.Unlock()

	return nil
}

func (s *Storage) lockDataFile(ctx *utils.Context) error {
	if contents, err := os.ReadFile(s.lockPath); os.IsNotExist(err) {
		ctx.Debug.Printf("lock file does not exist: %s", s.lockPath)
	} else if err != nil {
		// Some other error occurred
		return fmt.Errorf("reading lock file: %s", err)
	} else {
		// Some other process is holding the lock
		return fmt.Errorf("lock for %s held by %s", s.lockPath, string(contents))
	}

	return os.WriteFile(s.lockPath, []byte(strconv.Itoa(os.Getpid())), 0o600)
}

func (s *Storage) Close() error {
	return os.Remove(s.lockPath)
}

func (s *Storage) GetStatus(principalArn string) (PrincipalStatus, *utils.Info, error) {
	s.mux.Lock()
	defer s.mux.Unlock()

	if accountData, ok := s.data[principalArn]; !ok {
		return PrincipalUnknown, nil, nil
	} else if info, ok := accountData[principalArn]; !ok {
		return PrincipalUnknown, nil, nil
	} else if info.Exists {
		return PrincipalExists, &info, nil
	} else {
		return PrincipalDoesNotExist, &info, nil
	}
}
