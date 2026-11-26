package api

import (
	"context"
	"testing"

	"panel/internal/executor"
)

// TestOrphanContainers: an admin sees and removes the containers of apps and databases
// the panel no longer knows, and nothing else: not a known app's container, not the
// panel's own services, and not for a maintainer.
func TestOrphanContainers(t *testing.T) {
	e := newEnv(t)
	e.srv.authRate.max = 1000
	ctx := context.Background()
	create := func(name string, kind executor.Kind, resource string) string {
		id, err := e.ex.CreateContainer(ctx, executor.ContainerSpec{Name: name, Image: "img", Kind: kind, ResourceID: resource})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	gone := create("app-gone", executor.KindApp, "gone")
	lostDB := create("db-gone", executor.KindDatabase, "gone-db")
	known := create("app-known", executor.KindApp, "known")
	proxy := create("proxy", executor.KindProxy, "unknown-proxy")
	e.srv.SetOrphans(executor.Orphans{Ex: e.ex, Known: func(_ context.Context, _ executor.Kind, id string) bool { return id == "known" }})

	e.must(401, "GET", "/api/orphans", nil)
	e.signIn()
	maintainer := e.join("m@b.co", "maintainer")
	maintainer.must(403, "GET", "/api/orphans", nil)
	maintainer.must(403, "DELETE", "/api/orphans/"+gone, nil)

	list := e.list("/api/orphans")
	if len(list) != 2 {
		t.Fatalf("orphans = %v, want the forgotten app and database", list)
	}
	for _, o := range list {
		if o["id"] != gone && o["id"] != lostDB {
			t.Fatalf("listed %v", o)
		}
	}
	e.must(404, "DELETE", "/api/orphans/"+known, nil)
	e.must(404, "DELETE", "/api/orphans/"+proxy, nil)
	e.must(404, "DELETE", "/api/orphans/nope", nil)
	e.must(204, "DELETE", "/api/orphans/"+gone, nil)
	e.must(404, "DELETE", "/api/orphans/"+gone, nil)
	if left := e.list("/api/orphans"); len(left) != 1 || left[0]["id"] != lostDB {
		t.Fatalf("after the removal: %v", left)
	}
	cs, _ := e.ex.ListContainers(ctx)
	if len(cs) != 3 {
		t.Fatalf("%d containers left, want the known app, the proxy and the database", len(cs))
	}
}
