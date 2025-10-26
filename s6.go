package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type S6 struct {
	Dir string
}

func (s6 *S6) ListServices() ([]*Service, error) {
	files, err := os.ReadDir(s6.Dir)
	if err != nil {
		return nil, err
	}

	var services []*Service
	for _, f := range files {
		fp := filepath.Join(s6.Dir, f.Name())
		stat, err := os.Stat(fp)
		if err != nil {
			log.Printf("error statting %s: %v", fp, err)
			continue
		}
		if stat.IsDir() {
			if fst, err := os.Stat(filepath.Join(fp, "run")); err == nil && fst.Mode()&0x100 != 0 {
				services = append(services, &Service{Dir: fp})
			}
		}
	}

	sort.Slice(services, func(i, j int) bool {
		return services[i].Dir < services[j].Dir
	})
	return services, nil
}

type Service struct {
	Dir string
}

func (svc *Service) Name() string {
	return filepath.Base(svc.Dir)
}

func (svc *Service) Stat() (ServiceStatus, error) {
	var status ServiceStatus
	cmd := exec.CommandContext(context.Background(), "s6-svstat", "-o", "up,pid,exitcode,signal,updownfor,ready,readyfor", svc.Dir)
	line, err := cmd.CombinedOutput()
	if err != nil {
		return status, fmt.Errorf("error running s6-svstat: %w (%s)", err, line)
	}

	fields := strings.Fields(string(line))
	if len(fields) != 7 {
		return status, fmt.Errorf("unexpected output from s6-svstat: %s", line)
	}

	status.Up, err = strconv.ParseBool(fields[0])
	if err != nil {
		return status, fmt.Errorf("error parsing up field: %w", err)
	}
	status.Pid, err = strconv.Atoi(fields[1])
	if err != nil {
		return status, fmt.Errorf("error parsing pid field: %w", err)
	}
	status.ExitCode, err = strconv.Atoi(fields[2])
	if err != nil {
		return status, fmt.Errorf("error parsing exitcode field: %w", err)
	}
	status.Signal = fields[3]
	updownFor, err := strconv.Atoi(fields[4])
	if err != nil {
		return status, fmt.Errorf("error parsing updownfor field: %w", err)
	}
	status.UpdownFor = time.Duration(updownFor) * time.Second

	status.Ready, err = strconv.ParseBool(fields[5])
	if err != nil {
		return status, fmt.Errorf("error parsing up field: %w", err)
	}
	readyFor, err := strconv.Atoi(fields[6])
	if err != nil {
		return status, fmt.Errorf("error parsing updownfor field: %w", err)
	}
	status.ReadyFor = time.Duration(readyFor) * time.Second

	return status, nil
}

func (svc *Service) Signal(ctx context.Context, sig syscall.Signal) error {
	return exec.CommandContext(ctx, "s6-svc", "-s", strconv.Itoa(int(sig)), svc.Dir).Run()
}

func (svc *Service) Restart(ctx context.Context) error {
	return exec.CommandContext(ctx, "s6-svc", "-r", svc.Dir).Run()
}

func (svc *Service) Up(ctx context.Context) error {
	return exec.CommandContext(ctx, "s6-svc", "-u", svc.Dir).Run()
}

func (svc *Service) Down(ctx context.Context) error {
	return exec.CommandContext(ctx, "s6-svc", "-d", svc.Dir).Run()
}

type ServiceStatus struct {
	Up        bool
	UpdownFor time.Duration
	Ready     bool
	ReadyFor  time.Duration
	Pid       int
	ExitCode  int
	Signal    string
}
