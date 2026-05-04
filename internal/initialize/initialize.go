package initialize

import (
	"fmt"
	"os"

	c "github.com/dinhdev-nu/chat-platform-api/config"
	g "github.com/dinhdev-nu/chat-platform-api/global"
	m "github.com/dinhdev-nu/chat-platform-api/internal/midleware"
	"github.com/gin-gonic/gin"
)

type Application struct {
	Config *c.Config
	Router *gin.Engine
}

func NewApp() *Application {
	env := os.Getenv("APP_ENV")
	if env == "" {
		env = "local"
	}

	// Load configuration
	cfg := LoadConfig()
	g.Config = cfg

	// Initialize
	InitLogger()
	r := InitRouter()

	// Add middleware
	r.Use(
		gin.Recovery(),
		m.Logger(),
	)

	return &Application{
		Config: cfg,
		Router: r,
	}

}

func (app *Application) Run() {
	addr := fmt.Sprintf("%s:%d", app.Config.Server.Host, app.Config.Server.Port)

	fmt.Printf("Starting server on %s in %s mode...\n", addr, app.Config.Server.Mode)

	if err := app.Router.Run(addr); err != nil {
		panic(fmt.Errorf("failed to start server: %w", err))
	}
}
