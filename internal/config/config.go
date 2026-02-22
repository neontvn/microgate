package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Route defines a route mapping: a URL path prefix to one or more backend servers.
// Supports both single backend (Backend field) and multiple backends (Backends field)
// for load balancing.
type Route struct {
	Path     string   `yaml:"path"`
	Backend  string   `yaml:"backend,omitempty"`  // single backend (backward compatible)
	Backends []string `yaml:"backends,omitempty"` // multiple backends for load balancing
	Strategy string   `yaml:"strategy,omitempty"` // "round-robin" or "random"
}

// GetBackends returns the list of backend URLs for this route.
// Handles both single-backend and multi-backend configs.
func (r Route) GetBackends() []string {
	if len(r.Backends) > 0 {
		return r.Backends
	}
	if r.Backend != "" {
		return []string{r.Backend}
	}
	return nil
}

// ServerConfig holds the gateway server settings.
type ServerConfig struct {
	Port int `yaml:"port"`
}

// RateLimitConfig holds rate limiter settings.
type RateLimitConfig struct {
	MaxTokens  float64 `yaml:"max_tokens"`
	RefillRate float64 `yaml:"refill_rate"`
}

// AuthConfig holds authentication settings.
type AuthConfig struct {
	APIKeys   []string `yaml:"api_keys"`
	JWTSecret string   `yaml:"jwt_secret"`
}

// CircuitBreakerConfig holds circuit breaker settings.
type CircuitBreakerConfig struct {
	Threshold int `yaml:"threshold"`
	Timeout   int `yaml:"timeout"` // seconds
}

// HealthCheckConfig holds health check settings.
type HealthCheckConfig struct {
	Interval int `yaml:"interval"` // seconds between checks
}

// DashboardConfig holds dashboard settings
type DashboardConfig struct {
	Enabled     bool `yaml:"enabled"`
	LogCapacity int  `yaml:"log_capacity"`
	SSEBuffer   int  `yaml:"sse_buffer"`
}

// ProcessConfig holds managed process settings
type ProcessConfig struct {
	ID        string   `yaml:"id"`
	Command   string   `yaml:"command"`
	Args      []string `yaml:"args"`
	Port      int      `yaml:"port"`
	AutoStart bool     `yaml:"auto_start"`
}

// Config is the top-level configuration for the gateway.
type Config struct {
	Server         ServerConfig         `yaml:"server"`
	Routes         []Route              `yaml:"routes"`
	RateLimit      RateLimitConfig      `yaml:"ratelimit"`
	Auth           AuthConfig           `yaml:"auth"`
	CircuitBreaker CircuitBreakerConfig `yaml:"circuitbreaker"`
	HealthCheck    HealthCheckConfig    `yaml:"healthcheck"`
	Dashboard      DashboardConfig      `yaml:"dashboard,omitempty"`
	Processes      []ProcessConfig      `yaml:"processes,omitempty"`
}

// LoadConfig reads a YAML config file and parses it into a Config struct.
func LoadConfig(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return &cfg, nil
}
