package main

import (
	"context"
	"errors"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/danielfrg/notebook/internal/httpapi"
	"github.com/danielfrg/notebook/internal/web"
)

func run(ctx context.Context, args []string) error {
	if len(args) > 0 {
		return errors.New("usage: notebook (configured with NOTEBOOK_* environment variables)")
	}
	settings, err := loadSettings()
	if err != nil {
		return err
	}
	assets, err := web.Assets()
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", settings.Addr)
	if err != nil {
		return err
	}
	log.Printf("Notebook listening at http://%s", listener.Addr())
	handler := httpapi.NewHandler(assets, httpapi.Options{
		ReadOnly:        settings.ReadOnly,
		MCPWrite:        settings.MCPWrite,
		MCPAllowedHosts: settings.MCPAllowedHosts,
		MCPExclude:      settings.MCPExclude,
	})
	return serve(ctx, listener, handler, 5*time.Second)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}
