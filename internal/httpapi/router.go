package httpapi

import (
	"net/http"
	"time"

	"github.com/syukronhidayat/nearby-drivers/internal/config"
	"github.com/syukronhidayat/nearby-drivers/internal/httpapi/handlers"
	"github.com/syukronhidayat/nearby-drivers/internal/store/redisstore"
	"github.com/syukronhidayat/nearby-drivers/internal/throttle"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type Deps struct {
	DriverStore *redisstore.Store
	Throttler   throttle.Throttler
}

func NewRouter(cfg config.Config, deps Deps) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)

	r.Use(middleware.Logger)

	r.Get(cfg.HealthzPath, handlers.Healthz)

	dh := &handlers.DriversHandler{
		Store:      deps.DriverStore,
		Throttler:  deps.Throttler,
		MetaTTL:    10 * time.Minute,
		FutureSkew: 60 * time.Second,
		MaxPast:    24 * time.Hour,
	}

	r.Route("/v1", func(r chi.Router) {
		r.Post("/drivers/location", dh.PostDriverLocation)
		r.Get("/drivers/nearby", dh.GetDriversNearby)
	})

	return r
}
