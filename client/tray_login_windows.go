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
		MinSize:  Size{420, 260},
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
			VSpacer{ColumnSpan: 2},
			Composite{ColumnSpan: 2, Layout: HBox{MarginsZero: true}, Children: []Widget{
				PushButton{
					Text: "Login",
					OnClicked: func() {
						server := strings.TrimSpace(serverEdit.Text())
						user := strings.TrimSpace(userEdit.Text())
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
						cfg, doErr := DoLogin(server, user, pass, clientName)
						if doErr != nil {
							walk.MsgBox(dlg, "Login failed", doErr.Error(), walk.MsgBoxIconError)
							return
						}
						resultCfg = cfg
						resultErr = nil
						dlg.Accept()
					},
				},
				PushButton{
					Text: "Test connection",
					OnClicked: func() {
						server := strings.TrimSpace(serverEdit.Text())
						if server == "" {
							walk.MsgBox(dlg, "Test connection", "Please enter the server URL first.", walk.MsgBoxIconWarning)
							return
						}
						if testErr := TestConnection(server); testErr != nil {
							walk.MsgBox(dlg, "Connection failed", testErr.Error(), walk.MsgBoxIconError)
						} else {
							walk.MsgBox(dlg, "Connection OK", "Server is reachable.", walk.MsgBoxIconInformation)
						}
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
