package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/eleost04/miyohub/internal/api"
	"github.com/eleost04/miyohub/internal/buildinfo"
	"github.com/eleost04/miyohub/internal/store"
)

var version = buildinfo.Version()

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := execute(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "MiyoHub:", err)
		os.Exit(1)
	}
}

func serve(ctx context.Context, state *store.Store, options cliOptions, output io.Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// Binding must succeed before any run-on-start task or scheduled exchange.
	listener, err := net.Listen("tcp", net.JoinHostPort(options.host, strconv.Itoa(options.port)))
	if err != nil {
		return fmt.Errorf("监听失败: %w", err)
	}
	defer listener.Close()
	app := api.NewServerWithOptions(state, api.Options{Version: version, PublicOrigin: os.Getenv("MIYOHUB_PUBLIC_ORIGIN"), SecureCookies: os.Getenv("MIYOHUB_SECURE_COOKIE") == "true"})
	defer app.Stop()
	if err := app.Start(); err != nil {
		return err
	}
	server := &http.Server{Handler: api.SecurityHeaders(staticHandlerAt(app.Handler(), options.webDir)), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 2 * time.Minute, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	fmt.Fprintf(output, "MiyoHub %s · http://%s\n", version, listener.Addr())
	if _, err := os.Stat(filepath.Join(options.webDir, "index.html")); err != nil {
		fmt.Fprintln(output, "前端尚未构建；请在 web 目录执行 npm ci && npm run build。当前仅提供 API。")
	}
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			_ = server.Close()
		}
		<-done
		return nil
	}
}

func getenv(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
