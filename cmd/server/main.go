package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/swvn/eink-library/internal/auth"
	"github.com/swvn/eink-library/internal/config"
	"github.com/swvn/eink-library/internal/index"
	"github.com/swvn/eink-library/internal/thumbnail"
	"github.com/swvn/eink-library/internal/users"
	"github.com/swvn/eink-library/internal/web"
)

// runHealthcheck is invoked as `server -healthcheck` from the Dockerfile's
// HEALTHCHECK instruction; distroless has no shell/curl for a CMD-SHELL probe.
func runHealthcheck() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	conn, err := net.DialTimeout("tcp", "localhost:"+port, 2*time.Second)
	if err != nil {
		os.Exit(1)
	}
	conn.Close()
	os.Exit(0)
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "-healthcheck" {
		runHealthcheck()
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		log.Fatalf("create data dir: %v", err)
	}

	db, err := index.Open(filepath.Join(cfg.DataDir, "index.db"))
	if err != nil {
		log.Fatalf("open index: %v", err)
	}
	defer db.Close()

	covers, err := thumbnail.NewStore(filepath.Join(cfg.DataDir, "covers"), cfg.CoverWidth)
	if err != nil {
		log.Fatalf("open cover store: %v", err)
	}

	log.Printf("scanning library at %s", cfg.LibraryPath)
	if err := db.Scan(cfg.LibraryPath, covers); err != nil {
		log.Fatalf("initial scan: %v", err)
	}
	if n, err := db.Count(index.Filter{}); err == nil {
		log.Printf("indexed %d books", n)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	watcher, err := index.NewWatcher(cfg.LibraryPath, db, covers)
	if err != nil {
		log.Fatalf("start watcher: %v", err)
	}
	go watcher.Run(ctx)

	userStore, err := users.Open(filepath.Join(cfg.DataDir, "users.db"))
	if err != nil {
		log.Fatalf("open users store: %v", err)
	}
	defer userStore.Close()
	if err := userStore.Bootstrap(cfg.LibraryUser, cfg.LibraryPass); err != nil {
		log.Fatalf("bootstrap admin user: %v", err)
	}

	authn := auth.New(cfg.SessionSecret, cfg.SessionTTL, userStore)
	srv := &web.Server{Auth: authn, DB: db, Covers: covers, Users: userStore, LibraryPath: cfg.LibraryPath, PageSize: cfg.PageSize}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	mux.Handle("GET /static/", web.StaticHandler())
	mux.HandleFunc("GET /login", srv.LoginPage)
	mux.HandleFunc("POST /login", srv.LoginSubmit)
	mux.Handle("POST /logout", authn.RequireAuth(http.HandlerFunc(srv.Logout)))
	mux.Handle("GET /", authn.RequireAuth(http.HandlerFunc(srv.LibraryGrid)))
	mux.Handle("GET /authors", authn.RequireAuth(http.HandlerFunc(srv.AuthorsHandler)))
	mux.Handle("GET /series", authn.RequireAuth(http.HandlerFunc(srv.SeriesHandler)))
	mux.Handle("GET /covers/{id}", authn.RequireAuth(http.HandlerFunc(srv.Cover)))
	mux.Handle("GET /books/{id}/download", authn.RequireAuth(http.HandlerFunc(srv.DownloadEPUB)))
	mux.Handle("GET /books/{id}/download.kepub", authn.RequireAuth(http.HandlerFunc(srv.DownloadKepub)))
	mux.Handle("GET /admin/users", authn.RequireAdmin(http.HandlerFunc(srv.AdminUsers)))
	mux.Handle("POST /admin/users", authn.RequireAdmin(http.HandlerFunc(srv.AdminUsersCreate)))
	mux.Handle("POST /admin/users/{id}/delete", authn.RequireAdmin(http.HandlerFunc(srv.AdminUsersDelete)))
	mux.Handle("POST /admin/users/{id}/reset-password", authn.RequireAdmin(http.HandlerFunc(srv.AdminUsersResetPassword)))

	httpSrv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           mux,
		ReadTimeout:       15 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
		// No WriteTimeout: the kepub download handler streams a live
		// conversion of potentially large books; a write deadline could
		// truncate legitimate downloads.
	}

	serveErr := make(chan error, 1)
	go func() {
		log.Printf("eink-library starting on :%s (library=%s data=%s)", cfg.Port, cfg.LibraryPath, cfg.DataDir)
		serveErr <- httpSrv.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		if err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	case <-ctx.Done():
		log.Println("shutting down...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpSrv.Shutdown(shutdownCtx); err != nil {
			log.Printf("shutdown error: %v", err)
		}
	}
}
