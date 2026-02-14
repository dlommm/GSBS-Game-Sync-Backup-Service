package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"
)

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
	if username == "" || password == "" {
		fmt.Println("username and password required")
		os.Exit(1)
	}
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
	resp, err := http.Post(cfg.ServerURL+"/api/login", "application/json", strings.NewReader(string(jsonBody)))
	if err != nil {
		fmt.Println("request failed:", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Println("login failed:", resp.Status)
		os.Exit(1)
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		fmt.Println("decode failed:", err)
		os.Exit(1)
	}
	cfg.Token = out.Token
	if err := saveConfig(cfg); err != nil {
		fmt.Println("save config failed:", err)
		os.Exit(1)
	}
	fmt.Println("Logged in. Token saved to config.")
}
