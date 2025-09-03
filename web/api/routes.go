// Package api provides the HTTP API for the lotus curio web gui.
package api

import (
	"context"

	"github.com/gorilla/mux"

	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/web/api/config"
	"github.com/filecoin-project/curio/web/api/sector"
	"github.com/filecoin-project/curio/web/api/webrpc"
	"github.com/filecoin-project/go-jsonrpc/auth"
)

func Routes(
	r *mux.Router,
	deps *deps.Deps,
	authVerify func(ctx context.Context, token string) ([]auth.Permission, error),
	debug bool) {

	r.Use(authMiddleware(authVerify, deps.Cfg.Apis.EnableWebAuth))

	webrpc.Routes(r.PathPrefix("/webrpc").Subrouter(), deps, debug)
	config.Routes(r.PathPrefix("/config").Subrouter(), deps)
	sector.Routes(r.PathPrefix("/sector").Subrouter(), deps)
}
