package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

func loadEnvFile(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			return fmt.Errorf("env file %s: line %d missing '='", path, lineNo)
		}
		key = strings.TrimSpace(key)
		if key == "" {
			return fmt.Errorf("env file %s: line %d has empty key", path, lineNo)
		}
		_ = os.Setenv(key, strings.TrimSpace(value))
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}
