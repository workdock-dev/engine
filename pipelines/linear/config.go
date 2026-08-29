package linear

type Config struct {
	WebhookSecret string   `yaml:"webhook_secret"`
	IPs           []string `yaml:"ips"`
	ClientId      string   `yaml:"client_id"`
	ClientSecret  string   `yaml:"client_secret"`
	ServerUrl     string   `yaml:"server_url"`
	ApiUrl        string   `yaml:"api_url"`
	TokenUrl      string   `yaml:"token_url"`
}
