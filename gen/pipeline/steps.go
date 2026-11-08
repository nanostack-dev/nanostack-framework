package pipeline

import (
	"path/filepath"

	"github.com/nanostack-dev/nanostack-framework/gen"
	genconfig "github.com/nanostack-dev/nanostack-framework/gen/config"
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
		var own []*unit
		if s.name != "validate" || r.proj.API != nil {
			own = r.unitsOf(s)
		} else if !r.declared {
			// No declaration: validate has nothing to run on, and it did
			// not run, so a validate.gen.go is not an orphan.
			continue
		}
		if len(own) == 0 && r.failedNeed(s) == "" {
			// A step with no unit, such as errors in a module without a
			// catalogue, or validate in a declaration without an API, has
			// run: its orphans are reported. Only a need skipped for the
			// declaration has failed yet; a step without a unit has nothing
			// that a later failure could change, and every file with its
			// header is an orphan. runUnit marks the steps with units.
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
	switch s.name {
	case "validate":
		dir := r.abs(r.proj.API.Server.Dir)
		units = append(units, &unit{step: s, dir: dir, inputs: []string{r.abs(r.proj.API.Spec)},
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
