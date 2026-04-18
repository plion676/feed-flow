package app

import (
	"fmt"
	"log"

	"github.com/gin-gonic/gin"

	"github.com/plion676/feed-flow/internal/handler"
	"github.com/plion676/feed-flow/internal/middleware"
	"github.com/plion676/feed-flow/internal/repository"
	"github.com/plion676/feed-flow/internal/router"
	"github.com/plion676/feed-flow/internal/service"
)

// App is the composition root for the feed-flow HTTP service.
type App struct {
	Config *Config
	Engine *gin.Engine
}

// New builds the application with the shared middleware chain and routes.
func New(cfg *Config) *App {
	gin.SetMode(gin.DebugMode)
	if cfg.App.Env != "local" {
		gin.SetMode(gin.ReleaseMode)
	}

	engine := gin.New()
	engine.Use(
		middleware.RequestID(),
		middleware.Logger(),
		middleware.Recovery(),
	)
	db, err := NewMySQLDB(cfg)
	if err != nil {
		log.Fatalf("connect mysql failed: %v", err)
	}
	userRepo := repository.NewUserRepository(db)
	userCountRepo := repository.NewUserCountRepository(db)
	authService := service.NewAuthService(db, userRepo, userCountRepo)
	authHandler := handler.NewAuthHandler(authService)

	router.RegisterRoutes(engine, authHandler)

	return &App{
		Config: cfg,
		Engine: engine,
	}
}

// Run starts the HTTP server.
func (a *App) Run() error {
	return a.Engine.Run(fmt.Sprintf(":%d", a.Config.App.Port))
}
