package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"
)

const loginTimeout = 30 * time.Second

// TestConnection checks whether the server URL is reachable (manifest v2, fallback v1).
func TestConnection(serverURL string) error {
	resp, err := pingManifestHealth(serverURL, "")
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// defaultClientName returns a default name for this client (hostname or "client").
func defaultClientName() string {
	name, err := os.Hostname()
	if err != nil || name == "" {
		return "client"
	}
	return name
}

// DoLogin calls the server login API and returns a config with the token set (and saves it).
// Used by both CLI runLogin and the Windows tray login dialog.
// Merges server URL, token, and client name into existing config so watch_paths and other settings are preserved.
// serverURL must be non-empty (e.g. https://your-server:8080). clientName is sent to the server; if empty, defaultClientName() is used.
func DoLogin(serverURL, username, password, clientName string) (*config, error) {
	serverURL = strings.TrimSpace(serverURL)
	username = strings.TrimSpace(username)
	password = strings.TrimSpace(password)
	clientName = strings.TrimSpace(clientName)
	if serverURL == "" {
		return nil, fmt.Errorf("server URL is required")
	}
	if username == "" || password == "" {
		return nil, fmt.Errorf("username and password are required")
	}
	if clientName == "" {
		clientName = defaultClientName()
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
		"client_name": clientName,
		"client_os":   clientOS,
	}
	jsonBody, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, serverURL+"/api/login", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: loginTimeout}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("client login: failed server=%s username=%q: %v", serverURL, username, err)
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("client login: failed server=%s username=%q: %s", serverURL, username, resp.Status)
		return nil, fmt.Errorf("login failed: %s", resp.Status)
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		log.Printf("client login: failed server=%s username=%q: invalid response: %v", serverURL, username, err)
		return nil, fmt.Errorf("invalid response: %w", err)
	}
	token := strings.TrimSpace(out.Token)
	if token == "" {
		return nil, fmt.Errorf("server did not return a token")
	}
	// Merge into existing config so watch_paths and launcher paths are preserved
	cfg, _ := loadConfig()
	if cfg == nil {
		cfg = defaultConfig(serverURL)
	}
	cfg.ServerURL = serverURL
	cfg.Token = token
	cfg.ClientName = clientName
	if cfg.SyncInterval == 0 {
		cfg.SyncInterval = Duration(5 * time.Minute)
	}
	if err := saveConfig(cfg); err != nil {
		log.Printf("client login: save config failed server=%s username=%q: %v", serverURL, username, err)
		return nil, fmt.Errorf("save config: %w", err)
	}
	log.Printf("client login: ok server=%s username=%q client_name=%q", serverURL, username, clientName)
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
	clientName := cfg.ClientName
	if clientName == "" {
		clientName = defaultClientName()
	}
	fmt.Print("Client name [", clientName, "]: ")
	clientNameLine, _ := reader.ReadString('\n')
	clientNameLine = strings.TrimSpace(clientNameLine)
	if clientNameLine != "" {
		clientName = clientNameLine
	}
	cfg, err := DoLogin(cfg.ServerURL, username, password, clientName)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	_ = cfg
	fmt.Println("Logged in. Token saved to config.")
}
