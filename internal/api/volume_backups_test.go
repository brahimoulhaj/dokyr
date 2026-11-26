package api

import (
	"context"
	"testing"

	"panel/internal/store"
	"panel/internal/volumebackup"
)

type volumeStub struct {
	started, restored, deleted int
	restoreErr                 error
}

func (v *volumeStub) Start(_ context.Context, app store.App, volume, conn, _ string) (store.VolumeBackup, error) {
	for _, name := range volumebackup.Volumes(app) {
		if name == volume {
			v.started++
			return store.VolumeBackup{ID: "b1", AppID: app.ID, Volume: volume, S3ConnectionID: conn, Status: store.VolumeBackupRunning}, nil
		}
	}
	return store.VolumeBackup{}, volumebackup.ErrNotVolume
}
func (v *volumeStub) Restore(context.Context, string, string) (store.VolumeBackup, error) {
	if v.restoreErr != nil {
		return store.VolumeBackup{}, v.restoreErr
	}
	v.restored++
	return store.VolumeBackup{ID: "b1", Status: store.VolumeBackupReady}, nil
}
func (v *volumeStub) Delete(context.Context, string) (store.VolumeBackup, error) {
	v.deleted++
	return store.VolumeBackup{ID: "b1", Volume: "web-data"}, nil
}

// TestVolumeBackupsAPI: anyone signed in backs up a volume the app mounts; restoring and
// deleting a backup are an admin's, and a restore under a running app is refused.
func TestVolumeBackupsAPI(t *testing.T) {
	e := newEnv(t)
	e.srv.authRate.max = 1000
	stub := &volumeStub{}
	e.signIn()
	pid := e.must(201, "POST", "/api/projects", map[string]any{"name": "Shop"})["id"].(string)
	app := e.must(201, "POST", "/api/projects/"+pid+"/apps", map[string]any{"name": "web", "image": "nginx:1", "port": 80,
		"mounts": []map[string]any{{"type": "volume", "source": "web-data", "target": "/data"}}})["id"].(string)
	// Straight into the store: the API checks a new connection against its endpoint.
	c, err := e.srv.store.CreateS3Connection(context.Background(), store.S3Input{S3Connection: store.S3Connection{Name: "s3", Endpoint: "s3.example.com", Bucket: "backups"}, SecretKey: "s"})
	if err != nil {
		t.Fatal(err)
	}
	conn := c.ID
	path := "/api/apps/" + app + "/volume-backups"

	e.must(503, "POST", path, map[string]any{"volume": "web-data", "s3_connection_id": conn})
	e.srv.SetVolumeBackups(stub)
	if got := e.must(200, "GET", path, nil); len(got["volumes"].([]any)) != 1 || got["volumes"].([]any)[0] != "web-data" || len(got["backups"].([]any)) != 0 {
		t.Fatalf("list = %v", got)
	}
	e.must(422, "POST", path, map[string]any{"volume": "web-data", "s3_connection_id": "missing"})
	e.must(422, "POST", path, map[string]any{"volume": "other", "s3_connection_id": conn})
	e.must(404, "POST", "/api/apps/missing/volume-backups", map[string]any{"volume": "web-data", "s3_connection_id": conn})

	maintainer := e.join("m@b.co", "maintainer")
	if b := maintainer.must(202, "POST", path, map[string]any{"volume": "web-data", "s3_connection_id": conn}); b["status"] != "running" || stub.started != 1 {
		t.Fatalf("started = %v", b)
	}
	maintainer.must(403, "POST", "/api/volume-backups/b1/restore", nil)
	maintainer.must(403, "DELETE", "/api/volume-backups/b1", nil)
	if stub.restored+stub.deleted != 0 {
		t.Fatal("a maintainer restored or deleted a backup")
	}

	stub.restoreErr = volumebackup.ErrRunning
	e.must(409, "POST", "/api/volume-backups/b1/restore", nil)
	stub.restoreErr = store.ErrNotFound
	e.must(404, "POST", "/api/volume-backups/b1/restore", nil)
	stub.restoreErr = nil
	e.must(202, "POST", "/api/volume-backups/b1/restore", nil)
	e.must(204, "DELETE", "/api/volume-backups/b1", nil)
	if stub.restored != 1 || stub.deleted != 1 {
		t.Fatalf("restored %d, deleted %d", stub.restored, stub.deleted)
	}
}
