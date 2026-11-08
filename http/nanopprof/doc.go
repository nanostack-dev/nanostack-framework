// Package nanopprof is the nano module that serves net/http/pprof on a
// separate listener, on loopback by default (Config.Addr), when ENABLE_PPROF
// is set.
//
// It replaces the deprecated fx module modules/pprof. A service lists Module
// in its manifest and puts Config in its own Config:
//
//	type Config struct {
//		Pprof nanopprof.Config `config:"pprof"`
//	}
//
// The switch keeps its name ENABLE_PPROF, which services and infrastructure
// already use, whatever section the service chooses. The other settings
// follow the section (PPROF_ADDR, ...).
package nanopprof
