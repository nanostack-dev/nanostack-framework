package httpserver

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
	"github.com/nanostack-dev/nanostack-framework/pkg/health"
	nethttpmiddleware "github.com/oapi-codegen/nethttp-middleware"
	"github.com/rs/zerolog"
	"go.uber.org/fx"
)

const (
	defaultPort              = 8080
	defaultCorsMaxAge        = 300
	defaultReadHeaderTimeout = 30 * time.Second
	defaultReadTimeout       = 60 * time.Second
	defaultWriteTimeout      = 60 * time.Second
	defaultIdleTimeout       = 120 * time.Second
	defaultShutdownTimeout   = 5 * time.Second
)

type Options struct {
	Port              int
	AllowedOrigins    []string
	AllowOrigin       func(*http.Request, string) bool
	AllowedMethods    []string
	AllowedHeaders    []string
	ExposedHeaders    []string
	AllowCredentials  bool
	OpenAPI           []byte
	ValidatorBypass   func(*http.Request) bool
	ConfigureRouter   func(*chi.Mux)
	Handler           func(*chi.Mux) http.Handler
	Middlewares       []func(http.Handler) http.Handler
	Health            health.Config
	HealthExtra       func(context.Context, *http.Request) (map[string]any, error)
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
}

func (o Options) withDefaults() Options {
	if o.Port == 0 {
		o.Port = defaultPort
	}
	if len(o.AllowedMethods) == 0 {
		o.AllowedMethods = []string{
			http.MethodGet,
			http.MethodPost,
			http.MethodPut,
			http.MethodPatch,
			http.MethodDelete,
			http.MethodOptions,
		}
	}
	if len(o.AllowedHeaders) == 0 {
		o.AllowedHeaders = []string{"Accept", "Authorization", "Content-Type", "X-Request-Id"}
	}
	if o.ReadHeaderTimeout == 0 {
		o.ReadHeaderTimeout = defaultReadHeaderTimeout
	}
	if o.ReadTimeout == 0 {
		o.ReadTimeout = defaultReadTimeout
	}
	if o.WriteTimeout == 0 {
		o.WriteTimeout = defaultWriteTimeout
	}
	if o.IdleTimeout == 0 {
		o.IdleTimeout = defaultIdleTimeout
	}
	if o.ShutdownTimeout == 0 {
		o.ShutdownTimeout = defaultShutdownTimeout
	}
	return o
}

type Params struct {
	fx.In

	Lifecycle fx.Lifecycle
	Logger    zerolog.Logger
	Registry  *health.Registry `optional:"true"`
}

func NewModule(options Options) fx.Option {
	options = options.withDefaults()
	return fx.Module("httpserver", fx.Invoke(func(params Params) { Register(params, options) }))
}

func Register(params Params, options Options) {
	options = options.withDefaults()
	var server *http.Server
	params.Lifecycle.Append(fx.Hook{
		OnStart: func(context.Context) error {
			router, err := setupRouter(options, params.Logger, params.Registry)
			if err != nil {
				return err
			}
			handler := http.Handler(router)
			if options.Handler != nil {
				handler = options.Handler(router)
			}
			for i := len(options.Middlewares) - 1; i >= 0; i-- {
				handler = options.Middlewares[i](handler)
			}
			server = &http.Server{
				Addr:              ":" + strconv.Itoa(options.Port),
				Handler:           handler,
				ReadHeaderTimeout: options.ReadHeaderTimeout,
				ReadTimeout:       options.ReadTimeout,
				WriteTimeout:      options.WriteTimeout,
				IdleTimeout:       options.IdleTimeout,
			}
			go func() {
				params.Logger.Info().Int("port", options.Port).Msg("http server started")
				if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
					params.Logger.Error().Err(err).Msg("http server stopped")
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			if server == nil {
				return nil
			}
			shutdownCtx, cancel := context.WithTimeout(ctx, options.ShutdownTimeout)
			defer cancel()
			return server.Shutdown(shutdownCtx)
		},
	})
}

func setupRouter(options Options, logger zerolog.Logger, registry *health.Registry) (*chi.Mux, error) {
	router := chi.NewRouter()
	router.Use(cors.New(cors.Options{
		AllowedOrigins:   options.AllowedOrigins,
		AllowOriginFunc:  options.AllowOrigin,
		AllowedMethods:   options.AllowedMethods,
		AllowedHeaders:   options.AllowedHeaders,
		ExposedHeaders:   options.ExposedHeaders,
		AllowCredentials: options.AllowCredentials,
		MaxAge:           defaultCorsMaxAge,
	}).Handler)

	if len(options.OpenAPI) > 0 {
		validator, err := openAPIValidator(options.OpenAPI)
		if err != nil {
			return nil, err
		}
		router.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if defaultValidatorBypass(r) || (options.ValidatorBypass != nil && options.ValidatorBypass(r)) {
					next.ServeHTTP(w, r)
					return
				}
				validator(next).ServeHTTP(w, r)
			})
		})
		router.Get("/openapi.yaml", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/yaml")
			_, _ = w.Write(options.OpenAPI)
		})
	}

	if options.Health.Service != "" {
		router.Get(health.DefaultPath, func(w http.ResponseWriter, r *http.Request) {
			cfg := options.Health
			if cfg.Registry == nil {
				cfg.Registry = registry
			}
			if options.HealthExtra != nil {
				extra, err := options.HealthExtra(r.Context(), r)
				if err != nil {
					logger.Error().Err(err).Msg("health extra provider failed")
					http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
					return
				}
				cfg.Extra = extra
			}
			health.NewHandler(cfg).ServeHTTP(w, r)
		})
	}

	if options.ConfigureRouter != nil {
		options.ConfigureRouter(router)
	}
	return router, nil
}

func defaultValidatorBypass(r *http.Request) bool {
	return r.URL.Path == "/openapi.yaml" || r.URL.Path == health.DefaultPath
}

func openAPIValidator(openAPI []byte) (func(http.Handler) http.Handler, error) {
	swagger, err := openapi3.NewLoader().LoadFromData(openAPI)
	if err != nil {
		return nil, err
	}
	return nethttpmiddleware.OapiRequestValidatorWithOptions(swagger, &nethttpmiddleware.Options{
		Options: openapi3filter.Options{
			IncludeResponseStatus: true,
			MultiError:            true,
			AuthenticationFunc: func(context.Context, *openapi3filter.AuthenticationInput) error {
				return nil
			},
		},
	}), nil
}
