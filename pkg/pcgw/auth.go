package pcgw

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
)

// Bot-password login.
//
// PCGW has required a MediaWiki login for action=cargoquery since 2026-08-23,
// and its Cloudflare front end challenges Special:CargoExport from data-center
// IPs, so a server-hosted sync has only the authenticated API left. Logging in
// with a bot password (Special:BotPasswords, grant "Create, query and delete
// data through the Cargo extension") is the method PCGamingWiki:API documents.

// errNotLoggedIn marks an API answer that means the session is missing or has
// expired, so the caller can log in again once and retry.
var errNotLoggedIn = errors.New("pcgw: not logged in")

// botCredentials returns the bot-password login from the client or, failing
// that, from GSBS_PCGW_BOT_USER / GSBS_PCGW_BOT_PASSWORD. The user has the
// form "Account@botname", as Special:BotPasswords shows it.
func (c *Client) botCredentials() (user, pass string) {
	if c != nil && c.BotUser != "" {
		return c.BotUser, c.BotPassword
	}
	return strings.TrimSpace(os.Getenv("GSBS_PCGW_BOT_USER")), os.Getenv("GSBS_PCGW_BOT_PASSWORD")
}

// hasBotCredentials reports whether a bot-password login is configured.
func (c *Client) hasBotCredentials() bool {
	user, pass := c.botCredentials()
	return user != "" && pass != ""
}

// ensureLogin logs in once per session. Concurrent callers wait for the first
// attempt rather than each logging in; a failed attempt is retried by the next
// caller.
func (c *Client) ensureLogin(ctx context.Context) error {
	c.authMu.Lock()
	defer c.authMu.Unlock()
	if c.loggedIn {
		return nil
	}
	if err := c.login(ctx); err != nil {
		return err
	}
	c.loggedIn = true
	return nil
}

// invalidateLogin forgets the session so the next ensureLogin logs in again.
func (c *Client) invalidateLogin() {
	c.authMu.Lock()
	c.loggedIn = false
	c.authMu.Unlock()
}

func (c *Client) login(ctx context.Context) error {
	user, pass := c.botCredentials()
	if user == "" || pass == "" {
		return errors.New("pcgw login: GSBS_PCGW_BOT_USER and GSBS_PCGW_BOT_PASSWORD must both be set")
	}
	if c.HTTP.Jar == nil {
		return errors.New("pcgw login: HTTP client has no cookie jar to hold the session")
	}

	// 1. A login token, which also starts the session cookie.
	resp, err := c.doGet(ctx, c.baseURL()+"/w/api.php?action=query&meta=tokens&type=login&format=json")
	if err != nil {
		return fmt.Errorf("pcgw login: fetch token: %w", err)
	}
	var tok struct {
		Query struct {
			Tokens struct {
				LoginToken string `json:"logintoken"`
			} `json:"tokens"`
		} `json:"query"`
	}
	err = decodeAPIJSON(resp, &tok)
	if err != nil {
		return fmt.Errorf("pcgw login: fetch token: %w", err)
	}
	if tok.Query.Tokens.LoginToken == "" {
		return errors.New("pcgw login: no login token in response")
	}

	// 2. action=login with the bot password. The password goes in a POST body,
	// never a URL, so it cannot end up in a proxy or server access log.
	form := url.Values{
		"action":     {"login"},
		"format":     {"json"},
		"lgname":     {user},
		"lgpassword": {pass},
		"lgtoken":    {tok.Query.Tokens.LoginToken},
	}
	resp, err = c.doPostForm(ctx, c.baseURL()+"/w/api.php", form)
	if err != nil {
		return fmt.Errorf("pcgw login: %w", err)
	}
	var res struct {
		Login struct {
			Result string `json:"result"`
			Reason string `json:"reason"`
		} `json:"login"`
	}
	if err := decodeAPIJSON(resp, &res); err != nil {
		return fmt.Errorf("pcgw login: %w", err)
	}
	if res.Login.Result != "Success" {
		reason := res.Login.Reason
		if reason == "" {
			reason = "no reason given"
		}
		// Name the user, never the password.
		return fmt.Errorf("pcgw login as %q: %s: %s", user, res.Login.Result, reason)
	}
	return nil
}

// doPostForm sends one rate-limited form POST. Unlike doGet it does not retry:
// a login is cheap to repeat from the caller, and a replayed request body
// would need rebuilding on every attempt.
func (c *Client) doPostForm(ctx context.Context, u string, form url.Values) (*http.Response, error) {
	if err := c.waitBetweenRequests(ctx); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", c.userAgentString())
	return c.HTTP.Do(req)
}

// decodeAPIJSON reads a MediaWiki API response into v, turning a non-200
// status into an error that carries the start of the body.
func decodeAPIJSON(resp *http.Response, v interface{}) error {
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// newCookieJar returns a jar for the login session. cookiejar.New only fails
// on a bad PublicSuffixList option, and none is passed.
func newCookieJar() http.CookieJar {
	jar, _ := cookiejar.New(nil)
	return jar
}
