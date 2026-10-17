package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type ServerConfig struct {
	Port         int `yaml:"port"`
	ReadTimeout  int `yaml:"read_timeout"`
	WriteTimeout int `yaml:"write_timeout"`
	MaxHeader    int `yaml:"max_header"`
}

type DatabaseConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	Database string `yaml:"database"`
	Charset  string `yaml:"charset"`
}

func (c *DatabaseConfig) DSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=True&loc=Local",
		c.Username, c.Password, c.Host, c.Port, c.Database, c.Charset)
}

type RedisConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
}

func (c *RedisConfig) Addr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

type KafkaConfig struct {
	Brokers []string          `yaml:"brokers"`
	Topic   string            `yaml:"topic"`
	Topics  map[string]string `yaml:"topics"`
}

type JWTConfig struct {
	SecretKey string `yaml:"secret_key"`
	Expire    int    `yaml:"expire"`
	Refresh   int    `yaml:"refresh"`
}

type Config struct {
	BlogOwnerUserID uint           `yaml:"blog_owner_user_id"`
	SiteURL         string         `yaml:"site_url"`
	TrustedProxies  []string       `yaml:"trusted_proxies"`
	Server          ServerConfig   `yaml:"server"`
	Database        DatabaseConfig `yaml:"database"`
	Redis           RedisConfig    `yaml:"redis"`
	Kafka           KafkaConfig    `yaml:"kafka"`
	JWT             JWTConfig      `yaml:"jwt"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config file: %w", err)
	}

	cfg.applyEnvOverrides()
	siteURL, err := normalizeSiteURL(cfg.SiteURL)
	if err != nil {
		return nil, err
	}
	cfg.SiteURL = siteURL
	trustedProxies, err := normalizeTrustedProxies(cfg.TrustedProxies)
	if err != nil {
		return nil, err
	}
	cfg.TrustedProxies = trustedProxies
	return cfg, nil
}

func normalizeSiteURL(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}

	parsed, err := url.Parse(value)
	if err != nil {
		return "", fmt.Errorf("parse site_url: %w", err)
	}
	isHTTP := parsed.Scheme == "http" || parsed.Scheme == "https"
	hasNoPath := parsed.Path == "" || parsed.Path == "/"
	hasNoQueryOrFragment := parsed.RawQuery == "" && parsed.Fragment == "" && !parsed.ForceQuery
	if !parsed.IsAbs() || !isHTTP || parsed.Host == "" || parsed.User != nil || !hasNoPath || !hasNoQueryOrFragment {
		return "", fmt.Errorf("site_url must be an HTTP(S) origin without credentials, path, query, or fragment")
	}

	return strings.TrimRight(value, "/"), nil
}

func normalizeTrustedProxies(values []string) ([]string, error) {
	proxies := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if net.ParseIP(value) == nil {
			if _, _, err := net.ParseCIDR(value); err != nil {
				return nil, fmt.Errorf("trusted_proxies entry %q must be an IP address or CIDR", value)
			}
		}
		proxies = append(proxies, value)
	}
	return proxies, nil
}

func (c *Config) applyEnvOverrides() {
	if v := os.Getenv("BLOG_OWNER_USER_ID"); v != "" {
		id, err := strconv.ParseUint(v, 10, 32)
		if err != nil {
			c.BlogOwnerUserID = 0
		} else {
			c.BlogOwnerUserID = uint(id)
		}
	}
	if v := os.Getenv("SERVER_PORT"); v != "" {
		c.Server.Port, _ = strconv.Atoi(v)
	}

	if v := os.Getenv("DB_HOST"); v != "" {
		c.Database.Host = v
	}
	if v := os.Getenv("DB_PORT"); v != "" {
		c.Database.Port, _ = strconv.Atoi(v)
	}
	if v := os.Getenv("DB_USER"); v != "" {
		c.Database.Username = v
	}
	if v := os.Getenv("DB_PASSWORD"); v != "" {
		c.Database.Password = v
	}
	if v := os.Getenv("DB_NAME"); v != "" {
		c.Database.Database = v
	}

	if v := os.Getenv("REDIS_ADDR"); v != "" {
		c.Redis.Host, c.Redis.Port = parseAddr(v)
	}
	if v := os.Getenv("REDIS_PASSWORD"); v != "" {
		c.Redis.Password = v
	}
	if v := os.Getenv("REDIS_DB"); v != "" {
		c.Redis.DB, _ = strconv.Atoi(v)
	}

	if v := os.Getenv("JWT_SECRET"); v != "" {
		c.JWT.SecretKey = v
	}
	if v := os.Getenv("JWT_EXPIRE"); v != "" {
		c.JWT.Expire, _ = strconv.Atoi(v)
	}
	if v := os.Getenv("SITE_URL"); strings.TrimSpace(v) != "" {
		c.SiteURL = v
	}
	if v := os.Getenv("TRUSTED_PROXIES"); strings.TrimSpace(v) != "" {
		c.TrustedProxies = strings.Split(v, ",")
	}
}

func parseAddr(addr string) (string, int) {
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			port, _ := strconv.Atoi(addr[i+1:])
			return addr[:i], port
		}
	}
	return addr, 6379
}
