// Package api exposes the REST endpoints consumed by the web UI.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"pfwebd/internal/auth"
	"pfwebd/internal/logs"
	"pfwebd/internal/pfctl"
	"pfwebd/internal/rules"
)

// Register mounts all API routes on mux. rwTables lists the PF tables
// that may be modified (by Terraform, via the API token); every other
// table is read-only. hub provides the live pflog stream; mgr owns the
// UI anchor ruleset.
//
// pfwebd is always read-only for the web UI: writes are only accepted
// from the API token (see WithSecurity).
func Register(mux *http.ServeMux, c *pfctl.Client, rwTables []string, hub *logs.Hub, mgr *rules.Manager) {
	writable := make(map[string]bool, len(rwTables))
	for _, t := range rwTables {
		writable[t] = true
	}

	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		info, err := c.Info(r.Context())
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, info)
	})

	mux.HandleFunc("GET /api/states", func(w http.ResponseWriter, r *http.Request) {
		states, err := c.States(r.Context())
		if err != nil {
			fail(w, err)
			return
		}
		if states == nil {
			states = []pfctl.State{}
		}
		writeJSON(w, states)
	})

	mux.HandleFunc("GET /api/rules", func(w http.ResponseWriter, r *http.Request) {
		rules, err := c.Rules(r.Context())
		if err != nil {
			fail(w, err)
			return
		}
		if rules == nil {
			rules = []pfctl.Rule{}
		}
		writeJSON(w, rules)
	})

	mux.HandleFunc("GET /api/interfaces", func(w http.ResponseWriter, r *http.Request) {
		raw, err := c.InterfacesRaw(r.Context())
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, map[string]string{"raw": raw})
	})

	type tableSummary struct {
		Name     string `json:"name"`
		Writable bool   `json:"writable"`
	}

	mux.HandleFunc("GET /api/tables", func(w http.ResponseWriter, r *http.Request) {
		names, err := c.Tables(r.Context())
		if err != nil {
			fail(w, err)
			return
		}
		out := []tableSummary{}
		for _, n := range names {
			out = append(out, tableSummary{Name: n, Writable: writable[n]})
		}
		writeJSON(w, out)
	})

	type tableDetail struct {
		Name      string   `json:"name"`
		Writable  bool     `json:"writable"`
		Addresses []string `json:"addresses"`
	}

	getTable := func(ctx context.Context, name string) (tableDetail, error) {
		addrs, err := c.TableAddresses(ctx, name)
		if err != nil {
			return tableDetail{}, err
		}
		if addrs == nil {
			addrs = []string{}
		}
		return tableDetail{Name: name, Writable: writable[name], Addresses: addrs}, nil
	}

	mux.HandleFunc("GET /api/tables/{name}", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if !pfctl.ValidTableName(name) {
			failStatus(w, http.StatusBadRequest, "invalid table name")
			return
		}
		detail, err := getTable(r.Context(), name)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, detail)
	})

	mux.HandleFunc("POST /api/tables/{name}", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if !pfctl.ValidTableName(name) {
			failStatus(w, http.StatusBadRequest, "invalid table name")
			return
		}
		if !writable[name] {
			failStatus(w, http.StatusForbidden, "table is read-only (see -rw-tables)")
			return
		}
		var req struct {
			Action  string `json:"action"`
			Address string `json:"address"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			failStatus(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if !pfctl.ValidAddress(req.Address) {
			failStatus(w, http.StatusBadRequest, "invalid address (expected IP or CIDR)")
			return
		}
		var err error
		switch req.Action {
		case "add":
			err = c.TableAdd(r.Context(), name, req.Address)
		case "delete":
			err = c.TableDelete(r.Context(), name, req.Address)
		default:
			failStatus(w, http.StatusBadRequest, `action must be "add" or "delete"`)
			return
		}
		if err != nil {
			fail(w, err)
			return
		}
		detail, err := getTable(r.Context(), name)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, detail)
	})

	mux.HandleFunc("GET /api/logs/stream", func(w http.ResponseWriter, r *http.Request) {
		serveLogStream(w, r, hub)
	})

	// --- UI anchor (rule editor with anti-lockout confirmation) ---

	writeAnchor := func(w http.ResponseWriter, ctx context.Context, st rules.Status) {
		live, err := mgr.AnchorLive(ctx)
		if err != nil {
			log.Printf("api: anchor live: %v", err)
			live = ""
		}
		writeJSON(w, map[string]any{
			"active":  st.Active,
			"pending": st.Pending,
			"live":    live,
		})
	}

	failRules := func(w http.ResponseWriter, err error) {
		var ve *rules.ValidationError
		switch {
		case errors.As(err, &ve):
			failStatus(w, http.StatusBadRequest, ve.Msg)
		case errors.Is(err, rules.ErrPending):
			failStatus(w, http.StatusConflict, err.Error())
		default:
			fail(w, err)
		}
	}

	mux.HandleFunc("GET /api/anchor", func(w http.ResponseWriter, r *http.Request) {
		writeAnchor(w, r.Context(), mgr.Status())
	})

	mux.HandleFunc("POST /api/anchor", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Rules []string `json:"rules"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			failStatus(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		st, err := mgr.Apply(r.Context(), req.Rules)
		if err != nil {
			failRules(w, err)
			return
		}
		writeAnchor(w, r.Context(), st)
	})

	mux.HandleFunc("POST /api/anchor/confirm", func(w http.ResponseWriter, r *http.Request) {
		st, err := mgr.Confirm()
		if err != nil {
			failRules(w, err)
			return
		}
		writeAnchor(w, r.Context(), st)
	})

	mux.HandleFunc("POST /api/anchor/cancel", func(w http.ResponseWriter, r *http.Request) {
		st, err := mgr.Cancel(r.Context())
		if err != nil {
			failRules(w, err)
			return
		}
		writeAnchor(w, r.Context(), st)
	})
}

// serveLogStream pushes pflog lines to the client as Server-Sent Events.
func serveLogStream(w http.ResponseWriter, r *http.Request, hub *logs.Hub) {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")

	rc := http.NewResponseController(w)
	// The server has a global WriteTimeout; for this long-lived response
	// we manage the deadline ourselves, per write.
	send := func(line string) error {
		_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if _, err := fmt.Fprintf(w, "data: %s\n\n", line); err != nil {
			return err
		}
		return rc.Flush()
	}

	ch, backlog, cancel := hub.Subscribe()
	defer cancel()

	for _, line := range backlog {
		if send(line) != nil {
			return
		}
	}

	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case line, ok := <-ch:
			if !ok || send(line) != nil {
				return
			}
		case <-heartbeat.C:
			// SSE comment keeps proxies and timeouts from closing us.
			_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			if rc.Flush() != nil {
				return
			}
		}
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("api: encode response: %v", err)
	}
}

func fail(w http.ResponseWriter, err error) {
	log.Printf("api: %v", err)
	failStatus(w, http.StatusInternalServerError, err.Error())
}

func failStatus(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// WithSecurity adds security headers and enforces authentication:
//   - a valid "Authorization: Bearer <token>" header authenticates the
//     request with the token's scope (this is what Terraform uses);
//     an invalid token is rejected with 401.
//
// pfwebd is always read-only: every non-GET request is rejected unless
// it is authenticated with a token carrying the "write" scope. The web
// UI is a pure dashboard and Terraform stays the single writer. If no
// token is configured, all writes are disabled.
func WithSecurity(next http.Handler, tokens *auth.Store) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")

		authed := false
		var scope auth.Scope
		if hdr := r.Header.Get("Authorization"); strings.HasPrefix(hdr, "Bearer ") {
			presented := strings.TrimPrefix(hdr, "Bearer ")
			if _, sc, ok := tokens.Lookup(presented); ok {
				authed, scope = true, sc
			} else {
				failStatus(w, http.StatusUnauthorized, "invalid API token")
				return
			}
		}

		// Writes require a token with the "write" scope.
		if r.Method != http.MethodGet {
			switch {
			case !authed:
				failStatus(w, http.StatusForbidden,
					"read-only: writes require an API token")
				return
			case scope != auth.ScopeWrite:
				failStatus(w, http.StatusForbidden,
					"this API token is read-only (write scope required)")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
