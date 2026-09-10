// Command tasker serves the time tracker: registration and sign-in, companies,
// any number of timers running at once, and the history of where the time went.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
	// The binary carries its own zone database. History is reckoned in the zone
	// each user's browser reported, and on a server image without
	// /usr/share/zoneinfo every zone would otherwise fall back to UTC without a
	// word.
	_ "time/tzdata"

	"github.com/urfave/cli/v3"

	"github.com/atvirokodosprendimai/app-taskerv1/internal/auth"
	"github.com/atvirokodosprendimai/app-taskerv1/internal/store"
	"github.com/atvirokodosprendimai/app-taskerv1/internal/tracking"
	"github.com/atvirokodosprendimai/app-taskerv1/internal/web"
	"github.com/atvirokodosprendimai/app-taskerv1/migrations"
)

// shutdownTimeout is how long requests already in progress get to finish once
// the process is asked to stop.
const shutdownTimeout = 10 * time.Second

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(log, os.Args); err != nil {
		log.Error("tasker stopped", "err", err)
		os.Exit(1)
	}
}

// run parses the command line and serves until the process is signalled.
func run(log *slog.Logger, args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cmd := &cli.Command{
		Name:  "tasker",
		Usage: "a multi-user time tracker that runs several timers at once",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "addr",
				Value:   ":8080",
				Usage:   "address to listen on",
				Sources: cli.EnvVars("TASKER_ADDR"),
			},
			&cli.StringFlag{
				Name:    "db",
				Value:   "data/tasker.db",
				Usage:   "SQLite database file, created if it does not exist",
				Sources: cli.EnvVars("TASKER_DB"),
			},
			&cli.BoolFlag{
				Name:    "secure-cookies",
				Usage:   "mark the session cookie Secure: required behind HTTPS, and breaks sign-in over plain HTTP",
				Sources: cli.EnvVars("TASKER_SECURE_COOKIES"),
			},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			return serve(ctx, log, c.String("addr"), c.String("db"), c.Bool("secure-cookies"))
		},
	}
	return cmd.Run(ctx, args)
}

// serve opens the database, brings its schema up to date, and serves HTTP until
// ctx is cancelled.
func serve(ctx context.Context, log *slog.Logger, addr, dbPath string, secureCookies bool) error {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o750); err != nil {
		return fmt.Errorf("create database directory: %w", err)
	}
	db, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.Migrate(ctx, migrations.FS); err != nil {
		return err
	}

	users := auth.NewRepo(db.Read, db.Write)
	timers := tracking.NewRepo(db.Read, db.Write)
	closing := make(chan struct{})
	app := &web.App{
		Log:      log,
		Sessions: web.NewSessions(db.Write, secureCookies),
		Users:    users,
		Tracking: timers,
		Auth:     auth.NewService(users),
		Tracker:  tracking.NewService(timers),
		Bus:      web.NewBus(),
		Now:      time.Now,
		Closing:  closing,
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           app.Routes(),
		ReadHeaderTimeout: web.ReadHeaderTimeout,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}
	// Shutdown stops accepting connections and waits for requests to finish,
	// but it cancels none of them — and a dashboard stream never finishes by
	// itself. Closing this channel is what ends the streams, so a shutdown
	// waits for the slowest ordinary request rather than for its whole timeout.
	srv.RegisterOnShutdown(func() { close(closing) })

	if !secureCookies {
		log.Warn("the session cookie is not marked Secure; pass --secure-cookies when serving over HTTPS")
	}

	serveErr := make(chan error, 1)
	go func() {
		log.Info("tasker listening", "addr", addr, "db", dbPath)
		serveErr <- srv.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		// Until Shutdown is called, ListenAndServe returns only on failure.
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}

	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shut down: %w", err)
	}
	log.Info("stopped")
	return nil
}
