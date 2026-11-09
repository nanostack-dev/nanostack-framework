// Package deadlock is the state that used to deadlock nanostack gen config and
// nanostack gen wire: Config.Postgres was renamed to Config.Database, and the
// committed wire.gen.go still reads the old field. config loads with
// -tags=nanogen, which drops wire.gen.go and run.go, so it regenerates.
package deadlock

// Config was renamed from Postgres to Database.
type Config struct {
	Database DBConfig `config:"database"`
}

// DBConfig is the database setting group.
type DBConfig struct {
	// Connection URL.
	URL string `config:"url"`
}

// Store is the one provider.
type Store struct{ url string }

// NewStore returns a Store.
func NewStore(cfg DBConfig) *Store { return &Store{url: cfg.URL} }
