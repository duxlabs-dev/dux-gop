package main

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/duxlabs-dev/dux-go/app"
	"github.com/duxlabs-dev/dux-go/errdefs/humaerr"
	"github.com/duxlabs-dev/dux-go/httpapi"
	"github.com/duxlabs-dev/dux-go/httpapi/middleware"
	"github.com/duxlabs-dev/dux-go/log"
	gops_handler "github.com/duxlabs-dev/dux-gop/api/gops"
	"github.com/duxlabs-dev/dux-gop/api/server"
	"github.com/duxlabs-dev/dux-gop/config"
	"github.com/duxlabs-dev/dux-gop/gops"
	"github.com/go-chi/chi/v5"
	"github.com/spf13/cobra"
)

const apiTitle = "Dux GOP API"

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Start the API server",
	Long:  "Start the REST API server to provide system metrics endpoints.",
	RunE:  runServerCommand,
}

func runServerCommand(cmd *cobra.Command, args []string) error {
	cfg := config.NewConfig()
	return startAPI(cmd.Context(), cfg)
}

func startAPI(ctx context.Context, cfg *config.Config) error {
	gopsUtil := gops.NewGopsUtil()
	defer gopsUtil.Close()
	srvImpl := &server.Server{
		Cfg:  cfg,
		Gops: gopsUtil,
	}

	r := chi.NewRouter()

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	r.Group(func(r chi.Router) {
		r.Use(middleware.Logger)

		huma.NewError = humaerr.HumaErrorFunc

		humaCfg := httpapi.NewHumaConfig(apiTitle, "1.0.0", httpapi.WithURLEncodedForms())
		humaCfg.DocsPath = ""
		api := humachi.New(r, humaCfg)

		mw := middleware.NewMiddleware(api)
		api.UseMiddleware(mw.Recoverer)

		r.Get("/docs", httpapi.DocsHandler(apiTitle))

		gopsGroup := huma.NewGroup(api, "/gops")
		gopsGroup.UseModifier(func(op *huma.Operation, next func(*huma.Operation)) {
			op.Tags = []string{"Gops"}
			next(op)
		})
		gops_handler.RegisterHandlers(srvImpl, gopsGroup)
	})

	addr := cfg.ApiPort
	log.Infof(" Starting Dux GOP API server on %s", addr)
	log.Infof(" API Documentation: http://localhost%s/docs", addr)
	log.Infof(" OpenAPI Spec: http://localhost%s/openapi.json", addr)
	log.Infof(" Health Check: http://localhost%s/health", addr)

	return app.Serve(ctx, httpapi.NewServer(addr, r))
}
