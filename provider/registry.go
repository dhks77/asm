package provider

// Registry manages registered AI providers.
type Registry struct {
	providers   map[string]Provider
	defaultName string
	order       []string
}

// NewRegistry creates an empty provider registry.
func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]Provider),
	}
}

// Register adds a provider to the registry.
func (r *Registry) Register(p Provider) {
	name := p.Name()
	if _, exists := r.providers[name]; !exists {
		r.order = append(r.order, name)
	}
	r.providers[name] = p
}

// SetDefault sets the default provider by name.
func (r *Registry) SetDefault(name string) {
	if _, ok := r.providers[name]; ok {
		r.defaultName = name
	}
}

// Get returns a provider by name, or nil if not found.
func (r *Registry) Get(name string) Provider {
	return r.providers[name]
}

// Default returns the default provider.
func (r *Registry) Default() Provider {
	if p, ok := r.providers[r.defaultName]; ok {
		return p
	}
	// Fallback to first registered
	if len(r.order) > 0 {
		return r.providers[r.order[0]]
	}
	return nil
}

// Names returns provider names in registration order.
func (r *Registry) Names() []string {
	return r.order
}

// Count returns the number of registered providers.
func (r *Registry) Count() int {
	return len(r.providers)
}
