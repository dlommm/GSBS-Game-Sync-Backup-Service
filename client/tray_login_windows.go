//go:build windows

package main

import (
	"fmt"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

// showLoginDialog shows a modal dialog to enter server URL, username, and password.
// On success returns the saved config and nil. On cancel or error returns (nil, error).
func showLoginDialog(initialServerURL string) (*config, error) {
	var dlg *walk.Dialog
	var serverEdit, userEdit, passEdit *walk.LineEdit
	var resultCfg *config
	var resultErr error

	_, err := Dialog{
		AssignTo: &dlg,
		Title:    "GSBS — Login",
		MinSize:  Size{400, 180},
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
			VSpacer{ColumnSpan: 2},
			PushButton{
				Text: "Login",
				OnClicked: func() {
					server := serverEdit.Text()
					user := userEdit.Text()
					pass := passEdit.Text()
					if server == "" {
						walk.MsgBox(dlg, "Login", "Please enter the server URL (e.g. https://your-server:8080).", walk.MsgBoxIconWarning)
						return
					}
					if user == "" || pass == "" {
						walk.MsgBox(dlg, "Login", "Please enter username and password.", walk.MsgBoxIconWarning)
						return
					}
					cfg, err := DoLogin(server, user, pass)
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
