package pipeline

import (
	"path/filepath"

	"github.com/nanostack-dev/nanostack-framework/core/nano/project"
	"github.com/nanostack-dev/nanostack-framework/gen"
	genapi "github.com/nanostack-dev/nanostack-framework/gen/api"
	genconfig "github.com/nanostack-dev/nanostack-framework/gen/config"
	gendb "github.com/nanostack-dev/nanostack-framework/gen/db"
	generrors "github.com/nanostack-dev/nanostack-framework/gen/errors"
	genvalidate "github.com/nanostack-dev/nanostack-framework/gen/validate"
	"github.com/nanostack-dev/nanostack-framework/gen/wire"
)

// Output file names, as each generator writes them by default.
const (
	validateOut  = genvalidate.DefaultOutput
	errorsOut    = "errors.gen.go"
	errorsDoc    = "ERRORS.md"
	configOut    = "config.gen.go"
	configDoc    = "CONFIG.md"
	configEnvOut = ".env.example"
)

// plan returns the units of the selected steps, in pipeline order.
// withProject is false when nanogen/project.go could not be read: the steps
// that read it are skipped, so a broken declaration cannot move an output
// or delete the declared one as an orphan.
func (r *runner) plan(selected map[string]bool, withProject bool) []*unit {
	var units []*unit
	for i := range steps {
		s := &steps[i]
		if !selected[s.name] {
			continue
		}
		if s.project && !withProject {
			r.skipped(s, projectFile)
			continue
		}
		if s.project && !r.declared && (s.name == "db" || s.name == "api" || s.name == "validate") {
			// No declaration: db, api and validate have nothing to run on,
			// and they did not run, so their files are not orphans.
			continue
		}
		own := r.unitsOf(s)
		if len(own) == 0 && r.failedNeed(s) == "" {
			// A step with no unit, such as errors in a module without a
			// catalogue, or api and validate in a declaration without an
			// API, has run: its orphans are reported. Only a need skipped
			// for the declaration has failed yet; a step without a unit has
			// nothing that a later failure could change, and every file
			// with its header is an orphan. runUnit marks the steps with
			// units.
			r.ran[s.name] = true
		}
		units = append(units, own...)
	}
	return units
}

// unitsOf returns the units of step s: one per discovered package, or the
// declared API.
func (r *runner) unitsOf(s *step) []*unit {
	var units []*unit
	api := r.proj.API
	switch s.name {
	case "db":
		units = append(units, r.dbUnits(s)...)
	case "api":
		if api == nil {
			break
		}
		u := &unit{step: s, inputs: []string{r.abs(api.Spec)}}
		if api.Server.Dir != "" {
			u.dir = r.abs(api.Server.Dir)
			u.outputs = append(u.outputs, filepath.Join(u.dir, orDefault(api.Server.File, genapi.DefaultServerFile)),
				filepath.Join(u.dir, genapi.HandlersFile))
		}
		for _, c := range api.Clients {
			dir := r.abs(c.Dir)
			if u.dir == "" {
				u.dir = dir
			}
			u.outputs = append(u.outputs, filepath.Join(dir, orDefault(c.File, genapi.DefaultClientFile)))
		}
		units = append(units, u)
	case "validate":
		if api == nil || api.Server.Dir == "" {
			break
		}
		dir := r.abs(api.Server.Dir)
		units = append(units, &unit{step: s, dir: dir, inputs: []string{r.abs(api.Spec)},
			outputs: []string{filepath.Join(dir, validateOut)}})
	case "errors":
		for _, c := range r.found.catalogues {
			units = append(units, &unit{step: s, dir: c.dir, inputs: c.files,
				outputs: []string{filepath.Join(c.dir, errorsOut), filepath.Join(c.dir, errorsDoc)}})
		}
	case "config":
		for _, m := range r.found.manifests {
			units = append(units, &unit{step: s, dir: m.dir, inputs: m.files, outputs: []string{
				filepath.Join(m.dir, configOut), filepath.Join(m.dir, configDoc), r.envPath(m.dir),
			}})
		}
	case "wire":
		for _, m := range r.found.manifests {
			units = append(units, &unit{step: s, dir: m.dir, inputs: m.files,
				outputs: []string{filepath.Join(m.dir, wire.FileName)}})
		}
	}
	return units
}

// dbUnits returns the unit of the declared database, if any. Its outputs
// are known once the schema is: before the run they are nanogen.sum and the
// files it lists, after it the files the run produced (runUnit plans those).
func (r *runner) dbUnits(s *step) []*unit {
	if r.proj.Postgres == nil {
		return nil
	}
	p := gendb.WithDefaults(*r.proj.Postgres)
	return []*unit{{step: s, dir: r.abs(filepath.Join(p.Out, p.Name)), inputs: []string{r.abs(p.Migrations)},
		outputs: gendb.Planned(r.root, p)}}
}

// abs returns a project path as an absolute path.
func (r *runner) abs(path string) string {
	return filepath.Join(r.root, filepath.FromSlash(path))
}

// envPath is the .env.example that config writes for the package in dir.
func (r *runner) envPath(dir string) string {
	if r.proj.Env != "" {
		return r.abs(r.proj.Env)
	}
	return filepath.Join(dir, configEnvOut)
}

// runDB generates the go-jet code of the declared database. The runner
// owns the database server for the run: it starts it on the first unit that
// needs one, and stops it when the run ends.
func runDB(r *runner, _ *unit, _ map[string][]byte) (output, error) {
	files, err := gendb.Generate(gendb.Options{
		Dir:      r.root,
		Postgres: *r.proj.Postgres,
		Fresh:    r.opts.Fresh,
		Database: r.database,
	})
	return output{files: files}, err
}

// runAPI generates the server and the clients of the declared API. It
// reads the spec only, so it takes no overlay.
func runAPI(r *runner, _ *unit, _ map[string][]byte) (output, error) {
	api := r.proj.API
	opts := genapi.Options{Spec: r.abs(api.Spec)}
	if api.Server.Dir != "" {
		opts.Server = project.Package{Dir: r.abs(api.Server.Dir), File: api.Server.File}
	}
	for _, c := range api.Clients {
		c.Dir = r.abs(c.Dir)
		opts.Clients = append(opts.Clients, c)
	}
	files, err := genapi.Generate(opts)
	return output{files: files}, err
}

func runValidate(_ *runner, u *unit, overlay map[string][]byte) (output, error) {
	res, err := genvalidate.Generate(genvalidate.Options{Spec: u.inputs[0], Dir: u.dir, Overlay: overlay})
	if err != nil {
		return output{}, err
	}
	return output{files: []gen.File{res.File}, gaps: res.Gaps()}, nil
}

func runErrors(_ *runner, u *unit, overlay map[string][]byte) (output, error) {
	files, err := generrors.Generate(generrors.Options{Dir: u.dir, Overlay: overlay})
	return output{files: files}, err
}

func runConfig(r *runner, u *unit, overlay map[string][]byte) (output, error) {
	env, err := filepath.Rel(u.dir, r.envPath(u.dir))
	if err != nil {
		return output{}, gen.Errorf("", 0, "set Env in nanogen/project.go to a path relative to "+r.root,
			"the .env.example path %s cannot be reached from %s", r.envPath(u.dir), u.dir)
	}
	files, err := genconfig.Generate(genconfig.Options{Dir: u.dir, EnvExample: env, Overlay: overlay})
	return output{files: files}, err
}

func runWire(r *runner, u *unit, overlay map[string][]byte) (output, error) {
	files, err := wire.Generate(wire.Options{Dir: u.dir, Out: u.dir, Base: r.root, Overlay: overlay})
	return output{files: files}, err
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
