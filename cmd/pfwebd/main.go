// pfwebd is a small web interface to monitor and (eventually) manage
// an OpenBSD PF firewall. It runs as an unprivileged user and talks to
// pfctl through doas with a strict command allowlist.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"pfwebd/internal/api"
	"pfwebd/internal/auth"
	"pfwebd/internal/logs"
	"pfwebd/internal/pfctl"
	"pfwebd/internal/rules"
	"pfwebd/internal/web"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "listen address (host:port)")
	mock := flag.Bool("mock", false, "use the mock pfctl backend (development without a firewall)")
	rwTables := flag.String("rw-tables", "",
		"comma-separated PF tables the UI may modify (empty = all tables read-only)")
	confirmTimeout := flag.Duration("confirm-timeout", 60*time.Second,
		"delay before an unconfirmed anchor change is rolled back")
	rulesFile := flag.String("rules-file", "",
		"path where the confirmed anchor ruleset is persisted (empty = no persistence)")
	tokensFile := flag.String("tokens-file", "",
		"path to the API tokens file (name scope sha256 per line); reloaded on SIGHUP")
	genToken := flag.Bool("gen-token", false,
		"generate a random API token; append it to the tokens file if writable, otherwise print the line, then exit")
	tokenName := flag.String("token-name", "client",
		"token name (label) recorded with -gen-token")
	tokenScope := flag.String("token-scope", "write",
		"token scope (\"read\" or \"write\") recorded with -gen-token")
	hashToken := flag.Bool("hash-token", false,
		"read a token on stdin, print its sha256 (for the tokens file), then exit")
	flag.Parse()

	// Token helpers: these never start the server.
	if *genToken {
		scope := auth.Scope(*tokenScope)
		if scope != auth.ScopeRead && scope != auth.ScopeWrite {
			log.Fatalf("gen-token: invalid -token-scope %q (use \"read\" or \"write\")", *tokenScope)
		}
		tok, err := auth.GenerateToken()
		if err != nil {
			log.Fatalf("gen-token: %v", err)
		}
		hash := auth.Hash(tok)
		fmt.Printf("API token (store it now, it is not saved anywhere):\n  %s\n\n", tok)

		// When pfwebd can write the tokens file (e.g. run under doas), record
		// the line directly. Otherwise (no privileges, or unwritable path) fall
		// back to printing the line for the operator to add manually.
		path := *tokensFile
		if path == "" {
			path = auth.DefaultTokensFile
		}
		if err := auth.AppendToken(path, *tokenName, scope, hash); err != nil {
			fmt.Printf("Could not write %s (%v).\nAdd this line to your tokens file manually:\n  %s  %s  %s\n",
				path, err, *tokenName, scope, hash)
		} else {
			fmt.Printf("Added token %q (scope %s) to %s.\nReload with: rcctl reload pfwebd\n", *tokenName, scope, path)
		}
		return
	}
	if *hashToken {
		sc := bufio.NewScanner(os.Stdin)
		if !sc.Scan() {
			log.Fatal("hash-token: no token read on stdin")
		}
		fmt.Println(auth.Hash(strings.TrimSpace(sc.Text())))
		return
	}

	useMock := *mock || os.Getenv("PFWEBD_MOCK") == "1"

	var runner pfctl.Runner
	var streamer pfctl.LogStreamer
	var applier rules.Applier
	if useMock {
		log.Println("running with MOCK pfctl backend (no real firewall access)")
		m := pfctl.NewMockRunner()
		runner, streamer, applier = m, m, m
	} else {
		e := pfctl.NewExecRunner(5 * time.Second)
		runner, streamer, applier = e, e, e
	}

	if *rwTables == "" && useMock {
		*rwTables = "blocklist,allowlist" // sensible default for demos
	}
	var writable []string
	for _, t := range strings.Split(*rwTables, ",") {
		if t = strings.TrimSpace(t); t != "" {
			if !pfctl.ValidTableName(t) {
				log.Fatalf("invalid table name in -rw-tables: %q", t)
			}
			writable = append(writable, t)
		}
	}
	if len(writable) > 0 {
		log.Printf("writable tables: %v", writable)
	}

	client := pfctl.NewClient(runner)

	hub := logs.NewHub()
	go hub.Run(context.Background(), streamer)

	mgr := rules.NewManager(applier, *confirmTimeout, *rulesFile)
	if err := mgr.LoadPersisted(context.Background()); err != nil {
		log.Printf("rules: could not restore persisted ruleset: %v", err)
	}

	// pfwebd is always read-only: the web UI only displays the firewall
	// configuration. The only way to write is via an API token carrying
	// the "write" scope — tokens come from the -tokens-file and/or the
	// PFWEBD_TOKEN environment variable (an implicit write token).
	tokens, err := auth.New(*tokensFile, os.Getenv("PFWEBD_TOKEN"))
	if err != nil {
		log.Fatalf("tokens: %v", err)
	}
	if *tokensFile != "" {
		warnIfReadable(*tokensFile)
	}
	if tokens.Empty() {
		log.Println("read-only: no API token configured, ALL writes are disabled (dashboard only)")
	} else {
		log.Printf("read-only: %d API token(s) loaded; writes require the \"write\" scope", tokens.Count())
	}

	// Reload tokens on SIGHUP (rcctl reload) without restarting.
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	go func() {
		for range hup {
			if err := tokens.Reload(); err != nil {
				log.Printf("tokens: reload failed, keeping previous set: %v", err)
			} else {
				log.Printf("tokens: reloaded (%d token(s))", tokens.Count())
			}
		}
	}()

	mux := http.NewServeMux()
	api.Register(mux, client, writable, hub, mgr)
	web.Register(mux)

	handler := api.WithSecurity(mux, tokens)

	srv := &http.Server{
		Addr:         *addr,
		Handler:      handler,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	log.Printf("pfwebd listening on http://%s", *addr)
	log.Fatal(srv.ListenAndServe())
}

// warnIfReadable logs a warning if the tokens file is readable by group
// or other, since it contains secret material (hashed, but still).
func warnIfReadable(path string) {
	fi, err := os.Stat(path)
	if err != nil {
		log.Printf("tokens: cannot stat %s: %v", path, err)
		return
	}
	if fi.Mode().Perm()&0o077 != 0 {
		log.Printf("tokens: %s is group/world-accessible (%#o); consider chmod 600", path, fi.Mode().Perm())
	}
}
