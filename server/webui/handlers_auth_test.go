package webui

import (
	"net/http/httptest"
	"testing"
)

func TestAdminQuerySuccessPCGWActionMessages(t *testing.T) {
	t.Run("missing local action", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/admin/activity?job_started=1&job_action=missing_local", nil)
		got := adminQuerySuccess(r)
		if got == "" {
			t.Fatalf("expected success message for missing_local action")
		}
	})

	t.Run("retry failed action", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/admin/activity?job_started=1&job_action=retry_failed", nil)
		got := adminQuerySuccess(r)
		if got == "" {
			t.Fatalf("expected success message for retry_failed action")
		}
	})

	t.Run("default action", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/admin/activity?job_started=1", nil)
		got := adminQuerySuccess(r)
		want := "PCGW sync job started in background."
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})
}
