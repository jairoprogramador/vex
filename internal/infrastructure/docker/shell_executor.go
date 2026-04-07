package docker

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sync"

	"github.com/jairoprogramador/vex/internal/domain/docker/ports"
)

type ShellExecutor struct{}

func NewShellExecutor() ports.CommandExecutor {
	return &ShellExecutor{}
}

func (s *ShellExecutor) Execute(ctx context.Context, command string) (string, error) {
	var cmd *exec.Cmd
	if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
		cmd = exec.CommandContext(ctx, "sh", "-c", command)
	} else {
		cmd = exec.CommandContext(ctx, "cmd", "/C", command)
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", errors.New("error creating stdout pipe")
	}

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return "", errors.New("error creating stderr pipe")
	}

	var stdoutBuf, stderrBuf bytes.Buffer

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stdoutPipe)
		for scanner.Scan() {
			line := scanner.Text()
			fmt.Fprintf(os.Stdout, "%s\n", line)
			stdoutBuf.WriteString(line + "\n")
		}
	}()
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stderrPipe)
		for scanner.Scan() {
			line := scanner.Text()
			fmt.Fprintf(os.Stderr, "%s\n", line)
			stderrBuf.WriteString(line + "\n")
		}
	}()

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stdout, "%s\n", err)
		return "", err
	}

	err = cmd.Wait()
	wg.Wait()

	if err != nil {
		return stderrBuf.String(), err
	}

	return stdoutBuf.String(), nil
}
