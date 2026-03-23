package config

type Config struct {
	Server struct {
		ListenHost    string `yaml:"listen_host"`
		ListenPort    int    `yaml:"listen_port"`
		PublicBaseURL string `yaml:"public_base_url"`
	} `yaml:"server"`

	Database struct {
		Driver            string `yaml:"driver"`
		DSN               string `yaml:"dsn"`
		WAL               bool   `yaml:"wal"`
		BusyTimeoutMS     int    `yaml:"busy_timeout_ms"`
		MaxReadOpenConns  int    `yaml:"max_read_open_conns"`
		MaxReadIdleConns  int    `yaml:"max_read_idle_conns"`
		MaxWriteOpenConns int    `yaml:"max_write_open_conns"`
		MaxWriteIdleConns int    `yaml:"max_write_idle_conns"`
		BatchFlushMS      int    `yaml:"batch_flush_ms"`
		BatchMaxSize      int    `yaml:"batch_max_size"`
		BatchQueueSize    int    `yaml:"batch_queue_size"`
	} `yaml:"database"`

	Identity struct {
		EnrollmentMode  string `yaml:"enrollment_mode"`
		AllowSelfEnroll bool   `yaml:"allow_self_enroll"`
	} `yaml:"identity"`

	PWA struct {
		StaticDir   string `yaml:"static_dir"`
		RoutePrefix string `yaml:"route_prefix"`
	} `yaml:"pwa"`

	Center struct {
		Enabled              bool   `yaml:"enabled"`
		BaseURL              string `yaml:"base_url"`
		RegisterOnStartup    bool   `yaml:"register_on_startup"`
		HeartbeatIntervalSec int    `yaml:"heartbeat_interval_sec"`
	} `yaml:"center"`

	Hub struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
		Visibility  string `yaml:"visibility"`
	} `yaml:"hub"`

	Feishu struct {
		Enabled   bool   `yaml:"enabled"`
		AppID     string `yaml:"app_id"`
		AppSecret string `yaml:"app_secret"`
	} `yaml:"feishu"`

	Mail struct {
		Enabled    bool   `yaml:"enabled"`
		Provider   string `yaml:"provider"`
		SMTPHost   string `yaml:"smtp_host"`
		SMTPPort   int    `yaml:"smtp_port"`
		Encryption string `yaml:"smtp_encryption"`
		Username   string `yaml:"smtp_username"`
		Password   string `yaml:"smtp_password"`
		FromName   string `yaml:"from_name"`
		FromEmail  string `yaml:"from_email"`
	} `yaml:"mail"`

	Logging struct {
		Level string `yaml:"level"`
		Dir   string `yaml:"dir"`
	} `yaml:"logging"`

	Bridge struct {
		Dir string `yaml:"dir"` // path to openclaw-bridge directory
	} `yaml:"bridge"`

	// TLS enables HTTPS/WSS on the same listen port (default 9399).
	// When enabled with auto_generate, Hub creates a self-signed certificate
	// on first start. Clients must use https:// URLs and accept self-signed certs.
	//
	// If you later add nginx reverse proxy, set enabled=false and configure nginx:
	//
	//   server {
	//       listen 443 ssl;
	//       ssl_certificate     /path/to/cert.pem;
	//       ssl_certificate_key /path/to/key.pem;
	//
	//       location / {
	//           proxy_pass http://127.0.0.1:9399;
	//           proxy_http_version 1.1;
	//           proxy_set_header Upgrade $http_upgrade;
	//           proxy_set_header Connection "upgrade";
	//           proxy_set_header Host $host;
	//           proxy_set_header X-Real-IP $remote_addr;
	//           proxy_read_timeout 3600s;   # WebSocket long-lived connections
	//           proxy_send_timeout 3600s;
	//       }
	//   }
	TLS struct {
		Enabled      bool   `yaml:"enabled"`
		CertFile     string `yaml:"cert_file"`
		KeyFile      string `yaml:"key_file"`
		AutoGenerate bool   `yaml:"auto_generate"` // generate self-signed cert if cert/key missing
	} `yaml:"tls"`
}

func Default() *Config {
	cfg := &Config{}
	cfg.Server.ListenHost = "0.0.0.0"
	cfg.Server.ListenPort = 9399

	cfg.Database.Driver = "sqlite"
	cfg.Database.DSN = "./data/maclaw-hub.db"
	cfg.Database.WAL = true
	cfg.Database.BusyTimeoutMS = 5000
	cfg.Database.MaxReadOpenConns = 8
	cfg.Database.MaxReadIdleConns = 4
	cfg.Database.MaxWriteOpenConns = 1
	cfg.Database.MaxWriteIdleConns = 1
	cfg.Database.BatchFlushMS = 250
	cfg.Database.BatchMaxSize = 64
	cfg.Database.BatchQueueSize = 1024

	cfg.Identity.EnrollmentMode = "open"
	cfg.Identity.AllowSelfEnroll = true

	cfg.PWA.StaticDir = "./web/dist"
	cfg.PWA.RoutePrefix = "/app"

	cfg.Center.Enabled = true
	cfg.Center.BaseURL = "http://hubs.mypapers.top:9388"
	cfg.Center.RegisterOnStartup = true
	cfg.Center.HeartbeatIntervalSec = 30

	cfg.Hub.Name = "MaClaw Hub"
	cfg.Hub.Description = "Self-hosted MaClaw remote hub"
	cfg.Hub.Visibility = "private"

	cfg.Mail.Provider = "smtp"
	cfg.Mail.Encryption = "auto"
	cfg.Mail.FromName = "MaClaw Hub"

	cfg.Logging.Level = "info"
	cfg.Logging.Dir = "./data/logs"

	cfg.Bridge.Dir = "./openclaw-bridge"

	cfg.TLS.Enabled = false
	cfg.TLS.CertFile = "./data/tls/hub.crt"
	cfg.TLS.KeyFile = "./data/tls/hub.key"
	cfg.TLS.AutoGenerate = true

	return cfg
}
