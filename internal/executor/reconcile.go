package executor

import (
	"context"
	"errors"
)

// Report is the result of comparing the container host with what the panel knows.
type Report struct {
	Adopted []Container // managed containers that belong to a known resource
	Orphans []Container // managed containers whose resource the panel no longer knows
}

// Reconcile runs at startup. Containers keep running while the panel is down or being
// updated, so the panel adopts what is already there instead of recreating it. Orphans
// are only reported: removing a container is a decision for the owner, not for startup.
//
// known reports whether a resource of that kind and id exists in the panel's database.
func Reconcile(ctx context.Context, ex Executor, known func(kind Kind, resourceID string) bool) (Report, error) {
	list, err := ex.ListContainers(ctx)
	if err != nil {
		return Report{}, err
	}
	var r Report
	for _, c := range list {
		if c.ResourceID != "" && known(c.Kind, c.ResourceID) {
			r.Adopted = append(r.Adopted, c)
		} else {
			r.Orphans = append(r.Orphans, c)
		}
	}
	return r, nil
}

// ErrNotOrphan: the container is not one the panel has lost track of.
var ErrNotOrphan = errors.New("not an orphan container")

// Orphans finds and removes the containers of apps and databases the panel no longer
// knows: what a restore of an older backup leaves behind, since the apps and databases
// created after that backup are gone from the database while their containers run on.
//
// Only app and database containers count. The proxy and the built-in services are the
// panel's own and are never offered for removal, whatever the database says.
type Orphans struct {
	Ex Executor
	// Known reports whether the app or database of that id exists in the database.
	Known func(ctx context.Context, kind Kind, resourceID string) bool
}

// List returns the orphans, in the order the host lists its containers.
func (o Orphans) List(ctx context.Context) ([]Container, error) {
	list, err := o.Ex.ListContainers(ctx)
	if err != nil {
		return nil, err
	}
	out := []Container{}
	for _, c := range list {
		if c.ResourceID == "" || (c.Kind != KindApp && c.Kind != KindDatabase) {
			continue
		}
		if !o.Known(ctx, c.Kind, c.ResourceID) {
			out = append(out, c)
		}
	}
	return out, nil
}

// Remove stops and removes the orphan with that container id, and returns it. A
// container that is not an orphan at this moment is refused with ErrNotOrphan, so an id
// from an old listing cannot remove a container the panel has come to know since.
// Volumes are left alone.
func (o Orphans) Remove(ctx context.Context, id string) (Container, error) {
	list, err := o.List(ctx)
	if err != nil {
		return Container{}, err
	}
	for _, c := range list {
		if c.ID == id {
			return c, o.Ex.RemoveContainer(ctx, c.ID, true)
		}
	}
	return Container{}, ErrNotOrphan
}
