package isureal

import (
	"log"

	"github.com/labstack/echo/v5"
	"github.com/newrelic/go-agent/v3/newrelic"
)

var nrApp *newrelic.Application

// 設定は環境変数（/home/isucon/newrelic.env）から読む。キーが無い・不正なときは APM 無効で続行する
func initNewRelic() {
	var err error
	nrApp, err = newrelic.NewApplication(
		newrelic.ConfigFromEnvironment(),
		func(cfg *newrelic.Config) {
			if cfg.License == "" {
				cfg.Enabled = false
			}
		},
	)
	if err != nil {
		log.Printf("NewRelic disabled: %s", err.Error())
		nrApp, _ = newrelic.NewApplication(newrelic.ConfigEnabled(false))
	}
}

// ResponseWriter は包まない（レスポンスヘッダと SSE の Flush を変えないため）。ステータスだけ記録する
func NewRelicMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			route := c.RouteInfo()
			txn := nrApp.StartTransaction(route.Method + " " + route.Path)
			defer txn.End()
			txn.SetWebRequestHTTP(c.Request())
			c.SetRequest(newrelic.RequestWithTransactionContext(c.Request(), txn))

			err := next(c)
			_, status := echo.ResolveResponseStatus(c.Response(), err)
			if txn != nil {
				txn.SetWebResponse(nil).WriteHeader(status)
			}
			return err
		}
	}
}
