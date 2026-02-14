package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"
)

// DoLogin calls the server login API and returns a config with the token set (and saves it).
// Used by both CLI runLogin and the Windows tray login dialog.
// serverURL must be non-empty (e.g. https://your-server:8080).
func DoLogin(serverURL, username, password string) (*config, error) {
	serverURL = strings.TrimSpace(serverURL)
	username = strings.TrimSpace(username)
	password = strings.TrimSpace(password)
	if serverURL == "" {
		return nil, fmt.Errorf("server URL is required")
	}
	if username == "" || password == "" {
		return nil, fmt.Errorf("username and password are required")
	}
	// Normalize URL (no trailing slash)
	serverURL = strings.TrimSuffix(serverURL, "/")
	clientOS := "linux"
	if runtime.GOOS == "windows" {
		clientOS = "windows"
	}
	body := map[string]string{
		"username":    username,
		"password":    password,
		"client_name": "client",
		"client_os":   clientOS,
	}
	jsonBody, _ := json.Marshal(body)
	resp, err := http.Post(serverURL+"/api/login", "application/json", strings.NewReader(string(jsonBody)))
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("login failed: %s", resp.Status)
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("invalid response: %w", err)
	}
	cfg := &config{
		ServerURL:    serverURL,
		Token:        out.Token,
		SyncInterval: 5 * time.Minute,
		WatchPaths:   []watchPath{},
	}
	if err := saveConfig(cfg); err != nil {
		return nil, fmt.Errorf("save config: %w", err)
	}
	return cfg, nil
}

func runLogin() {
	cfg, _ := loadConfig()
	if cfg == nil {
		cfg = defaultConfig("")
	}
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Server URL [", cfg.ServerURL, "]: ")
	serverURL, _ := reader.ReadString('\n')
	serverURL = strings.TrimSpace(serverURL)
	if serverURL != "" {
		cfg.ServerURL = serverURL
	}
	fmt.Print("Username: ")
	username, _ := reader.ReadString('\n')
	username = strings.TrimSpace(username)
	fmt.Print("Password: ")
	password, _ := reader.ReadString('\n')
	password = strings.TrimSpace(password)
	cfg, err := DoLogin(cfg.ServerURL, username, password)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	_ = cfg
	fmt.Println("Logged in. Token saved to config.")
}
