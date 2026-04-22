package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Env               string
	HTTPAddr          string
	ShutdownTimeout   time.Duration
	ReadHeaderTimeout time.Duration
	HealthzPath       string
	RedisAddr         string
	RedisPassword     string
	RedisDB           int
}

func FromEnv() (Config, error) {
	c := Config{
		Env:               getenv("ENV", "local"),
		HTTPAddr:          getenv("HTTP_ADDR", ":8080"),
		ShutdownTimeout:   getenvDuration("SHUTDOWN_TIMEOUT", 10*time.Second),
		ReadHeaderTimeout: getenvDuration("READ_HEADER_TIMEOUT", 5*time.Second),
		HealthzPath:       getenv("HEALTHZ_PATH", "/healthz"),

		RedisAddr:     getenv("REDIS_ADDR", "127.0.0.1:6379"),
		RedisPassword: getenv("REDIS_PASSWORD", ""),
		RedisDB:       getenvInt("REDIS_DB", 0),
	}

	if c.HTTPAddr == "" {
		return Config{}, fmt.Errorf("HTTP_ADDR must not be empty")
	}
	if c.HealthzPath == "" {
		return Config{}, fmt.Errorf("HEALTHZ_PATH must not be empty")
	}
	if c.RedisAddr == "" {
		return Config{}, fmt.Errorf("REDIS_ADDR must not be empty")
	}

	return c, nil
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
func getenvInt(k string, def int) int {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}
func getenvDuration(k string, def time.Duration) time.Duration {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}
