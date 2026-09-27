package pcgw

import (
	"context"
	"errors"
	"os"
	"strings"
)

// Cargo backend names accepted by Client.CargoBackend and by the
// GSBS_PCGW_CARGO_BACKEND environment variable.
const (
	// CargoBackendExport reads Cargo tables through Special:CargoExport.
	CargoBackendExport = "export"
	// CargoBackendAPI reads Cargo tables through action=cargoquery.
	CargoBackendAPI = "api"
)

// cargoRequest is one Cargo table query, described independently of the
// transport used to run it.
type cargoRequest struct {
	Tables string
	Fields string
	Where  string
	// OrderBy makes offset pagination deterministic. Cargo applies no stable
	// order of its own, so paging without it can repeat or skip rows when the
	// underlying tables change mid-scan.
	OrderBy string
	Limit   int
	Offset  int
}

// cargoBackend runs a cargoRequest against PCGamingWiki.
//
// Two exist because PCGW closed the documented API to anonymous callers on
// 2026-08-23: action=cargoquery now answers "permissiondenied" unless the
// request carries a MediaWiki bot login, while Special:CargoExport still
// serves anonymous callers — except that PCGW's Cloudflare front end
// challenges it from data-center IPs. So apiBackend is chosen whenever a bot
// login is configured (see auth.go), and exportBackend otherwise.
type cargoBackend interface {
	name() string
	run(ctx context.Context, c *Client, q cargoRequest) ([]map[string]interface{}, error)
}

// cargoBackendFor resolves the backend for this client: an explicit
// Client.CargoBackend wins, then GSBS_PCGW_CARGO_BACKEND, then the default.
func (c *Client) cargoBackendFor() cargoBackend {
	name := ""
	if c != nil {
		name = strings.TrimSpace(c.CargoBackend)
	}
	if name == "" {
		name = strings.TrimSpace(os.Getenv("GSBS_PCGW_CARGO_BACKEND"))
	}
	switch {
	case strings.EqualFold(name, CargoBackendAPI):
		return apiBackend{}
	case strings.EqualFold(name, CargoBackendExport):
		return exportBackend{}
	case c.hasBotCredentials():
		return apiBackend{}
	}
	return exportBackend{}
}

// runCargo executes q through the configured backend. Every internal Cargo
// read goes through here rather than calling a transport directly.
func (c *Client) runCargo(ctx context.Context, q cargoRequest) ([]map[string]interface{}, error) {
	return c.cargoBackendFor().run(ctx, c, q)
}

// apiBackend runs queries through action=cargoquery. Anonymous requests fail
// with permissiondenied, so with a bot login configured it logs in first, and
// logs in again once if the session has expired mid-sync.
type apiBackend struct{}

func (apiBackend) name() string { return CargoBackendAPI }

func (apiBackend) run(ctx context.Context, c *Client, q cargoRequest) ([]map[string]interface{}, error) {
	if !c.hasBotCredentials() {
		return c.cargoQueryOrdered(ctx, q)
	}
	if err := c.ensureLogin(ctx); err != nil {
		return nil, err
	}
	rows, err := c.cargoQueryOrdered(ctx, q)
	if !errors.Is(err, errNotLoggedIn) {
		return rows, err
	}
	c.invalidateLogin()
	if err := c.ensureLogin(ctx); err != nil {
		return nil, err
	}
	return c.cargoQueryOrdered(ctx, q)
}
