package main

import (
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/labstack/echo-contrib/echoprometheus"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/contrib/instrumentation/github.com/labstack/echo/otelecho"
	"go.opentelemetry.io/otel/attribute"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/plugin/opentelemetry/tracing"

	"github.com/concrnt/ccworld-ap-bridge/ap"
	"github.com/concrnt/ccworld-ap-bridge/apclient"
	"github.com/concrnt/ccworld-ap-bridge/api"
	"github.com/concrnt/ccworld-ap-bridge/bridge"
	apmiddleware "github.com/concrnt/ccworld-ap-bridge/middleware"
	"github.com/concrnt/ccworld-ap-bridge/store"
	"github.com/concrnt/ccworld-ap-bridge/types"
	"github.com/concrnt/ccworld-ap-bridge/worker"
	"github.com/concrnt/concrnt/client"
	"github.com/concrnt/concrnt/core"
	"github.com/concrnt/concrnt/util"
	"github.com/concrnt/concrnt/x/auth"
)

var (
	version      = "unknown"
	buildMachine = "unknown"
	buildTime    = "unknown"
	goVersion    = "unknown"
)

func main() {
	e := echo.New()

	configPaths := []string{}
	configPath := os.Getenv("CCWORLD_AP_BRIDGE_CONFIG")
	if configPath != "" {
		configPaths = append(configPaths, configPath)
	}

	additional_configs := os.Getenv("CCWORLD_AP_BRIDGE_CONFIGS")
	if additional_configs != "" {
		for v := range strings.SplitSeq(additional_configs, ":") {
			configPaths = append(configPaths, v)
		}
	}

	if len(configPaths) == 0 {
		configPaths = append(configPaths, "/etc/concrnt/config/apconfig.yaml")
	}

	config, err := util.LoadMultipleYamlFiles[Config](configPaths)
	if err != nil {
		slog.Error("Failed to load config: ", slog.String("error", err.Error()))
		panic(err)
	}

	config.ApConfig.ProxyCCID, err = core.PrivKeyToAddr(config.ApConfig.ProxyPriv, "con")
	if err != nil {
		slog.Error("Failed to load config: ", slog.String("error", err.Error()))
		panic(err)
	}

	slog.Info(fmt.Sprintf("ConcrntWorld Activitypub Bridge %s starting...", version))
	slog.Info(fmt.Sprintf("ApConfig loaded! Proxy: %s", config.ApConfig.ProxyCCID))

	config.NodeInfo.Version = "2.0"
	config.NodeInfo.Software.Name = "ccworld-ap-bridge"
	config.NodeInfo.Software.Version = version
	config.NodeInfo.Protocols = []string{"activitypub"}

	e.HidePort = true
	e.HideBanner = true

	if config.Server.EnableTrace {
		cleanup, err := util.SetupTraceProvider(config.Server.TraceEndpoint, config.ApConfig.FQDN+"/ccapi", version)
		if err != nil {
			panic(err)
		}
		defer cleanup()

		skipper := otelecho.WithSkipper(
			func(c echo.Context) bool {
				return c.Path() == "/metrics" || c.Path() == "/health"
			},
		)
		e.Use(otelecho.Middleware(config.ApConfig.FQDN, skipper))
	}

	e.Use(echoprometheus.NewMiddleware("ccapi"))
	//e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.Use(auth.ReceiveGatewayAuthPropagation)

	e.Binder = &apmiddleware.Binder{}

	gormLogger := logger.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags), // io writer
		logger.Config{
			SlowThreshold:             300 * time.Millisecond, // Slow SQL threshold
			LogLevel:                  logger.Warn,            // Log level
			IgnoreRecordNotFoundError: true,                   // Ignore ErrRecordNotFound error for logger
			Colorful:                  true,                   // Enable color
		},
	)

	db, err := gorm.Open(postgres.Open(config.Server.Dsn), &gorm.Config{
		Logger:         gormLogger,
		TranslateError: true,
	})
	if err != nil {
		panic("failed to connect database")
	}
	sqlDB, err := db.DB() // for pinging
	if err != nil {
		panic("failed to connect database")
	}
	defer sqlDB.Close()

	err = db.Use(tracing.NewPlugin(
		tracing.WithDBName("postgres"),
	))
	if err != nil {
		panic("failed to setup tracing plugin")
	}

	mc := memcache.New(config.Server.MemcachedAddr)
	defer mc.Close()

	// Migrate the schema
	log.Println("start migrate")
	db.AutoMigrate(
		&types.ApEntity{},
		&types.ApFollow{},
		&types.ApFollower{},
		&types.ApObjectReference{},
		&types.ApUserSettings{},
	)

	rdb := redis.NewClient(&redis.Options{
		Addr:     config.Server.RedisAddr,
		Password: "", // no password set
		DB:       config.Server.RedisDB,
	})
	err = redisotel.InstrumentTracing(
		rdb,
		redisotel.WithAttributes(
			attribute.KeyValue{
				Key:   "db.name",
				Value: attribute.StringValue("redis"),
			},
		),
	)
	if err != nil {
		panic("failed to setup tracing plugin")
	}

	storeService := store.NewStore(db)
	client := client.NewClient(config.Server.GatewayAddr)
	client.SetUserAgent("CCWorld-AP-Bridge", version)
	apclient := apclient.NewApClient(mc, storeService, config.ApConfig)

	bridge := bridge.NewService(storeService, client, apclient, config.ApConfig)

	apService := ap.NewService(
		storeService,
		client,
		apclient,
		bridge,
		config.NodeInfo,
		config.ApConfig,
	)

	apiService := api.NewService(storeService, client, apclient, bridge, config.ApConfig)
	apiHandler := api.NewHandler(apiService)

	apHandler := ap.NewHandler(apService)

	worker := worker.NewWorker(rdb, storeService, client, apclient, bridge, config.ApConfig)
	go worker.Run()

	e.GET("/cc-info", func(c echo.Context) error {
		return c.JSON(http.StatusOK, core.CCInfo{
			Name:    "github.com/concrnt/ccworld-ap-bridge",
			Version: version,
		})
	})

	e.GET("/.well-known/host-meta", apHandler.HostMeta)
	e.GET("/.well-known/webfinger", apHandler.WebFinger)
	e.GET("/.well-known/nodeinfo", apHandler.NodeInfoWellKnown)

	ap := e.Group("/ap")
	ap.GET("/nodeinfo/2.0", apHandler.NodeInfo)
	ap.GET("/acct/:id", apHandler.User)
	ap.POST("/acct/:id/inbox", apHandler.Inbox)
	ap.GET("/acct/:id/outbox", apHandler.Outbox)
	ap.GET("/note/:id", apHandler.Note)

	ap.POST("/inbox", apHandler.Inbox)

	ap.GET("/api/entity", apiHandler.GetEntity, auth.Restrict(auth.ISREGISTERED))                      // ISLOCAL
	ap.GET("/api/entity/:ccid", apiHandler.GetEntity, auth.Restrict(auth.ISREGISTERED))                // ISLOCAL
	ap.POST("/api/entity", apiHandler.CreateEntity, auth.Restrict(auth.ISREGISTERED))                  // ISLOCAL
	ap.POST("/api/follow/:id", apiHandler.Follow, auth.Restrict(auth.ISREGISTERED))                    // ISLOCAL
	ap.DELETE("/api/follow/:id", apiHandler.UnFollow, auth.Restrict(auth.ISREGISTERED))                // ISLOCAL
	ap.GET("/api/resolve/:id", apiHandler.ResolvePerson, auth.Restrict(auth.ISREGISTERED))             // ISLOCAL
	ap.GET("/api/stats", apiHandler.GetStats, auth.Restrict(auth.ISREGISTERED))                        // ISLOCAL
	ap.POST("/api/entities/aliases", apiHandler.UpdateEntityAliases, auth.Restrict(auth.ISREGISTERED)) // ISLOCAL
	ap.GET("/api/import", apiHandler.ImportNote, auth.Restrict(auth.ISREGISTERED))                     // ISLOCAL
	ap.GET("/api/settings", apiHandler.GetUserSettings, auth.Restrict(auth.ISREGISTERED))              // ISLOCAL
	ap.POST("/api/settings", apiHandler.UpdateUserSettings, auth.Restrict(auth.ISREGISTERED))          // ISLOCAL

	e.GET("/health", func(c echo.Context) (err error) {
		ctx := c.Request().Context()

		err = sqlDB.Ping()
		if err != nil {
			return c.String(http.StatusInternalServerError, "db error")
		}

		err = rdb.Ping(ctx).Err()
		if err != nil {
			return c.String(http.StatusInternalServerError, "redis error")
		}

		return c.String(http.StatusOK, "ok")
	})

	e.GET("/metrics", echoprometheus.NewHandler())

	port := ":8000"
	envport := os.Getenv("CC_AP_PORT")
	if envport != "" {
		port = ":" + envport
	}

	e.Logger.Fatal(e.Start(port))
}
