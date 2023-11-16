package metrics

import (
	"net/http"
	"orchestration/infra/utils/logger"
	"time"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const prometheusPost = ":9000"

func init() {
	go func() {
		router := mux.NewRouter()
		router.Path("/metrics").Handler(promhttp.Handler())
		srv := http.Server{
			ReadHeaderTimeout: time.Second,
			ReadTimeout:       time.Second,
			IdleTimeout:       2 * time.Minute,
			Addr:              prometheusPost,
			Handler:           router,
		}
		srv.SetKeepAlivesEnabled(true)
		err := srv.ListenAndServe()
		if err != nil {
			logger.Fatalf(err.Error())
		}
	}()
}
