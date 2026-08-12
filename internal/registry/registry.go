/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

// Package registry defines the service catalog that maps service names to
// their API endpoints, authentication configuration, and available actions.
//
// Services are loaded from YAML configuration at startup. Each service
// declares its base URL, auth type (OAuth2, API key, bearer token), and
// a set of named actions with HTTP method, path template, and parameter
// schema.
package registry

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// ErrServiceNotFound is returned when a requested service is not registered.
var ErrServiceNotFound = errors.New("registry: service not found")

// ErrActionNotFound is returned when a requested action is not in the service.
var ErrActionNotFound = errors.New("registry: action not found")

// Service describes one SaaS API endpoint.
type Service struct {
	Name    string  `yaml:"name"`
	BaseURL string  `yaml:"base_url"`
	Auth    AuthCfg `yaml:"auth"`
	Actions map[string]Action `yaml:"actions"`
}

// AuthCfg holds the authentication configuration for a service.
type AuthCfg struct {
	Type         string   `yaml:"type"` // "oauth2", "api_key", "bearer"
	AuthorizeURL string   `yaml:"authorize_url,omitempty"`
	TokenURL     string   `yaml:"token_url,omitempty"`
	Scopes       []string `yaml:"scopes,omitempty"`
	HeaderName   string   `yaml:"header_name,omitempty"` // for api_key: which header
}

// Action describes one callable operation on a service.
type Action struct {
	Method string            `yaml:"method"` // GET, POST, PUT, DELETE, PATCH
	Path   string            `yaml:"path"`   // path template, e.g. /repos/{owner}/{repo}/issues
	Params map[string]string `yaml:"params"` // param_name -> type (string, int, bool, string?, etc.)
}

// Config is the top-level YAML structure for the service catalog.
type Config struct {
	Services map[string]Service `yaml:"services"`
}

// Registry holds the loaded service catalog. Goroutine-safe for reads.
type Registry struct {
	mu       sync.RWMutex
	services map[string]Service
}

// New creates an empty registry.
func New() *Registry {
	return &Registry{services: make(map[string]Service)}
}

// LoadFile loads services from a YAML file.
func (r *Registry) LoadFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("registry: read %s: %w", path, err)
	}
	return r.LoadBytes(data)
}

// LoadBytes loads services from YAML bytes.
func (r *Registry) LoadBytes(data []byte) error {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("registry: parse: %w", err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for name, svc := range cfg.Services {
		svc.Name = name
		r.services[name] = svc
	}
	return nil
}

// Get returns a service by name.
func (r *Registry) Get(name string) (Service, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	svc, ok := r.services[strings.ToLower(name)]
	if !ok {
		return Service{}, fmt.Errorf("%w: %s", ErrServiceNotFound, name)
	}
	return svc, nil
}

// GetAction returns a specific action from a service.
func (r *Registry) GetAction(serviceName, actionName string) (Service, Action, error) {
	svc, err := r.Get(serviceName)
	if err != nil {
		return Service{}, Action{}, err
	}
	action, ok := svc.Actions[actionName]
	if !ok {
		return svc, Action{}, fmt.Errorf("%w: %s.%s", ErrActionNotFound, serviceName, actionName)
	}
	return svc, action, nil
}

// List returns all registered service names, sorted.
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.services))
	for name := range r.services {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Count returns the number of registered services.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.services)
}
