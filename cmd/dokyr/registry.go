package main

import (
	"context"
	"encoding/json"
	"fmt"
	"image/color"
	"net/http"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// repository is a row of GET /api/registry/repositories: a repository of the built-in
// registry with its tags.
type repository struct {
	Name string `json:"name"`
	Size int64  `json:"size"` // all its tags, shared layers counted once
	Tags []struct {
		Name      string   `json:"name"`
		Digest    string   `json:"digest"`
		Size      int64    `json:"size"`
		Platforms []string `json:"platforms"`
	} `json:"tags"`
}

func newRegistryCmd(s streams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "registry",
		Short: "See the images in the panel's built-in registry",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}

	var asJSON bool
	list := &cobra.Command{
		Use:     "list [REPOSITORY]",
		Aliases: []string{"ls", "images"},
		Short:   "List the images pushed to the built-in registry",
		Long: "List the images pushed to the panel's built-in registry, one line per tag, with the\n" +
			"name to pull it by. With a repository, only its tags.",
		Example: "  dokyr registry list\n  dokyr registry list shop/api\n  dokyr registry list --json",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			only := ""
			if len(args) == 1 {
				only = args[0]
			}
			return runRegistryList(cmd.Context(), s, only, asJSON)
		},
	}
	list.Flags().BoolVar(&asJSON, "json", false, "print the repositories as JSON, as the API returns them")
	cmd.AddCommand(list)
	return cmd
}

func runRegistryList(ctx context.Context, s streams, only string, asJSON bool) error {
	name, pc, client, err := signedIn()
	if err != nil {
		return err
	}
	// The settings say whether the registry is on, and the address images are named by.
	var settings struct {
		Enabled bool   `json:"enabled"`
		Address string `json:"address"`
	}
	if err := client.do(ctx, http.MethodGet, "/api/registry/settings", nil, &settings); err != nil {
		return again(err, name, pc)
	}
	var raw json.RawMessage
	if err := client.do(ctx, http.MethodGet, "/api/registry/repositories", nil, &raw); err != nil {
		return again(err, name, pc)
	}
	var repos []repository
	if err := json.Unmarshal(raw, &repos); err != nil {
		return fmt.Errorf("%s answered with something that is not a list of repositories", name)
	}
	if only != "" {
		kept := repos[:0]
		for _, r := range repos {
			if r.Name == only {
				kept = append(kept, r)
			}
		}
		repos = kept
	}
	if asJSON {
		out, err := json.Marshal(repos)
		if err != nil {
			return err
		}
		if only == "" { // untouched: exactly what the API sent
			out = raw
		}
		_, err = fmt.Fprintln(s.out, string(out))
		return err
	}
	switch {
	case !settings.Enabled:
		s.say("The built-in registry is off on %s. Turn it on in the panel, under Registry.", name)
		return nil
	case len(repos) == 0 && only != "":
		return fmt.Errorf("no repository %q in the registry of %s", only, name)
	case len(repos) == 0:
		s.say("No images in the registry of %s yet. Push one to %s.", name, settings.Address)
		return nil
	}

	// A script gets the whole name to pull on each line. A terminal gets the repository
	// and the tag apart, which stays readable when tags are commit hashes, and the
	// registry's address once, below.
	headers := []string{"IMAGE", "SIZE", "PLATFORMS", "DIGEST"}
	if s.interactive {
		headers = []string{"REPOSITORY", "TAG", "SIZE", "PLATFORMS", "DIGEST"}
	}
	var rows [][]string
	var total int64
	for _, r := range repos {
		total += r.Size
		for _, t := range r.Tags {
			platforms, digest, size := strings.Join(t.Platforms, ", "), shortDigest(t.Digest), humanSize(t.Size)
			if t.Digest == "" { // a tag whose manifest the panel could not read
				platforms, digest, size = "-", "-", "-"
			} else if platforms == "" {
				platforms = "-"
			}
			if s.interactive {
				rows = append(rows, []string{r.Name, t.Name, size, platforms, digest})
				continue
			}
			ref := r.Name + ":" + t.Name
			if settings.Address != "" {
				ref = settings.Address + "/" + ref
			}
			rows = append(rows, []string{ref, size, platforms, digest})
		}
	}
	if !s.interactive {
		tw := tabwriter.NewWriter(s.out, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, strings.Join(headers, "\t"))
		for _, r := range rows {
			fmt.Fprintln(tw, strings.Join(r, "\t"))
		}
		return tw.Flush()
	}
	p := s.palette()
	s.say("%s", s.table(headers, rows, func(col int, value string) color.Color {
		if col == 4 || value == "-" {
			return p.muted
		}
		return nil
	}))
	s.say("%s", s.muted(fmt.Sprintf("%d images in %d repositories on %s, %s stored", len(rows), len(repos), name, humanSize(total))))
	if settings.Address != "" {
		s.say("%s", s.muted("Pull as "+settings.Address+"/REPOSITORY:TAG"))
	}
	return nil
}

// shortDigest is the start of a digest, enough to tell two images apart: sha256:1a2b3c4d5e6f.
func shortDigest(d string) string {
	algo, hex, ok := strings.Cut(d, ":")
	if !ok || len(hex) <= 12 {
		return d
	}
	return algo + ":" + hex[:12]
}

// humanSize is a byte count the way Docker prints it: 1.5 MB, 240 kB.
func humanSize(n int64) string {
	const unit = 1000
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	f, units := float64(n), []string{"kB", "MB", "GB", "TB"}
	i := -1
	for f >= unit && i < len(units)-1 {
		f /= unit
		i++
	}
	if f >= 100 {
		return fmt.Sprintf("%.0f %s", f, units[i])
	}
	return fmt.Sprintf("%.1f %s", f, units[i])
}
