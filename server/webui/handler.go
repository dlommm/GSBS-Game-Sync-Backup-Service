package webui

import (
	"html/template"
	"net/http"

	"github.com/gsbs/gsbs/server/auth"
	"github.com/gsbs/gsbs/server/store"
)

// WebHandler serves the WebUI (login, register, dashboard).
type WebHandler struct {
	store    store.Store
	auth     *auth.Service
	secret   string
	templates *template.Template
}

// NewWebHandler creates a WebHandler. secret is used to sign session cookies; if empty, a default is used (insecure for production).
func NewWebHandler(st store.Store, authSvc *auth.Service, secret string) *WebHandler {
	if secret == "" {
		secret = "gsbs-default-secret-change-me"
	}
	tmpl := template.Must(template.New("").ParseFS(templatesFS, "templates/*.html"))
	return &WebHandler{store: st, auth: authSvc, secret: secret, templates: tmpl}
}

func (h *WebHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	switch {
	case path == "/" || path == "/login":
		if r.Method == http.MethodGet {
			h.serveLogin(w, r)
		} else if r.Method == http.MethodPost {
			h.handleLogin(w, r)
		} else {
			http.NotFound(w, r)
		}
	case path == "/register":
		if r.Method == http.MethodGet {
			h.serveRegister(w, r)
		} else if r.Method == http.MethodPost {
			h.handleRegister(w, r)
		} else {
			http.NotFound(w, r)
		}
	case path == "/dashboard":
		if r.Method == http.MethodGet {
			h.serveDashboard(w, r)
		} else {
			http.NotFound(w, r)
		}
	case path == "/logout":
		if r.Method == http.MethodPost || r.Method == http.MethodGet {
			ClearSession(w)
			Redirect(w, r, "/login")
		} else {
			http.NotFound(w, r)
		}
	default:
		http.NotFound(w, r)
	}
}

func (h *WebHandler) serveLogin(w http.ResponseWriter, r *http.Request) {
	if GetSession(r, h.secret) != "" {
		Redirect(w, r, "/dashboard")
		return
	}
	h.templates.ExecuteTemplate(w, "login.html", nil)
}

func (h *WebHandler) handleLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	username := r.FormValue("username")
	password := r.FormValue("password")
	if username == "" || password == "" {
		h.templates.ExecuteTemplate(w, "login.html", map[string]string{"Error": "Username and password required"})
		return
	}
	userID, _, err := h.auth.Login(r.Context(), username, password, "web", "web")
	if err != nil {
		h.templates.ExecuteTemplate(w, "login.html", map[string]string{"Error": "Invalid username or password"})
		return
	}
	SetSession(w, h.secret, userID)
	Redirect(w, r, "/dashboard")
}

func (h *WebHandler) serveRegister(w http.ResponseWriter, r *http.Request) {
	h.templates.ExecuteTemplate(w, "register.html", nil)
}

func (h *WebHandler) handleRegister(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	username := r.FormValue("username")
	password := r.FormValue("password")
	confirm := r.FormValue("confirm_password")
	if username == "" || password == "" {
		h.templates.ExecuteTemplate(w, "register.html", map[string]string{"Error": "Username and password required"})
		return
	}
	if password != confirm {
		h.templates.ExecuteTemplate(w, "register.html", map[string]string{"Error": "Passwords do not match"})
		return
	}
	_, err := h.auth.RegisterUser(r.Context(), username, password)
	if err != nil {
		h.templates.ExecuteTemplate(w, "register.html", map[string]string{"Error": "Username already taken"})
		return
	}
	Redirect(w, r, "/login")
}

type dashboardData struct {
	Clients []store.ClientInfo
	Saves   []saveSummary
	Error   string
}

type saveSummary struct {
	GameID    string
	PathKey   string
	UpdatedAt string
}

func (h *WebHandler) serveDashboard(w http.ResponseWriter, r *http.Request) {
	userID := GetSession(r, h.secret)
	if userID == "" {
		Redirect(w, r, "/login")
		return
	}
	clients, err := h.store.ListClientsByUserID(r.Context(), userID)
	if err != nil {
		h.templates.ExecuteTemplate(w, "dashboard.html", dashboardData{Error: "Failed to load clients"})
		return
	}
	saveBlobs, err := h.store.ListSaves(r.Context(), userID)
	if err != nil {
		h.templates.ExecuteTemplate(w, "dashboard.html", dashboardData{Clients: clients, Error: "Failed to load saves"})
		return
	}
	saves := make([]saveSummary, len(saveBlobs))
	for i := range saveBlobs {
		saves[i] = saveSummary{
			GameID:    saveBlobs[i].GameID,
			PathKey:   saveBlobs[i].PathKey,
			UpdatedAt: saveBlobs[i].UpdatedAt,
		}
	}
	h.templates.ExecuteTemplate(w, "dashboard.html", dashboardData{Clients: clients, Saves: saves})
}
