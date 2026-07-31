// Command coc-progress reads a Clash of Clans village export and serves a
// dashboard showing how far each part of the base is from its ceiling, and
// what finishes when.
//
//	go run . -snapshot village.json
//
// Then open http://localhost:8080. Exports can also be dropped onto the page.
//
// Setting DATABASE_URL switches to hosted mode: accounts (GitHub/Google),
// Postgres-backed history, and per-user quotas, meant for a Vercel
// deployment rather than a laptop. See README.md for the env vars it needs.
package main

import (
	"context"
	"embed"
	"encoding/json"
	"flag"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/you/coc-progress/internal/auth"
	"github.com/you/coc-progress/internal/catalog"
	"github.com/you/coc-progress/internal/server"
	"github.com/you/coc-progress/internal/store/file"
	"github.com/you/coc-progress/internal/store/postgres"
)

//go:embed data/catalog.json
var embeddedCatalog embed.FS

//go:embed all:web/dist
var embeddedWeb embed.FS

func main() {
	addr := flag.String("addr", "", "address to listen on (default :8080, or :$PORT if set)")
	catPath := flag.String("catalog", "", "path to a catalog.json (defaults to the built-in copy)")
	snapPath := flag.String("snapshot", "", "village export(s) to load at startup, comma-separated for more than one (local mode only)")
	historyDir := flag.String("history", "", "directory to keep past exports in, for the History tab (local mode only; off by default - nothing is written to disk otherwise)")
	flag.Parse()

	cat, err := loadCatalog(*catPath)
	if err != nil {
		log.Fatalf("catalog: %v", err)
	}
	log.Printf("catalog: %d entries, generated %s", len(cat.Entries), cat.GeneratedAt)

	mux := http.NewServeMux()
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		runHosted(mux, cat, dsn)
	} else {
		runLocal(mux, cat, *snapPath, *historyDir)
	}

	site, err := fs.Sub(embeddedWeb, "web/dist")
	if err != nil {
		log.Fatalf("embedded site: %v", err)
	}
	mux.Handle("/", spa{http.FS(site)})

	listenAddr := *addr
	if listenAddr == "" {
		if p := os.Getenv("PORT"); p != "" {
			listenAddr = ":" + p // Vercel's Go runtime listens on $PORT
		} else {
			listenAddr = ":8080"
		}
	}
	srv := &http.Server{
		Addr:              listenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("listening on http://localhost%s", listenAddr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func runLocal(mux *http.ServeMux, cat *catalog.Catalog, snapPath, historyDir string) {
	cfg := server.Config{Catalog: cat, InitialSnapshotPaths: splitPaths(snapPath)}
	if historyDir != "" {
		fh, err := file.New(historyDir)
		if err != nil {
			log.Fatalf("history: %v", err)
		}
		cfg.Store = fh
		cfg.Durable = true
		log.Printf("history: keeping snapshots in %s", historyDir)
	}
	server.New(cfg, mux)
}

// splitPaths turns a comma-separated -snapshot value into a clean list, so
// loading one village or several at startup works the same way.
func splitPaths(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func runHosted(mux *http.ServeMux, cat *catalog.Catalog, dsn string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pg, err := postgres.Open(ctx, dsn)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	if err := pg.Ping(ctx); err != nil {
		log.Fatalf("database ping: %v", err)
	}
	log.Print("database: connected, schema applied")

	baseURL := strings.TrimSuffix(os.Getenv("BASE_URL"), "/")
	if baseURL == "" {
		log.Fatal("BASE_URL must be set when DATABASE_URL is set (hosted mode)")
	}
	adminEmail := os.Getenv("ADMIN_EMAIL")
	authSvc := auth.New(pg, baseURL,
		os.Getenv("GITHUB_CLIENT_ID"), os.Getenv("GITHUB_CLIENT_SECRET"),
		os.Getenv("GOOGLE_CLIENT_ID"), os.Getenv("GOOGLE_CLIENT_SECRET"),
		adminEmail)
	if len(authSvc.Providers()) == 0 {
		log.Fatal("hosted mode needs at least one of GITHUB_CLIENT_ID/GITHUB_CLIENT_SECRET or GOOGLE_CLIENT_ID/GOOGLE_CLIENT_SECRET set")
	}
	log.Printf("hosted mode: sign-in via %v", authSvc.Providers())
	if adminEmail == "" {
		log.Print("ADMIN_EMAIL not set - nobody will be promoted to admin on sign-in")
	}

	server.New(server.Config{
		Catalog:    cat,
		Store:      pg,
		Durable:    true,
		Features:   pg,
		Auth:       authSvc,
		Hosted:     true,
		BaseURL:    baseURL,
		CronSecret: os.Getenv("CRON_SECRET"),
	}, mux)
}

// spa serves static files and falls back to index.html so client-side routes
// survive a refresh.
type spa struct{ fs http.FileSystem }

func (h spa) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if path == "/" {
		path = "/index.html"
	}
	f, err := h.fs.Open(path)
	if err != nil {
		index, err := h.fs.Open("/index.html")
		if err != nil {
			httpError(w, http.StatusNotFound, "The dashboard has not been built yet. Run: cd web && npm install && npm run build")
			return
		}
		defer index.Close()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.Copy(w, index)
		return
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil || stat.IsDir() {
		httpError(w, http.StatusNotFound, "Not found.")
		return
	}
	http.ServeContent(w, r, stat.Name(), stat.ModTime(), f)
}

func loadCatalog(path string) (*catalog.Catalog, error) {
	if path != "" {
		return catalog.Load(path)
	}
	f, err := embeddedCatalog.Open("data/catalog.json")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return catalog.Read(f)
}

func httpError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
