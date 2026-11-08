package nano

import (
	"reflect"
	"testing"
	"time"
)

// TestAppConfigTags pins the config tags that nano gen config binds: the
// names give APP_START_TIMEOUT, APP_STOP_TIMEOUT and APP_HEALTH_TIMEOUT under
// an app section, and each default equals the default withDefaults applies,
// so a bound AppConfig and a zero one behave the same.
func TestAppConfigTags(t *testing.T) {
	t.Parallel()
	want := map[string]struct {
		name string
		def  time.Duration
	}{
		"StartTimeout":  {"start_timeout", DefaultStartTimeout},
		"StopTimeout":   {"stop_timeout", DefaultStopTimeout},
		"HealthTimeout": {"health_timeout", DefaultHealthTimeout},
		"DrainDelay":    {"drain_delay", 0},
	}
	typ := reflect.TypeFor[AppConfig]()
	if typ.NumField() != len(want) {
		t.Fatalf("AppConfig has %d fields, want %d: tag every new field", typ.NumField(), len(want))
	}
	zero := AppConfig{}.withDefaults()
	for i := range typ.NumField() {
		f := typ.Field(i)
		w, ok := want[f.Name]
		if !ok {
			t.Fatalf("unexpected field %s", f.Name)
		}
		if got := f.Tag.Get("config"); got != w.name {
			t.Errorf("%s: config tag %q, want %q", f.Name, got, w.name)
		}
		for _, other := range []string{"json", "yaml"} {
			if got := f.Tag.Get(other); got != w.name {
				t.Errorf("%s: %s tag %q, want %q", f.Name, other, got, w.name)
			}
		}
		def, err := time.ParseDuration(f.Tag.Get("default"))
		if err != nil || def != w.def {
			t.Errorf("%s: default %q (%v), want %v", f.Name, f.Tag.Get("default"), err, w.def)
		}
		if got := reflect.ValueOf(zero).Field(i).Interface(); got != w.def {
			t.Errorf("%s: withDefaults gives %v, want %v", f.Name, got, w.def)
		}
	}
}
