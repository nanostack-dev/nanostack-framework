//go:build integration

package repotest

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/nanostack-dev/nanostack-framework/gen/repo/internal/repotest/workspacescope"
)

func projectNames(rows []Project, err error) (string, error) {
	names := make([]string, len(rows))
	for i, p := range rows {
		names[i] = p.ID
	}
	slices.Sort(names)
	return strings.Join(names, ","), err
}

// The derived methods that name Workspace keep the workspace of ctx, or every
// row of the tenant when ctx carries none, and never read another tenant's
// rows. Workspace binds no parameter: the function runs on workspace_id.
func TestDerivedContextScope(t *testing.T) {
	f := newFixture(t)
	seedProjects(t, f)
	repo := NewProjectRepository(f.db)
	bare := context.Background()
	w1, w2 := workspacescope.With(bare, "w1"), workspacescope.With(bare, "w2")
	for name, tc := range map[string]struct {
		run  func() (string, error)
		want string
	}{
		"list, w1":           {func() (string, error) { return projectNames(repo.FindByTenantAndWorkspace(w1, f.tenant)) }, "p1"},
		"list, w2":           {func() (string, error) { return projectNames(repo.FindByTenantAndWorkspace(w2, f.tenant)) }, "p2"},
		"list, no workspace": {func() (string, error) { return projectNames(repo.FindByTenantAndWorkspace(bare, f.tenant)) }, "p1,p2,p3"},
		"list, other tenant": {func() (string, error) { return projectNames(repo.FindByTenantAndWorkspace(w1, f.other)) }, "q1"},
		"by name, w1": {func() (string, error) {
			return projectNames(repo.FindByTenantAndWorkspaceAndName(w1, f.tenant, "two"))
		}, ""},
		"by name, w2": {func() (string, error) {
			return projectNames(repo.FindByTenantAndWorkspaceAndName(w2, f.tenant, "two"))
		}, "p2"},
		"by name, no workspace": {func() (string, error) {
			return projectNames(repo.FindByTenantAndWorkspaceAndName(bare, f.tenant, "three"))
		}, "p3"},
		"internal skips the workspace": {func() (string, error) {
			return projectNames(repo.FindByTenantAndNameInternal(w1, f.tenant, "two"))
		}, "p2"},
		"internal keeps the tenant": {func() (string, error) {
			return projectNames(repo.FindByTenantAndNameInternal(w1, f.tenant, "other one"))
		}, ""},
	} {
		got, err := tc.run()
		if err != nil || got != tc.want {
			t.Errorf("%s: got %q, %v; want %q", name, got, err, tc.want)
		}
	}
	for name, tc := range map[string]struct {
		ctx  context.Context
		id   string
		want bool
	}{
		"own workspace": {w1, "p1", true}, "other workspace": {w1, "p2", false},
		"no workspace": {bare, "p2", true}, "row with no workspace, in w1": {w1, "p3", false},
		"row with no workspace, bare": {bare, "p3", true}, "other tenant": {bare, "q1", false},
	} {
		found, err := repo.FindByTenantAndWorkspaceAndID(tc.ctx, f.tenant, tc.id)
		if err != nil || found.IsPresent() != tc.want {
			t.Errorf("find %s: present %v, %v; want %v", name, found.IsPresent(), err, tc.want)
		}
	}
	if n, err := repo.CountByTenantAndWorkspace(w1, f.tenant); err != nil || n != 1 {
		t.Errorf("count in w1: %d, %v", n, err)
	}
	if n, err := repo.CountByTenantAndWorkspace(bare, f.tenant); err != nil || n != 3 {
		t.Errorf("count, no workspace: %d, %v", n, err)
	}
	if ok, err := repo.ExistsByTenantAndWorkspaceAndName(w1, f.tenant, "two"); err != nil || ok {
		t.Errorf("exists two in w1: %v, %v", ok, err)
	}
	if ok, err := repo.ExistsByTenantAndWorkspaceAndName(w2, f.tenant, "two"); err != nil || !ok {
		t.Errorf("exists two in w2: %v, %v", ok, err)
	}
	// A delete reaches only its workspace and its tenant.
	if ok, err := repo.DeleteByTenantAndWorkspaceAndID(w1, f.tenant, "p2"); err != nil || ok {
		t.Errorf("delete p2 from w1: %v, %v", ok, err)
	}
	if ok, err := repo.DeleteByTenantAndWorkspaceAndID(w2, f.other, "p2"); err != nil || ok {
		t.Errorf("delete p2 as the other tenant: %v, %v", ok, err)
	}
	if ok, err := repo.DeleteByTenantAndWorkspaceAndID(w2, f.tenant, "p2"); err != nil || !ok {
		t.Errorf("delete p2 from w2: %v, %v", ok, err)
	}
	if n, err := repo.CountByTenantAndWorkspace(bare, f.tenant); err != nil || n != 2 {
		t.Errorf("count after the delete: %d, %v", n, err)
	}
}
