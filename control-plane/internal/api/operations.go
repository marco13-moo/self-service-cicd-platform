package api

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/scm"
)

func (h *Handlers) ListSCMCommands(w http.ResponseWriter, r *http.Request) {
	expected := os.Getenv("CONTROL_PLANE_ADMIN_TOKEN")
	provided := r.Header.Get("Authorization")
	if expected == "" {
		http.Error(w, "administrative API is not configured", http.StatusServiceUnavailable)
		return
	}
	wanted := "Bearer " + expected
	if len(provided) != len(wanted) || subtle.ConstantTimeCompare([]byte(provided), []byte(wanted)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, h.commandStore.SCMCommands())
}

func (h *Handlers) Metrics(w http.ResponseWriter, _ *http.Request) {
	counts := map[scm.CommandStatus]int{}
	for _, command := range h.commandStore.SCMCommands() {
		counts[command.Status]++
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	for _, status := range []scm.CommandStatus{scm.CommandPending, scm.CommandLeased, scm.CommandFailed, scm.CommandSucceeded, scm.CommandSuperseded, scm.CommandDeadLetter} {
		_, _ = fmt.Fprintf(w, "scm_commands{status=%q} %d\n", strings.TrimSpace(string(status)), counts[status])
	}
}
