package worker

import (
	"github.com/concrnt/ccworld-ap-bridge/ap"
	"github.com/concrnt/ccworld-ap-bridge/apclient"
	"github.com/concrnt/ccworld-ap-bridge/store"
	"github.com/concrnt/ccworld-ap-bridge/types"
	"github.com/redis/go-redis/v9"
	"github.com/totegamma/concurrent/client"
)

type Worker struct {
	rdb       *redis.Client
	store     *store.Store
	client    client.Client
	apservice *ap.Service
	apclient  *apclient.ApClient
	config    types.ApConfig
}

func NewWorker(rdb *redis.Client, store *store.Store, client client.Client, apservice *ap.Service, apclient *apclient.ApClient, config types.ApConfig) *Worker {
	return &Worker{
		rdb,
		store,
		client,
		apservice,
		apclient,
		config,
	}
}

func (w *Worker) Run() {
	go w.StartMessageWorker()
	go w.StartAssociationWorker()
}
