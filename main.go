package main

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/esapi"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/spf13/viper"
)

var (
	logger         *slog.Logger
	debugLogger    *slog.Logger
	configFileName string
	config         *ConfigType
	tenMinuteTick  = time.NewTicker(10 * time.Minute)
	quit           = make(chan struct{})
	ratelimit_used = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ratelimit_used",
		Help: "Ratelimit Used Statistics",
	})
	ratelimit_remaining = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ratelimit_remaining",
		Help: "Ratelimit Remaining Statistics",
	})
	ratelimit_total = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ratelimit_total",
		Help: "Ratelimit Total Statistics",
	})
	ratelimit_reset = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ratelimit_reset",
		Help: "Ratelimit Seconds Till Reset",
	})
)

const (
	BaseENVname = "HOOK"
	webhookPath = "/webhook"
)

type ConfigType struct {
	Logging    ConfigLogging    `mapstructure:"logging"`
	Port       string           `mapstructure:"port"`
	Prometheus ConfigPrometheus `mapstructure:"prometheus"`
	Elastic    *ConfigElastic   `mapstructure:"elastic"`
	Github     ConfigGithub     `mapstructure:"github"`
}
type ConfigLogging struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
}
type ConfigPrometheus struct {
	Enabled  bool   `mapstructure:"enabled"`
	Endpoint string `mapstructure:"endpoint"`
}
type ConfigGithub struct {
	Secret          string `mapstructure:"secret"`
	Endpoint        string `mapstructure:"endpoint"`
	PublicAddress   string `mapstructure:"public_address"`
	PRPageSize      int    `mapstructure:"pr_page_size"`
	WebhookPageSize int    `mapstructure:"webhook_page_size"`
	Token           string `mapstructure:"token"`
}

func (c *ConfigGithub) populateEnv() {
	envSecret := os.Getenv(BaseENVname + "_GITHUB_SECRET")
	if envSecret != "" {
		c.Secret = envSecret
	}
	envToken := os.Getenv(BaseENVname + "_GITHUB_TOKEN")
	if envToken != "" {
		c.Token = envToken
	}
}

func (c *ConfigGithub) getWebHookURL() string {
	if c.PublicAddress == "" && c.Endpoint == "" {
		return ""
	}
	if c.PublicAddress[len(c.PublicAddress)-1] == '/' {
		if c.Endpoint[0] == '/' {
			return c.PublicAddress + c.Endpoint[1:]
		}
		return c.PublicAddress + c.Endpoint
	}
	if c.Endpoint[0] == '/' {
		return c.PublicAddress + c.Endpoint
	}
	return c.PublicAddress + "/" + c.Endpoint
}

type ConfigElastic struct {
	Addresses         []string `mapstructure:"addresses"`
	Username          string   `mapstructure:"username"`
	Password          string   `mapstructure:"password"`
	CACert            string   `mapstructure:"cacert"`
	EnableMetrics     bool     `mapstructure:"enableMetrics"`
	EnableDebugLogger bool     `mapstructure:"enableDebugLogging"`
	Index             string   `mapstructure:"index"`
}

func (c *ConfigElastic) populateEnv() {
	envPassword := os.Getenv(BaseENVname + "_ELASTIC_PASSWORD")
	if envPassword != "" {
		c.Password = envPassword
	}
}

func (cfg *ConfigElastic) getConfig() *elasticsearch.Config {
	debugLogger.Debug("reading Elatic search config")
	if cfg.Password == "" {
		debugLogger.Debug("Password empty?")
	}
	config := &elasticsearch.Config{
		Addresses:         cfg.Addresses,
		Username:          cfg.Username,
		Password:          cfg.Password,
		EnableMetrics:     cfg.EnableMetrics,
		EnableDebugLogger: cfg.EnableDebugLogger,
	}
	if cfg.CACert != "" {
		sDec, err := base64.StdEncoding.DecodeString(cfg.CACert)
		if err != nil {
			logger.Error("error decoding base64", "error", err)
			os.Exit(1)
		}
		config.CACert = sDec
	}
	return config
}

func ConfigRead(configFileName string, configOutput *ConfigType) *viper.Viper {
	configReader := viper.New()
	configReader.SetConfigName(configFileName)
	configReader.SetConfigType("yaml")
	configReader.AddConfigPath("/app/")
	configReader.AddConfigPath(".")
	configReader.SetEnvPrefix(BaseENVname)
	configReader.SetDefault("logging.level", "info")
	configReader.SetDefault("logging.format", "text")
	configReader.SetDefault("port", 8080)
	configReader.SetDefault("prometheus.enabled", true)
	configReader.SetDefault("prometheus.endpoint", "/metrics")
	configReader.SetDefault("elastic.addresses", []string{"http://localhost:9200"})
	configReader.SetDefault("elastic.username", "github-hook")
	configReader.SetDefault("elastic.enableMetrics", true)
	configReader.SetDefault("elastic.enableDebugLogging", true)
	configReader.SetDefault("elastic.index", "application-github-webhook-test")
	configReader.SetDefault("github.secret", "application-github-webhook-test")
	configReader.SetDefault("github.endpoint", "/webhook")
	configReader.SetDefault("github.pr_page_size", 50)
	configReader.SetDefault("github.webhook_page_size", 0)

	err := configReader.ReadInConfig() // Find and read the config file
	if err != nil {                    // Handle errors reading the config file
		panic(fmt.Errorf("fatal error config file: %w", err))
	}
	configReader.AutomaticEnv()
	configReader.Unmarshal(configOutput)
	return configReader
}
func setupLogging(Logging ConfigLogging) {
	logLevel := strings.ToLower(Logging.Level)
	logFormat := strings.ToLower(Logging.Format)
	loggingLevel := new(slog.LevelVar)
	switch logLevel {
	case "debug":
		loggingLevel.Set(slog.LevelDebug)
	case "warn":
		loggingLevel.Set(slog.LevelWarn)
	case "error":
		loggingLevel.Set(slog.LevelError)
	default:
		loggingLevel.Set(slog.LevelInfo)
	}

	output := os.Stdout
	switch logFormat {
	case "json":
		logger = slog.New(slog.NewJSONHandler(output, &slog.HandlerOptions{Level: loggingLevel}))
		debugLogger = slog.New(slog.NewJSONHandler(output, &slog.HandlerOptions{Level: loggingLevel, AddSource: true}))
	default:
		logger = slog.New(slog.NewTextHandler(output, &slog.HandlerOptions{Level: loggingLevel}))
		debugLogger = slog.New(slog.NewTextHandler(output, &slog.HandlerOptions{Level: loggingLevel, AddSource: true}))
	}
	logger.Info("Logging started with options", "format", Logging.Format, "level", Logging.Level, "function", "setupLogging")
	slog.SetDefault(logger)
}

func setupTestlogging() {
	loggingLevel := new(slog.LevelVar)
	loggingLevel.Set(slog.LevelDebug)
	logger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: loggingLevel, AddSource: true}))
	debugLogger = logger
}

func main() {
	flag.StringVar(&configFileName, "config", "config", "Use a different config file name")
	flag.Parse()
	config = new(ConfigType)
	ConfigRead(configFileName, config)
	config.Github.populateEnv()
	config.Elastic.populateEnv()
	setupLogging(config.Logging)
	search := initSearch(config.Elastic)
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("{\"Status\": \"UP\"}"))
	})
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Webhook Server go to /webhook"))
	})
	if config.Prometheus.Enabled {
		http.Handle(config.Prometheus.Endpoint, promhttp.Handler())
	}
	crawler := Crawler{Config: config.Github, ES: search}

	//crawler.Tick()
	defer close(quit)
	go Ticker(crawler)

	portString := fmt.Sprintf(":%v", config.Port)
	logger.Info("listeining on port " + portString)
	http.ListenAndServe(portString, nil)
}

func Ticker(crawler Crawler) {
	for {
		select {
		case <-tenMinuteTick.C:
			debugLogger.Debug("------------------------------ Tick Started ------------------------------")
			crawler.Tick()
		case <-quit:
			logger.Info("ending ticker")
			tenMinuteTick.Stop()
			return
		}
	}
}

func printESError(message string, res *esapi.Response) {
	bodyText, err := io.ReadAll(res.Body)
	if err != nil {
		logger.Error("error reading body", "error", err)
	}
	var e map[string]interface{}
	err = json.Unmarshal(bodyText, &e)
	if err != nil {
		logger.Error("error unmarshaling body", "body", bodyText, "error", err)
	}
	logger.Error(message, "status", res.Status(), "type", e["error"].(map[string]interface{})["type"], "reason", e["error"].(map[string]interface{})["reason"])
}

type Search struct {
	esClient *elasticsearch.Client
	index    string
}

func initSearch(config *ConfigElastic) *Search {
	var err error
	search := &Search{index: config.Index}
	search.esClient, err = elasticsearch.NewClient(*config.getConfig())
	if err != nil {
		logger.Error("error staring elasticsearch client", "error", err)
		os.Exit(1)
	}
	res, err := search.esClient.Indices.Exists([]string{config.Index})
	if err != nil {
		logger.Error("error checking indice exists", "error", err)
	}
	if res.StatusCode == http.StatusNotFound {
		printESError("Indice does not exist, is webhook installed?", res)
		os.Exit(3)
	} else {
		if res.StatusCode == http.StatusOK {
			logger.Info("Indice exists, starting")
		} else {
			if res.StatusCode == http.StatusUnauthorized {
				printESError("Elastic Connection Unauthorized", res)
				os.Exit(-1)
			} else {
				printESError("Unknown response", res)
				os.Exit(4)
			}
		}
	}
	return search
}
