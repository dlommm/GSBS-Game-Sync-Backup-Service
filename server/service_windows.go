//go:build windows

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gsbs/gsbs/server/logx"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/eventlog"
	"golang.org/x/sys/windows/svc/mgr"
)

const (
	windowsServiceName        = "GSBSServer"
	windowsServiceDisplayName = "GSBS Server"
	serviceLogPathEnv         = "GSBS_SERVICE_LOG_PATH"
)

func runWindowsServiceHost() error {
	logPath := serviceLogPath()
	if err := logx.InitFile(logPath); err != nil {
		return fmt.Errorf("service logging init failed (%s): %w", logPath, err)
	}
	defer func() {
		if err := logx.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "close logger: %v\n", err)
		}
	}()

	isInteractive, err := svc.IsAnInteractiveSession()
	if err == nil && isInteractive {
		return fmt.Errorf("--service must be started by Windows Service Control Manager")
	}

	logx.Logger().Info().Str("path", logPath).Msg("service logging initialized")
	logx.Logger().Info().Msg("starting Windows service host")
	if err := svc.Run(windowsServiceName, &windowsService{}); err != nil {
		return fmt.Errorf("service run failed: %w", err)
	}
	logx.Logger().Info().Msg("windows service host stopped")
	return nil
}

func manageWindowsService(opts cliOptions) error {
	switch {
	case opts.installService:
		return installWindowsService(strings.TrimSpace(opts.envFile))
	case opts.uninstallService:
		return uninstallWindowsService()
	case opts.startService:
		return startWindowsService()
	case opts.stopService:
		return stopWindowsService(20 * time.Second)
	default:
		return fmt.Errorf("no service action specified")
	}
}

type windowsService struct{}

func (m *windowsService) Execute(_ []string, req <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	const accepted = svc.AcceptStop | svc.AcceptShutdown
	status <- svc.Status{State: svc.StartPending}

	app, err := newServerApp()
	if err != nil {
		logx.Logger().Error().Err(err).Msg("service startup failed")
		status <- svc.Status{State: svc.Stopped}
		return false, 1
	}
	app.Start(context.Background())
	status <- svc.Status{State: svc.Running, Accepts: accepted}

	for {
		select {
		case change := <-req:
			switch change.Cmd {
			case svc.Interrogate:
				status <- change.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending}
				if err := app.Shutdown(context.Background()); err != nil {
					logx.Logger().Error().Err(err).Msg("service shutdown failed")
				}
				status <- svc.Status{State: svc.Stopped}
				return false, 0
			default:
			}
		case listenErr := <-app.Errors():
			if listenErr != nil {
				_ = app.Shutdown(context.Background())
				logx.Logger().Error().Err(listenErr).Msg("service listen failed")
				status <- svc.Status{State: svc.Stopped}
				return false, 1
			}
			_ = app.Shutdown(context.Background())
			status <- svc.Status{State: svc.Stopped}
			return false, 0
		}
	}
}

func installWindowsService(envFile string) error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()

	exePath, err := os.Executable()
	if err != nil {
		return err
	}
	exePath, err = filepath.Abs(exePath)
	if err != nil {
		return err
	}

	cfg := mgr.Config{
		DisplayName: windowsServiceDisplayName,
		StartType:   mgr.StartAutomatic,
		Description: "GSBS Server (Game Sync & Backup Service)",
	}
	args := []string{"--service"}
	if envFile := strings.TrimSpace(envFile); envFile != "" {
		args = append(args, "--env-file", envFile)
	}
	s, err := m.CreateService(windowsServiceName, exePath, cfg, args...)
	if err != nil {
		return err
	}
	defer s.Close()

	_ = eventlog.InstallAsEventCreate(windowsServiceName, eventlog.Info|eventlog.Warning|eventlog.Error)
	fmt.Printf("Installed service %s (%s)\n", windowsServiceName, windowsServiceDisplayName)
	return nil
}

func uninstallWindowsService() error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()

	s, err := m.OpenService(windowsServiceName)
	if err != nil {
		return err
	}
	defer s.Close()

	status, err := s.Query()
	if err == nil && status.State != svc.Stopped {
		if _, stopErr := s.Control(svc.Stop); stopErr == nil {
			_ = waitForServiceStopped(s, 20*time.Second)
		}
	}
	if err := s.Delete(); err != nil {
		return err
	}
	_ = eventlog.Remove(windowsServiceName)
	fmt.Printf("Uninstalled service %s\n", windowsServiceName)
	return nil
}

func startWindowsService() error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()

	s, err := m.OpenService(windowsServiceName)
	if err != nil {
		return err
	}
	defer s.Close()

	if err := s.Start(); err != nil {
		return err
	}
	fmt.Printf("Started service %s\n", windowsServiceName)
	return nil
}

func stopWindowsService(timeout time.Duration) error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()

	s, err := m.OpenService(windowsServiceName)
	if err != nil {
		return err
	}
	defer s.Close()

	if _, err := s.Control(svc.Stop); err != nil {
		return err
	}
	if err := waitForServiceStopped(s, timeout); err != nil {
		return err
	}
	fmt.Printf("Stopped service %s\n", windowsServiceName)
	return nil
}

func waitForServiceStopped(s *mgr.Service, timeout time.Duration) error {
	status, err := s.Query()
	if err != nil {
		return err
	}
	deadline := time.Now().Add(timeout)
	for status.State != svc.Stopped {
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for service to stop")
		}
		time.Sleep(300 * time.Millisecond)
		status, err = s.Query()
		if err != nil {
			return err
		}
	}
	return nil
}

func serviceLogPath() string {
	if v := os.Getenv(serviceLogPathEnv); v != "" {
		return filepath.Clean(v)
	}
	base := os.Getenv("ProgramData")
	if base == "" {
		base = `C:\ProgramData`
	}
	return filepath.Join(base, "GSBS", "logs", "server.log")
}
