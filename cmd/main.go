package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"github.com/redis/go-redis/v9"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/bradfitz/gomemcache/memcache"

	"github.com/labstack/echo-contrib/echoprometheus"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	"github.com/totegamma/concurrent/client"
	"github.com/totegamma/concurrent/x/auth"

	"github.com/redis/go-redis/extra/redisotel/v9"
	"go.opentelemetry.io/contrib/instrumentation/github.com/labstack/echo/otelecho"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.7.0"
	"gorm.io/plugin/opentelemetry/tracing"

	"github.com/concrnt/ccworld-ap-bridge/ap"
	"github.com/concrnt/ccworld-ap-bridge/api"
	apmiddleware "github.com/concrnt/ccworld-ap-bridge/middleware"
	"github.com/concrnt/ccworld-ap-bridge/store"
	"github.com/concrnt/ccworld-ap-bridge/types"
)

var (
	version      = "unknown"
	buildMachine = "unknown"
	buildTime    = "unknown"
	goVersion    = "unknown"
)

func main() {
	e := echo.New()

	config := Config{}
	ConfPath := os.Getenv("CCWORLD_AP_BRIDGE_CONFIG")
	if ConfPath == "" {
		ConfPath = "/etc/concurrent/activitypub.yaml"
	}
	config.Load(ConfPath)

	log.Print("ConcurrentWorld Activitypub Bridge ", version, " starting...")
	log.Print("ApConfig loaded! Proxy: ", config.ApConfig.ProxyCCID)

	e.HidePort = true
	e.HideBanner = true

	if config.Server.EnableTrace {
		cleanup, err := setupTraceProvider(config.Server.TraceEndpoint, config.ApConfig.FQDN+"/ccapi", version)
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
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.Use(auth.ReceiveGatewayAuthPropagation)

	e.Binder = &apmiddleware.Binder{}

	db, err := gorm.Open(postgres.Open(config.Server.Dsn), &gorm.Config{})
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
	if err != nil {
		panic("failed to connect memcached")
	}
	defer mc.Close()

	// Migrate the schema
	log.Println("start migrate")
	db.AutoMigrate(
		&types.ApEntity{},
		&types.ApPerson{},
		&types.ApFollow{},
		&types.ApFollower{},
		&types.ApObjectReference{},
	)

	rdb := redis.NewClient(&redis.Options{
		Addr:     config.Server.RedisAddr,
		Password: "", // no password set
		DB:       0,  // use default DB
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
	client := client.NewClient()
	apService := ap.NewService(
		storeService,
		&client,
		config.NodeInfo,
		config.ApConfig,
	)

	apiService := api.NewService(storeService)
	apiHandler := api.NewHandler(apiService)

	apHandler := ap.NewHandler(apService)

	e.GET("/.well-known/webfinger", apHandler.WebFinger)
	e.GET("/.well-known/nodeinfo", apHandler.NodeInfoWellKnown)

	ap := e.Group("/ap")
	ap.GET("/nodeinfo/2.0", apHandler.NodeInfo)
	ap.GET("/acct/:id", apHandler.User)

	ap.GET("/api/entity/:ccid", apiHandler.GetEntityID)
	ap.POST("/api/entity", apiHandler.CreateEntity, auth.Restrict(auth.ISLOCAL)) // ISLOCAL

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

	e.Logger.Fatal(e.Start(":8001"))
}

func setupTraceProvider(endpoint string, serviceName string, serviceVersion string) (func(), error) {

	exporter, err := otlptracehttp.New(
		context.Background(),
		otlptracehttp.WithEndpoint(endpoint),
		otlptracehttp.WithInsecure(),
	)

	if err != nil {
		return nil, err
	}

	resource := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceNameKey.String(serviceName),
		semconv.ServiceVersionKey.String(serviceVersion),
	)

	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithResource(resource),
	)
	otel.SetTracerProvider(tracerProvider)

	propagator := propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
	otel.SetTextMapPropagator(propagator)

	cleanup := func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		if err := tracerProvider.Shutdown(ctx); err != nil {
			log.Printf("Failed to shutdown tracer provider: %v", err)
		}
	}
	return cleanup, nil
}
