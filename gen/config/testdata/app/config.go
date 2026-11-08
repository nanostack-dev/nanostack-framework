// Package app is the golden fixture of nano gen config.
package app

import (
	"log/slog"
	"net/url"
	"time"

	"github.com/nanostack-dev/nanostack-framework/core/secret"
	"github.com/nanostack-dev/nanostack-framework/gen/config/testdata/app/httpconf"
)

// Config is the root configuration of the fixture service.
type Config struct {
	Service
	HTTP     httpconf.Config `config:"http"`
	Postgres PostgresConfig  `config:"postgres" env:"PG"`
	Billing  BillingConfig   `config:"billing"`
	// Not configuration: computed at start-up.
	StartedAt time.Time `config:"-"`
}

// Service holds settings shared by every service; it is embedded and inlined.
type Service struct {
	// Deployment environment.
	Environment Environment `config:"environment" default:"dev" validate:"oneof=dev staging prod"`
	// Minimum log level: debug, info, warn or error.
	LogLevel slog.Level `config:"log_level" default:"info"`
	// Expose pprof handlers.
	Debug bool `config:"debug" default:"false"`
}

// Environment names a deployment environment.
type Environment string

// PostgresConfig configures the database pool.
type PostgresConfig struct {
	// Connection URL; the password is redacted in logs.
	URL *url.URL `config:"url" validate:"scheme=postgres postgresql"`
	// Maximum open connections.
	MaxConns int32 `config:"max_conns" default:"10" validate:"min=1,max=500"`
}

// BillingConfig configures invoicing.
type BillingConfig struct {
	// Days before an open invoice is overdue.
	DueDays int `config:"due_days" default:"30" validate:"min=1,max=365"`
	// Sender address for invoice mail.
	From string `config:"from" env:"BILLING_FROM"`
	// Stripe API key.
	StripeKey secret.Secret `config:"stripe_key" env:"STRIPE_KEY"`
	// Webhook signing secret; webhooks are disabled when unset.
	WebhookSecret secret.Secret `config:"webhook_secret,optional"`
	// Extra recipients of overdue notices.
	CC []string `config:"cc,optional"`
	// Currencies invoices may use.
	Currencies []string `config:"currencies" default:"usd,eur" validate:"oneof=usd eur cad"`
	// Share of the invoice charged as a late fee.
	LateFeeRate float64 `config:"late_fee_rate" default:"0.015" validate:"min=0,max=0.25"`
	// Base URL of the hosted invoice page.
	PortalURL *url.URL `config:"portal_url" default:"https://pay.example.com" validate:"scheme=https"`
}
