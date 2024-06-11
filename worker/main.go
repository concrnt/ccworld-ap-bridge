package worker

import (
	"github.com/concrnt/ccworld-ap-bridge/apclient"
	"github.com/concrnt/ccworld-ap-bridge/bridge"
	"github.com/concrnt/ccworld-ap-bridge/store"
	"github.com/concrnt/ccworld-ap-bridge/types"
	"github.com/redis/go-redis/v9"
	"github.com/totegamma/concurrent/client"
)

type Worker struct {
	rdb      *redis.Client
	store    *store.Store
	client   client.Client
	apclient *apclient.ApClient
	bridge   *bridge.Service
	config   types.ApConfig
}

func NewWorker(
	rdb *redis.Client,
	store *store.Store,
	client client.Client,
	apclient *apclient.ApClient,
	bridge *bridge.Service,
	config types.ApConfig,
) *Worker {
	return &Worker{
		rdb,
		store,
		client,
		apclient,
		bridge,
		config,
	}
}

func (w *Worker) Run() {
	go w.StartMessageWorker()
	go w.StartAssociationWorker()
}
