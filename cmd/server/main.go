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
	"github.com/swvn/eink-library/internal/hardcover"
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
	hc := hardcover.New(cfg.HardcoverToken)
	srv := &web.Server{
		Auth:        authn,
		DB:          db,
		Covers:      covers,
		Users:       userStore,
		Hardcover:   hc,
		LibraryPath: cfg.LibraryPath,
		DataDir:     cfg.DataDir,
		PageSize:    cfg.PageSize,
		SiteName:    cfg.SiteName,
		StartedAt:   time.Now(),
	}
	if hc.Enabled() {
		go srv.RunEnrichmentQueue(ctx)
	}
	if smtpSettings, err := userStore.GetSMTPSettings(); err != nil {
		log.Printf("load smtp settings: %v", err)
	} else if smtpSettings.Enabled() {
		go srv.RunDigestScheduler(ctx)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	mux.Handle("GET /static/", web.StaticHandler())
	mux.HandleFunc("GET /login", srv.LoginPage)
	mux.HandleFunc("POST /login", srv.LoginSubmit)
	mux.HandleFunc("GET /forgot-password", srv.ForgotPasswordPage)
	mux.HandleFunc("POST /forgot-password", srv.ForgotPasswordSubmit)
	mux.HandleFunc("GET /reset-password", srv.ResetPasswordPage)
	mux.HandleFunc("POST /reset-password", srv.ResetPasswordSubmit)
	mux.HandleFunc("GET /invite/accept", srv.InviteAcceptPage)
	mux.HandleFunc("POST /invite/accept", srv.InviteAcceptSubmit)
	mux.Handle("POST /logout", authn.RequireAuth(http.HandlerFunc(srv.Logout)))
	mux.Handle("GET /", authn.RequireAuth(http.HandlerFunc(srv.LibraryGrid)))
	mux.Handle("GET /authors", authn.RequireAuth(http.HandlerFunc(srv.AuthorsHandler)))
	mux.Handle("GET /series", authn.RequireAuth(http.HandlerFunc(srv.SeriesHandler)))
	mux.Handle("GET /favorites", authn.RequireAuth(http.HandlerFunc(srv.FavoritesHandler)))
	mux.Handle("GET /covers/{id}", authn.RequireAuth(http.HandlerFunc(srv.Cover)))
	mux.Handle("GET /books/{id}/download", authn.RequireAuth(http.HandlerFunc(srv.DownloadEPUB)))
	mux.Handle("GET /books/{id}/download.kepub", authn.RequireAuth(http.HandlerFunc(srv.DownloadKepub)))
	mux.Handle("POST /books/{id}/shelves/{shelfID}", authn.RequireAuth(http.HandlerFunc(srv.ShelfToggle)))
	mux.Handle("GET /admin/users", authn.RequireManageUsers(http.HandlerFunc(srv.AdminUsers)))
	mux.Handle("POST /admin/users", authn.RequireManageUsers(http.HandlerFunc(srv.AdminUsersCreate)))
	mux.Handle("POST /admin/users/{id}/role", authn.RequireManageUsers(http.HandlerFunc(srv.AdminUsersSetRole)))
	mux.Handle("POST /admin/users/{id}/delete", authn.RequireManageUsers(http.HandlerFunc(srv.AdminUsersDelete)))
	mux.Handle("POST /admin/users/{id}/reset-password", authn.RequireManageUsers(http.HandlerFunc(srv.AdminUsersResetPassword)))
	mux.Handle("POST /admin/users/invite", authn.RequireManageUsers(http.HandlerFunc(srv.AdminUsersInvite)))
	mux.Handle("POST /admin/users/{id}/invite/resend", authn.RequireManageUsers(http.HandlerFunc(srv.AdminUsersResendInvite)))
	mux.Handle("GET /admin/server", authn.RequireManageServer(http.HandlerFunc(srv.ServerInfo)))
	mux.Handle("POST /admin/server/rescan", authn.RequireManageServer(http.HandlerFunc(srv.ServerRescan)))
	mux.Handle("POST /admin/server/reimport", authn.RequireManageServer(http.HandlerFunc(srv.ServerReimport)))
	mux.Handle("POST /admin/server/enrichment-reset", authn.RequireManageServer(http.HandlerFunc(srv.ServerEnrichmentReset)))
	mux.Handle("POST /admin/server/smtp", authn.RequireManageServer(http.HandlerFunc(srv.ServerSMTPSave)))
	mux.Handle("POST /admin/server/smtp/test", authn.RequireManageServer(http.HandlerFunc(srv.ServerSMTPTest)))
	mux.Handle("GET /account/bookmark", authn.RequireAuth(http.HandlerFunc(srv.AccountBookmark)))
	mux.Handle("POST /account/bookmark/regenerate", authn.RequireAuth(http.HandlerFunc(srv.AccountBookmarkRegenerate)))
	mux.Handle("GET /account/password", authn.RequireAuth(http.HandlerFunc(srv.AccountPassword)))
	mux.Handle("POST /account/password", authn.RequireAuth(http.HandlerFunc(srv.AccountPasswordSubmit)))
	mux.Handle("POST /account/digest", authn.RequireAuth(http.HandlerFunc(srv.AccountDigestToggle)))
	mux.Handle("GET /books/{id}/hardcover-check", authn.RequireManageServer(http.HandlerFunc(srv.BookHardcoverCheck)))
	mux.Handle("POST /books/{id}/hardcover-apply", authn.RequireManageServer(http.HandlerFunc(srv.BookHardcoverApply)))

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
