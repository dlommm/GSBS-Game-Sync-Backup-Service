//go:build windows

package main

import (
	"fmt"
	"strings"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

// showLoginDialog shows a modal dialog to enter server URL, username, password, and client name.
// On success returns the saved config and nil. On cancel or error returns (nil, error).
func showLoginDialog(initialServerURL, initialClientName string) (*config, error) {
	if initialClientName == "" {
		initialClientName = defaultClientName()
	}
	var dlg *walk.Dialog
	var serverEdit, userEdit, passEdit, clientNameEdit *walk.LineEdit
	var resultCfg *config
	var resultErr error

	_, err := Dialog{
		AssignTo: &dlg,
		Title:    "GSBS — Login",
		MinSize:  Size{400, 220},
		Layout:   Grid{Columns: 2, Spacing: 10},
		Children: []Widget{
			Label{Text: "Server URL:"},
			LineEdit{
				AssignTo: &serverEdit,
				Text:     initialServerURL,
				MinSize:  Size{280, 0},
			},
			Label{Text: "Username:"},
			LineEdit{AssignTo: &userEdit, MinSize: Size{280, 0}},
			Label{Text: "Password:"},
			LineEdit{
				AssignTo:       &passEdit,
				MinSize:        Size{280, 0},
				PasswordMode:   true,
			},
			Label{Text: "Client name:"},
			LineEdit{
				AssignTo: &clientNameEdit,
				Text:     initialClientName,
				MinSize:  Size{280, 0},
			},
			VSpacer{ColumnSpan: 2},
			PushButton{
				Text: "Login",
				OnClicked: func() {
					server := serverEdit.Text()
					user := userEdit.Text()
					pass := passEdit.Text()
					clientName := strings.TrimSpace(clientNameEdit.Text())
					if server == "" {
						walk.MsgBox(dlg, "Login", "Please enter the server URL (e.g. https://your-server:8080).", walk.MsgBoxIconWarning)
						return
					}
					if user == "" || pass == "" {
						walk.MsgBox(dlg, "Login", "Please enter username and password.", walk.MsgBoxIconWarning)
						return
					}
					cfg, err := DoLogin(server, user, pass, clientName)
					if err != nil {
						walk.MsgBox(dlg, "Login failed", err.Error(), walk.MsgBoxIconError)
						return
					}
					resultCfg = cfg
					resultErr = nil
					dlg.Accept()
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
