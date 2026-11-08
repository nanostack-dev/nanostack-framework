package main

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// git runs a git command in dir and returns its standard output.
func git(ctx context.Context, dir string, env []string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

var hexRef = regexp.MustCompile(`^[0-9a-f]{7,40}$`)

// upstreamRepo returns a local go-jet repository that contains ref.
//
// With repo set, it uses that existing clone as is and never fetches. Otherwise it keeps a
// bare mirror of url in cacheDir, creating it on first use and fetching branches and tags on
// every run. A commit that is not reachable from a branch or tag is fetched by SHA.
func upstreamRepo(ctx context.Context, repo, url, cacheDir string, refs ...string) (string, error) {
	if repo != "" {
		return repo, nil
	}
	if _, statErr := os.Stat(cacheDir); errors.Is(statErr, fs.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(cacheDir), 0o750); err != nil {
			return "", err
		}
		if _, err := git(ctx, filepath.Dir(cacheDir), nil, "init", "--quiet", "--bare", cacheDir); err != nil {
			return "", err
		}
		if _, err := git(ctx, cacheDir, nil, "remote", "add", "origin", url); err != nil {
			return "", err
		}
	}
	if _, err := git(ctx, cacheDir, nil, "remote", "set-url", "origin", url); err != nil {
		return "", err
	}
	fetch := []string{"fetch", "--quiet", "--force", "--tags", "origin", "+refs/heads/*:refs/heads/*"}
	if _, err := git(ctx, cacheDir, nil, fetch...); err != nil {
		return "", err
	}
	for _, ref := range refs {
		if _, err := resolve(ctx, cacheDir, ref); err == nil || !hexRef.MatchString(ref) {
			continue
		}
		if _, err := git(ctx, cacheDir, nil, "fetch", "--quiet", "origin", ref); err != nil {
			return "", err
		}
	}
	return cacheDir, nil
}

// resolve returns the full commit SHA that ref names in repo.
func resolve(ctx context.Context, repo, ref string) (string, error) {
	out, err := git(ctx, repo, nil, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("upstream ref %q not found in %s", ref, repo)
	}
	return strings.TrimSpace(string(out)), nil
}

// submoduleCommit returns the commit a gitlink at p points to in sha, or "" if there is none.
func submoduleCommit(ctx context.Context, repo, sha, p string) string {
	out, err := git(ctx, repo, nil, "ls-tree", sha, "--", p)
	if err != nil {
		return ""
	}
	fields := strings.Fields(string(out))
	if len(fields) < 3 || fields[1] != "commit" {
		return ""
	}
	return fields[2]
}

// target is one rewritten copy of an upstream tree: files go under dir with a header naming headerSHA.
type target struct {
	dir       string
	headerSHA string
}

// extract writes the upstream tree at sha, read with `git archive` like the original import,
// into every target after applying Rewrite. It returns the set of written paths
// (slash-separated, relative to the db module root). Excluded paths are skipped.
func extract(ctx context.Context, repo, sha string, targets ...target) (map[string]bool, error) {
	cmd := exec.CommandContext(ctx, "git", "archive", "--format=tar", sha)
	cmd.Dir = repo
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		return nil, err
	}
	paths := map[string]bool{}
	readErr := readArchive(stdout, paths, targets)
	_, _ = io.Copy(io.Discard, stdout)
	if err = cmd.Wait(); err != nil {
		return nil, fmt.Errorf("git archive %s: %w: %s", sha, err, strings.TrimSpace(stderr.String()))
	}
	return paths, readErr
}

func readArchive(r io.Reader, paths map[string]bool, targets []target) error {
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		p := path.Clean(hdr.Name)
		if hdr.Typeflag != tar.TypeReg || excluded(p) {
			continue
		}
		if strings.HasPrefix(p, "../") || path.IsAbs(p) {
			return fmt.Errorf("unsafe path in archive: %s", hdr.Name)
		}
		content, err := io.ReadAll(tr)
		if err != nil {
			return err
		}
		mode := fs.FileMode(0o644)
		if hdr.Mode&0o111 != 0 {
			mode = 0o755
		}
		for _, t := range targets {
			name := filepath.Join(t.dir, filepath.FromSlash(p))
			if writeErr := writeFile(name, Rewrite(p, content, t.headerSHA), mode); writeErr != nil {
				return writeErr
			}
		}
		paths[p] = true
	}
}

func writeFile(name string, content []byte, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(name), 0o750); err != nil {
		return err
	}
	// name is under the -db module or the work directory; readArchive rejects ../ paths.
	return os.WriteFile(name, content, mode) //nolint:gosec // G703: see above
}

// scratch is a throwaway git repository used to turn directories into tree objects, so that
// `git diff` can compare them with clean paths.
type scratch struct {
	gitDir string
	n      int
}

func newScratch(ctx context.Context, dir string) (*scratch, error) {
	if _, err := git(ctx, dir, nil, "init", "--quiet", "--bare", "scratch.git"); err != nil {
		return nil, err
	}
	return &scratch{gitDir: filepath.Join(dir, "scratch.git")}, nil
}

// tree stores every file under dir as a git tree and returns its id.
func (s *scratch) tree(ctx context.Context, dir string) (string, error) {
	s.n++
	env := []string{
		"GIT_DIR=" + s.gitDir,
		"GIT_WORK_TREE=" + dir,
		fmt.Sprintf("GIT_INDEX_FILE=%s/index-%d", s.gitDir, s.n),
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}
	if _, err := git(ctx, dir, env, "-c", "core.autocrlf=false", "add", "--all", "--force", "."); err != nil {
		return "", err
	}
	out, err := git(ctx, dir, env, "write-tree")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// diff returns `git diff` output between two trees.
func (s *scratch) diff(ctx context.Context, from, to string, args ...string) ([]byte, error) {
	full := append([]string{"-c", "core.quotepath=false", "diff", "--no-color", "--no-ext-diff"}, args...)
	full = append(full, from, to)
	return git(ctx, ".", []string{"GIT_DIR=" + s.gitDir}, full...)
}

// change is one path that differs between two trees.
type change struct {
	status byte // 'A', 'M' or 'D'
	path   string
}

// changes lists the paths that differ between two trees, without rename detection.
func (s *scratch) changes(ctx context.Context, from, to string) ([]change, error) {
	out, err := s.diff(ctx, from, to, "--no-renames", "--name-status", "-z")
	if err != nil {
		return nil, err
	}
	fields := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	var result []change
	for i := 0; i+1 < len(fields); i += 2 {
		status := fields[i]
		if status == "T" {
			status = "M"
		}
		result = append(result, change{status: status[0], path: fields[i+1]})
	}
	return result, nil
}
