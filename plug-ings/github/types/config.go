package types

type Config struct {
	BotLoginId     string `yaml:"bot_login_id"`
	ClientId       string `yaml:"client_id"`
	PrivateKeyPath string `yaml:"private_key_path"`
	WebhookSecret  string `yaml:"webhook_secret"`
	AppInstallURL  string `yaml:"app_install_url"`
	BaseURL        string `yaml:"base_url"`
}
