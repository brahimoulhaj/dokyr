package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"panel/internal/brand"
	"panel/internal/config"
	"panel/internal/selfbackup"
)

// restore stages a panel backup; the panel swaps it in on its next start. It never
// touches the running panel's files, so it can run next to it (docker exec).
func restore(ctx context.Context, cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	passFile := fs.String("passphrase-file", "", "file holding the backup passphrase")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("restore: give one backup file (%s-<time>%s), or - for standard input", brand.Slug, selfbackup.Extension)
	}
	source := fs.Arg(0)

	passphrase := os.Getenv(brand.EnvPrefix() + "BACKUP_PASSPHRASE")
	switch {
	case *passFile != "":
		b, err := os.ReadFile(*passFile)
		if err != nil {
			return fmt.Errorf("passphrase file: %w", err)
		}
		passphrase = strings.TrimRight(string(b), "\r\n")
	case passphrase != "":
	case source == "-":
		return fmt.Errorf("restore: reading the backup from standard input needs --passphrase-file or %sBACKUP_PASSPHRASE", brand.EnvPrefix())
	default:
		fmt.Fprint(os.Stderr, "Backup passphrase (input is visible): ")
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && line == "" {
			return errors.New("no passphrase given")
		}
		passphrase = strings.TrimRight(line, "\r\n")
	}

	var in io.Reader = os.Stdin
	if source != "-" {
		f, err := os.Open(source)
		if err != nil {
			return err
		}
		defer f.Close()
		in = f
	}
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return err
	}
	m, err := selfbackup.Stage(ctx, in, passphrase, cfg.DataDir)
	if err != nil {
		return err
	}
	fmt.Printf("Backup from %s (%s %s, schema %d) is checked and staged in %s.\n",
		m.CreatedAt.Format("2006-01-02 15:04 MST"), brand.Name, orDash(m.PanelVersion), m.SchemaVersion, cfg.DataDir)
	if m.Product != "" && m.Product != brand.Slug {
		fmt.Printf("Note: it was made by %q, not %q.\n", m.Product, brand.Slug)
	}
	fmt.Printf("Restart the panel to finish (for example: docker restart %s). The current database and key\nwill be moved to %s/pre-restore-<time> first.\n", brand.Slug, cfg.BackupDir())
	return nil
}
