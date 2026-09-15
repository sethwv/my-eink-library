package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/sethwv/my-eink-library/internal/auth"
	"github.com/sethwv/my-eink-library/internal/chaptarr"
	"github.com/sethwv/my-eink-library/internal/config"
	"github.com/sethwv/my-eink-library/internal/hardcover"
	"github.com/sethwv/my-eink-library/internal/index"
	"github.com/sethwv/my-eink-library/internal/thumbnail"
	"github.com/sethwv/my-eink-library/internal/users"
	"github.com/sethwv/my-eink-library/internal/web"
)

// buildVersion is set by release builds with -ldflags. The fallback keeps
// direct go build invocations usable when no build metadata is supplied.
var buildVersion = "dev"
var buildDate = "unknown"

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
	if err := db.BackfillLibraryRoot(cfg.LibraryPaths[0]); err != nil {
		log.Fatalf("backfill library root: %v", err)
	}

	userStore, err := users.Open(filepath.Join(cfg.DataDir, "users.db"))
	if err != nil {
		log.Fatalf("open users store: %v", err)
	}
	defer userStore.Close()
	if err := userStore.Bootstrap(cfg.LibraryUser, cfg.LibraryPass); err != nil {
		log.Fatalf("bootstrap admin user: %v", err)
	}

	// SiteName/PublicURL/CoverWidth/PageSize/SessionTTL are DB-backed (admin
	// Settings page) and take precedence over env vars at every run after
	// the first: if no general_settings row exists yet, the env-var config
	// seeds it once so existing deployments don't lose their configured
	// values on upgrade, then the DB is authoritative from then on (same
	// precedence pattern as integration_settings/HARDCOVER_API_TOKEN below).
	generalSettings, err := userStore.GetGeneralSettings()
	if err != nil {
		log.Fatalf("load general settings: %v", err)
	}
	if generalSettings == (users.GeneralSettings{}) {
		generalSettings = users.GeneralSettings{
			SiteName:   cfg.SiteName,
			PublicURL:  cfg.PublicURL,
			CoverWidth: cfg.CoverWidth,
			PageSize:   cfg.PageSize,
			SessionTTL: cfg.SessionTTL,
		}
		if err := userStore.SaveGeneralSettings(generalSettings); err != nil {
			log.Fatalf("seed general settings from env vars: %v", err)
		}
	}

	covers, err := thumbnail.NewStore(filepath.Join(cfg.DataDir, "covers"), generalSettings.CoverWidth)
	if err != nil {
		log.Fatalf("open cover store: %v", err)
	}

	log.Printf("scanning library at %s", strings.Join(cfg.LibraryPaths, ", "))
	if err := db.Scan(cfg.LibraryPaths, covers); err != nil {
		log.Fatalf("initial scan: %v", err)
	}
	if n, err := db.Count(index.Filter{}); err == nil {
		log.Printf("indexed %d books", n)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	watcher, err := index.NewWatcher(cfg.LibraryPaths, db, covers)
	if err != nil {
		log.Fatalf("start watcher: %v", err)
	}
	go watcher.Run(ctx)

	// Hardcover/Chaptarr settings are DB-backed (admin Integrations page)
	// and take precedence over env vars at every run after the first: if no
	// integration_settings row exists yet, HARDCOVER_API_TOKEN (the old
	// env-var-only config) seeds it once so existing deployments don't lose
	// their token on upgrade, then the DB is authoritative from then on.
	integrationSettings, err := userStore.GetIntegrationSettings()
	if err != nil {
		log.Fatalf("load integration settings: %v", err)
	}
	if integrationSettings == (users.IntegrationSettings{}) && cfg.HardcoverToken != "" {
		integrationSettings.HardcoverEnabled = true
		integrationSettings.HardcoverToken = cfg.HardcoverToken
		if err := userStore.SaveIntegrationSettings(integrationSettings); err != nil {
			log.Fatalf("seed integration settings from HARDCOVER_API_TOKEN: %v", err)
		}
	}

	authn := auth.New(cfg.SessionSecret, generalSettings.SessionTTL, userStore)
	hc := hardcover.New(integrationSettings.HardcoverEnabled, integrationSettings.HardcoverToken)
	ch := chaptarr.New(integrationSettings.ChaptarrEnabled, integrationSettings.ChaptarrURL, integrationSettings.ChaptarrAPIKey)
	srv := &web.Server{
		Auth:         authn,
		DB:           db,
		Covers:       covers,
		Users:        userStore,
		Hardcover:    hc,
		Chaptarr:     ch,
		LibraryPaths: cfg.LibraryPaths,
		DataDir:      cfg.DataDir,
		PageSize:     generalSettings.PageSize,
		SiteName:     generalSettings.SiteName,
		PublicURL:    generalSettings.PublicURL,
		StartedAt:    time.Now(),
		BuildVersion: buildVersion,
		BuildDate:    buildDate,
	}
	// Single background loop for both integrations (idles, rather than
	// exiting, while both are disabled) — see RunEnrichmentQueue's doc
	// comment for why Chaptarr and Hardcover are tried in one deterministic
	// per-book step in the same goroutine rather than two independently
	// polling ones, which enabling either from the admin Integrations page
	// later still works without a restart.
	go srv.RunEnrichmentQueue(ctx)
	if generalSettings.PublicURL == "" {
		log.Printf("warning: Public URL is not set; password-reset and invite emails will build their links from the request's Host header, which is not safe to trust in production")
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
	mux.Handle("POST /admin/users/{id}/email", authn.RequireManageUsers(http.HandlerFunc(srv.AdminUsersSetEmail)))
	mux.Handle("GET /admin/server", authn.RequireManageServer(http.HandlerFunc(srv.ServerInfo)))
	mux.Handle("POST /admin/server/rescan", authn.RequireManageServer(http.HandlerFunc(srv.ServerRescan)))
	mux.Handle("POST /admin/server/reimport", authn.RequireManageServer(http.HandlerFunc(srv.ServerReimport)))
	mux.Handle("GET /admin/settings", authn.RequireManageServer(http.HandlerFunc(srv.AdminSettings)))
	mux.Handle("POST /admin/settings/general", authn.RequireManageServer(http.HandlerFunc(srv.AdminSettingsGeneralSave)))
	mux.Handle("POST /admin/settings/smtp", authn.RequireManageServer(http.HandlerFunc(srv.ServerSMTPSave)))
	mux.Handle("POST /admin/settings/smtp/test", authn.RequireManageServer(http.HandlerFunc(srv.ServerSMTPTest)))
	mux.Handle("GET /admin/integrations", authn.RequireManageServer(http.HandlerFunc(srv.ServerIntegrations)))
	mux.Handle("POST /admin/integrations/enrichment-reset", authn.RequireManageServer(http.HandlerFunc(srv.ServerEnrichmentReset)))
	mux.Handle("POST /admin/integrations/hardcover", authn.RequireManageServer(http.HandlerFunc(srv.ServerIntegrationsHardcoverSave)))
	mux.Handle("POST /admin/integrations/chaptarr", authn.RequireManageServer(http.HandlerFunc(srv.ServerIntegrationsChaptarrSave)))
	mux.Handle("GET /account/bookmark", authn.RequireAuth(http.HandlerFunc(srv.AccountBookmark)))
	mux.Handle("POST /account/bookmark/regenerate", authn.RequireAuth(http.HandlerFunc(srv.AccountBookmarkRegenerate)))
	mux.Handle("GET /account/password", authn.RequireAuth(http.HandlerFunc(srv.AccountPassword)))
	mux.Handle("POST /account/password", authn.RequireAuth(http.HandlerFunc(srv.AccountPasswordSubmit)))
	mux.Handle("POST /account/digest", authn.RequireAuth(http.HandlerFunc(srv.AccountDigestToggle)))
	mux.Handle("GET /books/{id}/edit-metadata", authn.RequireManageServer(http.HandlerFunc(srv.BookEditMetadata)))
	mux.Handle("POST /books/{id}/edit-metadata", authn.RequireManageServer(http.HandlerFunc(srv.BookEditMetadataSave)))

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
		log.Printf("eink-library starting on :%s (library=%s data=%s)", cfg.Port, strings.Join(cfg.LibraryPaths, ", "), cfg.DataDir)
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
