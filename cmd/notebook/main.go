package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/danielfrg/notebook/internal/httpapi"
	"github.com/danielfrg/notebook/internal/vault"
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
	reg, err := openVaults(settings)
	if err != nil {
		return err
	}
	defer reg.Close()
	assets, err := web.Assets()
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", settings.Addr)
	if err != nil {
		return err
	}
	log.Printf("Notebook listening at http://%s", listener.Addr())
	handler := httpapi.NewHandler(assets, reg, httpapi.Options{
		ReadOnly:        settings.ReadOnly,
		MCPWrite:        settings.MCPWrite,
		MCPAllowedHosts: settings.MCPAllowedHosts,
		MCPExclude:      settings.MCPExclude,
	})
	return serve(ctx, listener, handler, 5*time.Second)
}

// openVaults opens every configured vault, in the order they are configured. It
// fails before anything is served if the excludes of a vault are not patterns
// that vault.ExcludeGlobs takes, or if a vault cannot be opened, and the error
// names the variable and the vault: an exclude that hides nothing from the MCP
// tools, or a vault that is not there, is a start that should not happen.
func openVaults(settings Settings) (*vault.Registry, error) {
	for _, spec := range settings.Vaults {
		if _, err := vault.ExcludeGlobs(settings.MCPExclude[spec.Name]); err != nil {
			return nil, fmt.Errorf("NOTEBOOK_MCP_EXCLUDE: vault %q: %w", spec.Name, err)
		}
	}
	vaults := make([]*vault.Vault, 0, len(settings.Vaults))
	closeAll := func() {
		for _, v := range vaults {
			v.Close()
		}
	}
	for _, spec := range settings.Vaults {
		v, err := vault.Open(spec.Name, spec.Path, vault.Options{ReadOnly: settings.ReadOnly})
		if err != nil {
			closeAll()
			return nil, fmt.Errorf("NOTEBOOK_VAULTS: %w", err)
		}
		vaults = append(vaults, v)
	}
	reg, err := vault.NewRegistry(vaults...)
	if err != nil {
		closeAll()
		return nil, err
	}
	return reg, nil
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}
