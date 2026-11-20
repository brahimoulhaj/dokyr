package deploy

import (
	"os"
	"panel/internal/store"
	"testing"
)

func TestPrepareAppMounts(t *testing.T) {
	s := &Service{opts: Options{DataDir: t.TempDir()}}
	app := store.App{ID: "app", Mounts: []store.AppMount{
		{Type: "volume", Source: "app-data", Target: "/data"},
		{Type: "bind", Source: "/srv/uploads", Target: "/uploads", ReadOnly: true},
		{Type: "file", Target: "/etc/app.conf", Content: "first", ReadOnly: true},
	}}
	first, err := s.prepareMounts(app, store.Deployment{ID: "first"})
	if err != nil {
		t.Fatal(err)
	}
	if first[0].Volume != "app-data" || first[1].Source != "/srv/uploads" || !first[1].ReadOnly {
		t.Fatal("wrong mount mapping")
	}
	app.Mounts[2].Content = "second"
	second, err := s.prepareMounts(app, store.Deployment{ID: "second"})
	if err != nil {
		t.Fatal(err)
	}
	old, err := os.ReadFile(first[2].Source)
	if err != nil || string(old) != "first" || first[2].Source == second[2].Source {
		t.Fatal("deployments must retain their own file contents")
	}
	s.opts.DataVolume = "panel-data"
	mounted, err := s.prepareMounts(app, store.Deployment{ID: "third"})
	if err != nil || !mounted[2].VolumeFile || mounted[2].Volume != "panel-data" || mounted[2].SubPath != "deployments/app/files/third/2" {
		t.Fatalf("volume mapping: %+v, %v", mounted, err)
	}
}

// A file mount reaches the container as a bind mount of a file the panel wrote; it is
// not a host path an admin chose. The data volume's mountpoint counts the same.
func TestHostPaths(t *testing.T) {
	s := &Service{opts: Options{DataDir: t.TempDir()}}
	app := store.App{ID: "app", Mounts: []store.AppMount{
		{Type: "bind", Source: "/var/run/docker.sock", Target: "/var/run/docker.sock"},
		{Type: "file", Target: "/etc/app.conf", Content: "x"},
	}}
	mounts, err := s.prepareMounts(app, store.Deployment{ID: "d1"})
	if err != nil {
		t.Fatal(err)
	}
	binds := []string{mounts[0].Source, mounts[1].Source, "/var/lib/docker/volumes/dokyr-data/_data/deployments/app/files/d1/1"}
	got := hostPaths("app", binds)
	if len(got) != 1 || got[0] != "/var/run/docker.sock" {
		t.Fatalf("hostPaths = %v", got)
	}
	// Another app's files are not this app's.
	if got := hostPaths("other", binds); len(got) != 3 {
		t.Fatalf("hostPaths of another app = %v", got)
	}
}
