// Package httpconf stands in for a framework module that ships its own
// configuration struct, to prove nano gen config binds types of other packages.
package httpconf

import "time"

// Config is the HTTP server configuration.
type Config struct {
	// Address the server listens on.
	Addr string `config:"addr" default:":8080"`
	// Maximum time to read a request, headers included.
	ReadTimeout time.Duration `config:"read_timeout" default:"5s" validate:"min=100ms,max=1m"`
	// TCP port of the admin listener.
	AdminPort uint16 `config:"admin_port" default:"9090" validate:"min=1024"`
}
