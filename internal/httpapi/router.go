package httpapi

import (
	"net/http"

	"github.com/syukronhidayat/nearby-drivers/internal/config"
	"github.com/syukronhidayat/nearby-drivers/internal/httpapi/handlers"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func NewRouter(cfg config.Config) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)

	r.Use(middleware.Logger)

	r.Get(cfg.HealthzPath, handlers.Healthz)

	return r
}
