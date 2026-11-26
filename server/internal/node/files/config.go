package files

import (
	"context"
	"os"
	"os/exec"
	"sync"
	"time"
)

// Clock supplies cache timestamps and the deadline used by concurrent stat calls.
type Clock interface {
	Now() time.Time
	Timer(time.Duration) (<-chan time.Time, func())
}

type wallClock struct{}

func (wallClock) Now() time.Time { return time.Now() }
func (wallClock) Timer(d time.Duration) (<-chan time.Time, func()) {
	timer := time.NewTimer(d)
	return timer.C, func() { timer.Stop() }
}

// Config supplies the node's path policy, filesystem operations and Git commands.
type Config struct {
	Home          string
	RealPath      func(string) (string, error)
	StatPath      func(string) (string, os.FileInfo, error)
	OffLimits     func(string) error
	TooManyLinks  error
	ExplainDenied func(string, error) error
	GitCommand    func(context.Context, string, ...string) *exec.Cmd
	IgnoreCommand func(context.Context, string) *exec.Cmd
	Open          func(string) (ReadHandle, error)
	Size          func(os.FileInfo) int64
	IsSysfs       func(ReadHandle) (bool, error)
	StatTimeout   time.Duration
	Clock         Clock
}

// Service reads and lists files through the node's path policy.
type Service struct {
	config  Config
	findMu  sync.Mutex
	finds   map[string]*findEntry
	namesMu sync.Mutex
	users   map[int]string
	groups  map[int]string
}

func New(config Config) *Service {
	if config.Open == nil {
		config.Open = func(path string) (ReadHandle, error) { return os.Open(path) }
	}
	if config.Size == nil {
		config.Size = os.FileInfo.Size
	}
	if config.IsSysfs == nil {
		config.IsSysfs = fileReadIsSysfs
	}
	if config.StatTimeout == 0 {
		config.StatTimeout = 3 * time.Second
	}
	if config.Clock == nil {
		config.Clock = wallClock{}
	}
	return &Service{config: config, finds: map[string]*findEntry{}, users: map[int]string{}, groups: map[int]string{}}
}
