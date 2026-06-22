//go:build windows

package main

import (
	"fmt"
	"strings"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

// friendlyLoginError maps low-level login/connection errors to user-facing text.
func friendlyLoginError(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "401"):
		return "Invalid username or password."
	case strings.Contains(msg, "403"):
		return "This account isn't allowed to log in."
	case strings.Contains(msg, "no such host"),
		strings.Contains(msg, "dial "),
		strings.Contains(msg, "connection refused"),
		strings.Contains(msg, "timeout"),
		strings.Contains(msg, "deadline exceeded"):
		return "Can't reach the server — check the URL and that the GSBS server is running."
	default:
		return msg
	}
}

// showLoginDialog shows a modal dialog to enter server URL, username, password, and client name.
// On success returns the saved config and nil. On cancel or error returns (nil, error).
func showLoginDialog(initialServerURL, initialClientName string) (*config, error) {
	if initialClientName == "" {
		initialClientName = defaultClientName()
	}
	var dlg *walk.Dialog
	var serverEdit, userEdit, passEdit, clientNameEdit *walk.LineEdit
	var loginBtn, testBtn *walk.PushButton
	var statusLabel *walk.Label
	var resultCfg *config
	var resultErr error

	// setBusy toggles input/buttons and shows a status message. Must run on the
	// UI thread (call directly from handlers or inside dlg.Synchronize).
	setBusy := func(busy bool, msg string) {
		enabled := !busy
		serverEdit.SetEnabled(enabled)
		userEdit.SetEnabled(enabled)
		passEdit.SetEnabled(enabled)
		clientNameEdit.SetEnabled(enabled)
		loginBtn.SetEnabled(enabled)
		testBtn.SetEnabled(enabled)
		statusLabel.SetText(msg)
	}

	_, err := Dialog{
		AssignTo: &dlg,
		Title:    "GSBS — Login",
		MinSize:  Size{420, 280},
		Layout:   Grid{Columns: 2, Spacing: 8},
		Children: []Widget{
			Label{Text: "Server URL:"},
			LineEdit{
				AssignTo: &serverEdit,
				Text:     initialServerURL,
				MinSize:  Size{300, 0},
			},
			Label{Text: "Username:"},
			LineEdit{AssignTo: &userEdit, MinSize: Size{300, 0}},
			Label{Text: "Password:"},
			LineEdit{
				AssignTo:     &passEdit,
				MinSize:      Size{300, 0},
				PasswordMode: true,
			},
			Label{Text: "Client name:"},
			LineEdit{
				AssignTo: &clientNameEdit,
				Text:     initialClientName,
				MinSize:  Size{300, 0},
			},
			Label{AssignTo: &statusLabel, ColumnSpan: 2, Text: ""},
			Composite{ColumnSpan: 2, Layout: HBox{MarginsZero: true}, Children: []Widget{
				PushButton{
					AssignTo: &loginBtn,
					Text:     "Login",
					OnClicked: func() {
						server := strings.TrimSpace(serverEdit.Text())
						user := strings.TrimSpace(userEdit.Text())
						pass := passEdit.Text()
						clientName := strings.TrimSpace(clientNameEdit.Text())
						if server == "" {
							statusLabel.SetText("Please enter the server URL (e.g. https://your-server:8080).")
							return
						}
						if user == "" || pass == "" {
							statusLabel.SetText("Please enter username and password.")
							return
						}
						setBusy(true, "Signing in…")
						go func() {
							cfg, doErr := DoLogin(server, user, pass, clientName)
							dlg.Synchronize(func() {
								if doErr != nil {
									setBusy(false, "")
									walk.MsgBox(dlg, "Login failed", friendlyLoginError(doErr), walk.MsgBoxIconError)
									return
								}
								resultCfg = cfg
								resultErr = nil
								dlg.Accept()
							})
						}()
					},
				},
				PushButton{
					AssignTo: &testBtn,
					Text:     "Test connection",
					OnClicked: func() {
						server := strings.TrimSpace(serverEdit.Text())
						if server == "" {
							statusLabel.SetText("Please enter the server URL first.")
							return
						}
						setBusy(true, "Testing connection…")
						go func() {
							testErr := TestConnection(server)
							dlg.Synchronize(func() {
								setBusy(false, "")
								if testErr != nil {
									walk.MsgBox(dlg, "Connection failed", friendlyLoginError(testErr), walk.MsgBoxIconError)
								} else {
									statusLabel.SetText("✓ Server is reachable.")
								}
							})
						}()
					},
				},
				PushButton{
					Text: "Cancel",
					OnClicked: func() {
						resultCfg = nil
						resultErr = fmt.Errorf("cancelled")
						dlg.Cancel()
					},
				},
			}},
		},
	}.Run(nil)

	if err != nil {
		return nil, err
	}
	if resultCfg == nil {
		return nil, resultErr
	}
	return resultCfg, nil
}
