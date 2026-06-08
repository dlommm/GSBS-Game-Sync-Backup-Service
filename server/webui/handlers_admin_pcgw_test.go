package webui

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRouteAdminPCGWAutoCatchUpEndpointRequiresCSRF(t *testing.T) {
	h := &WebHandler{secret: "test-secret"}
	req := httptest.NewRequest(http.MethodPost, "/admin/pcgw/sync/auto-catch-up", nil)
	rr := httptest.NewRecorder()

	if handled := h.routeAdminPCGW(rr, req); !handled {
		t.Fatal("expected routeAdminPCGW to handle /admin/pcgw/sync/auto-catch-up")
	}
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}
