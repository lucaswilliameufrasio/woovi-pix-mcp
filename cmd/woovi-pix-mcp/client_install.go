package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func executableForClientConfig() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", errors.New("unable to locate executable")
	}

	return persistentExecutable(executable)
}

func persistentExecutable(executable string) (string, error) {
	executable, err := filepath.Abs(executable)
	if err != nil {
		return "", errors.New("unable to resolve executable path")
	}

	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return "", errors.New("unable to resolve executable path")
	}

	for directory := filepath.Dir(executable); directory != filepath.Dir(directory); directory = filepath.Dir(directory) {
		if strings.HasPrefix(filepath.Base(directory), "go-build") {
			return "", errors.New("client registration requires a persistent binary; build it with `go build -o ./bin/woovi-pix-mcp ./cmd/woovi-pix-mcp` first")
		}
	}

	return executable, nil
}
