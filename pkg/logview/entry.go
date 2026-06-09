package logview

// Entry is a normalized log line for WebUI display.
type Entry struct {
	Timestamp string
	Level     string
	Message   string
	Raw       string
	Event     string
	Summary   string
	Context   string
	Method    string
	Path      string
	Status    string
	Duration  string
	RequestID string
	IP        string
	UserID    string
	Username  string
	GameID    string
	PathKey   string
	ClientID  string
	Error     string
}
