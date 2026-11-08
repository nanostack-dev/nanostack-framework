package nanopg

import (
	"testing"
	"time"
)

func TestAZeroConfigGetsThePoolDefaults(t *testing.T) {
	got := Config{Host: "db"}.withDefaults()
	if got.Port != 5432 || got.MaxOpenConns != 25 || got.ConnMaxLifetime != 5*time.Minute {
		t.Fatalf("withDefaults = %+v, want port 5432, 25 open connections and a 5m lifetime", got)
	}
	if got.MaxIdleConns != 0 {
		t.Fatalf("MaxIdleConns = %d, want 0 kept: zero is a valid setting", got.MaxIdleConns)
	}
	set := Config{Port: 6432, MaxOpenConns: 3, ConnMaxLifetime: time.Minute}.withDefaults()
	if set.Port != 6432 || set.MaxOpenConns != 3 || set.ConnMaxLifetime != time.Minute {
		t.Fatalf("withDefaults changed set values: %+v", set)
	}
}
