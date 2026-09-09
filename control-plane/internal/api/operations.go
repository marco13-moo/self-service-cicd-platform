package api

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/scm"
	"github.com/marco13-moo/self-service-cicd-platform/control-plane/internal/telemetry"
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
	snapshot := telemetry.Current()
	_, _ = fmt.Fprintln(w, "# TYPE platform_reconciliation_duration_seconds histogram")
	for index, bound := range snapshot.Bounds {
		_, _ = fmt.Fprintf(w, "platform_reconciliation_duration_seconds_bucket{le=%q} %d\n", fmt.Sprintf("%.0f", bound.Seconds()), snapshot.Buckets[index])
	}
	_, _ = fmt.Fprintf(w, "platform_reconciliation_duration_seconds_bucket{le=\"+Inf\"} %d\n", snapshot.Count)
	_, _ = fmt.Fprintf(w, "platform_reconciliation_duration_seconds_sum %.6f\nplatform_reconciliation_duration_seconds_count %d\n", snapshot.SumSeconds, snapshot.Count)
	_, _ = fmt.Fprintf(w, "# TYPE platform_jwks_refresh_failures_total counter\nplatform_jwks_refresh_failures_total %d\n", snapshot.JWKSRefreshFailures)
}
