package types

type Config struct {
	Version    string         `yaml:"version"`
	Permission map[string]any `yaml:"permission"`
}
