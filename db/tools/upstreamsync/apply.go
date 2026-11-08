package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
)

// merger applies upstream changes to the db module, one file at a time.
type merger struct {
	dbDir     string // the db module root (our side)
	work      string // scratch directory
	baseDir   string // rewritten base
	refDir    string // rewritten ref, with the base header (to detect header-only changes)
	theirsDir string // rewritten ref, with the ref header (what is merged)
}

type conflict struct {
	path   string
	reason string
}

type applyReport struct {
	updated   []string // ours equalled the base: replaced with upstream
	headers   int      // ours equalled the base and upstream changed only the header commit
	merged    []string // both sides changed: merged cleanly
	added     []string // new upstream files
	deleted   []string // removed upstream, unchanged here
	dropped   []string // changed upstream, deleted here on purpose (see dropped)
	conflicts []conflict
	notes     []string
}

func (r applyReport) print(w io.Writer) {
	section := func(title string, paths []string) {
		for _, p := range sortStrings(paths) {
			fmt.Fprintf(w, "  %-9s %s\n", title, p)
		}
	}
	section("updated", r.updated)
	section("merged", r.merged)
	section("added", r.added)
	section("deleted", r.deleted)
	section("dropped", r.dropped)
	sort.Slice(r.conflicts, func(i, j int) bool { return r.conflicts[i].path < r.conflicts[j].path })
	for _, c := range r.conflicts {
		fmt.Fprintf(w, "  CONFLICT  %s: %s\n", c.path, c.reason)
	}
	for _, n := range r.notes {
		fmt.Fprintf(w, "  note      %s\n", n)
	}
	fmt.Fprintf(w, "%d updated, %d merged, %d added, %d deleted, %d dropped, %d header-only, %d conflicts\n",
		len(r.updated), len(r.merged), len(r.added), len(r.deleted), len(r.dropped), r.headers, len(r.conflicts))
}

func (m merger) apply(ctx context.Context, changes []change) (applyReport, error) {
	var r applyReport
	for _, c := range changes {
		if excluded(c.path) {
			continue
		}
		if err := m.applyOne(ctx, c, &r); err != nil {
			return r, fmt.Errorf("%s: %w", c.path, err)
		}
	}
	return r, nil
}

func (m merger) applyOne(ctx context.Context, c change, r *applyReport) error {
	oursPath := filepath.Join(m.dbDir, filepath.FromSlash(forkPath(c.path)))
	ours, oursErr := os.ReadFile(oursPath)
	if oursErr != nil && !errors.Is(oursErr, fs.ErrNotExist) {
		return oursErr
	}
	oursExists := oursErr == nil
	if isDropped(c.path) && !oursExists {
		if c.status != 'D' {
			r.dropped = append(r.dropped, c.path)
		}
		return nil
	}
	o := ourFile{path: oursPath, content: ours, exists: oursExists}
	switch c.status {
	case 'A':
		return m.applyAdded(ctx, c.path, o, r)
	case 'D':
		return m.applyDeleted(c.path, o, r)
	case 'M':
		return m.applyModified(ctx, c.path, o, r)
	}
	return fmt.Errorf("unexpected status %q", c.status)
}

// ourFile is the module's copy of an upstream file.
type ourFile struct {
	path    string // where the fork keeps it
	content []byte
	exists  bool
}

// applyAdded handles a file added upstream.
func (m merger) applyAdded(ctx context.Context, p string, ours ourFile, r *applyReport) error {
	theirs, mode, err := readWithMode(filepath.Join(m.theirsDir, filepath.FromSlash(p)))
	if err != nil {
		return err
	}
	switch {
	case !ours.exists:
		r.added = append(r.added, p)
		return writeFile(ours.path, theirs, mode)
	case bytes.Equal(ours.content, theirs):
		return nil
	default:
		return m.merge(ctx, p, ours.content, nil, theirs, mode, "added upstream and in Nanostack DB", r)
	}
}

// applyDeleted handles a file deleted upstream.
func (m merger) applyDeleted(p string, ours ourFile, r *applyReport) error {
	base, err := os.ReadFile(filepath.Join(m.baseDir, filepath.FromSlash(p)))
	if err != nil {
		return err
	}
	switch {
	case !ours.exists:
		return nil
	case bytes.Equal(ours.content, base):
		r.deleted = append(r.deleted, p)
		return os.Remove(ours.path)
	default:
		r.conflicts = append(r.conflicts, conflict{p, "deleted upstream, changed in Nanostack DB (file kept)"})
		return nil
	}
}

// applyModified handles a file changed upstream.
func (m merger) applyModified(ctx context.Context, p string, ours ourFile, r *applyReport) error {
	rel := filepath.FromSlash(p)
	base, err := os.ReadFile(filepath.Join(m.baseDir, rel))
	if err != nil {
		return err
	}
	theirs, mode, err := readWithMode(filepath.Join(m.theirsDir, rel))
	if err != nil {
		return err
	}
	ref, err := os.ReadFile(filepath.Join(m.refDir, rel))
	if err != nil {
		return err
	}
	headerOnly := bytes.Equal(base, ref)
	switch {
	case !ours.exists:
		if !headerOnly {
			r.conflicts = append(r.conflicts, conflict{p, "changed upstream, deleted in Nanostack DB (not restored)"})
		}
		return nil
	case bytes.Equal(ours.content, base) && headerOnly:
		r.headers++
		return writeFile(ours.path, theirs, mode)
	case bytes.Equal(ours.content, base):
		r.updated = append(r.updated, p)
		return writeFile(ours.path, theirs, mode)
	}
	return m.merge(ctx, p, ours.content, base, theirs, mode, "", r)
}

// merge runs `git merge-file` and writes the result, with conflict markers if any, to the module.
// A nil base means the file has no common ancestor (added on both sides).
func (m merger) merge(
	ctx context.Context,
	p string,
	ours, base, theirs []byte,
	mode fs.FileMode,
	reason string,
	r *applyReport,
) error {
	files := make([]string, 3)
	for i, content := range [][]byte{ours, base, theirs} {
		f, err := os.CreateTemp(m.work, "merge-")
		if err != nil {
			return err
		}
		_, werr := f.Write(content)
		if joined := errors.Join(werr, f.Close()); joined != nil {
			return joined
		}
		files[i] = f.Name()
	}
	// G204: the arguments are fixed labels and the temporary files created above.
	cmd := exec.CommandContext(ctx, "git", "merge-file", "-p", //nolint:gosec // see above
		"-L", "Nanostack DB", "-L", "upstream base", "-L", "upstream ref",
		files[0], files[1], files[2])
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		r.merged = append(r.merged, p)
	case errors.As(err, &exitErr) && exitErr.ExitCode() > 0 && exitErr.ExitCode() < 128:
		if reason == "" {
			reason = "changed upstream and in Nanostack DB"
		}
		r.conflicts = append(
			r.conflicts,
			conflict{p, fmt.Sprintf("%s; %d conflict(s) marked in the file", reason, exitErr.ExitCode())},
		)
	default:
		// Binary or unreadable input: keep our file and let a human resolve it.
		r.conflicts = append(
			r.conflicts,
			conflict{p, fmt.Sprintf("cannot merge (%s); file kept", bytes.TrimSpace(stderr.Bytes()))},
		)
		return nil
	}
	return writeFile(filepath.Join(m.dbDir, filepath.FromSlash(forkPath(p))), stdout.Bytes(), mode)
}

func readWithMode(name string) ([]byte, fs.FileMode, error) {
	info, err := os.Stat(name)
	if err != nil {
		return nil, 0, err
	}
	content, err := os.ReadFile(name)
	return content, info.Mode().Perm(), err
}

func sortStrings(s []string) []string {
	sort.Strings(s)
	return s
}
