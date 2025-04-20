package worker

import (
	"context"
	"encoding/json"
	"log"
	"slices"
	"time"

	"github.com/totegamma/concurrent/client"
	"github.com/totegamma/concurrent/core"

	"github.com/concrnt/ccworld-ap-bridge/types"
	"github.com/concrnt/ccworld-ap-bridge/world"
)

type DeliverState struct {
	Dests   []string
	Listens []string
}

func (d DeliverState) Equals(other DeliverState) bool {
	if len(d.Dests) != len(other.Dests) {
		return false
	}

	for _, dest := range d.Dests {
		if !slices.Contains(other.Dests, dest) {
			return false
		}
	}

	if len(d.Listens) != len(other.Listens) {
		return false
	}

	for _, listen := range d.Listens {
		if !slices.Contains(other.Listens, listen) {
			return false
		}
	}

	return true
}

func (w *Worker) StartMessageWorker() {

	log.Printf("start message worker")

	ticker10 := time.NewTicker(10 * time.Second)
	workers := make(map[string]context.CancelFunc)
	states := make(map[string]DeliverState)

	for ; true; <-ticker10.C {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		followers, err := w.store.GetAllFollowers(ctx)
		if err != nil {
			log.Printf("worker/message GetAllFollowers: %v", err)
		}

		delivers := make(map[string][]types.ApFollower)
		for _, job := range followers {
			if _, ok := delivers[job.PublisherUserID]; !ok {
				delivers[job.PublisherUserID] = make([]types.ApFollower, 0)
			}
			delivers[job.PublisherUserID] = append(delivers[job.PublisherUserID], job)
		}

		for userID, deliver := range delivers {
			mustRestart := false
			existingWorker, workerFound := workers[userID]
			existingState, stateFound := states[userID]
			if !workerFound || !stateFound {
				mustRestart = true
			}

			entity, err := w.store.GetEntityByID(ctx, userID)
			if err != nil {
				log.Printf("worker/message/%v GetEntityByID: %v", userID, err)
				continue
			}

			if !entity.Enabled {
				if workerFound {
					log.Printf("worker/message/%v cancel worker\n", userID)
					existingWorker()
				}
				continue
			}

			ownerID := entity.CCID

			var newState DeliverState
			listenTimelines := make([]string, 0)
			userSettings, err := w.store.GetUserSettings(ctx, entity.CCID)
			if err == nil {
				listenTimelines = append(listenTimelines, userSettings.ListenTimelines...)
			}

			if len(listenTimelines) == 0 {
				listenTimelines = append(listenTimelines, world.UserHomeStream+"@"+ownerID)
			}

			newState.Dests = make([]string, 0)
			newState.Listens = listenTimelines
			for _, job := range deliver {
				newState.Dests = append(newState.Dests, job.SubscriberInbox)
			}

			if !newState.Equals(existingState) {
				mustRestart = true
			}

			if mustRestart {
				if workerFound {
					log.Printf("worker/message/%v cancel worker\n", userID)
					existingWorker()
				}

				log.Printf("worker/message/%v start worker \n", userID)

				timelines := make([]string, 0)
				for _, listenTimeline := range newState.Listens {
					timeline, err := w.client.GetTimeline(ctx, listenTimeline, &client.Options{Resolver: w.config.FQDN})
					if err != nil {
						log.Printf("worker/message/%v GetTimeline: %v", userID, err)
						continue
					}

					timelines = append(timelines, timeline.ID+"@"+w.config.FQDN)
				}

				if len(timelines) == 0 {
					log.Printf("worker/message/%v no timelines to listen", userID)
					continue
				}

				pubsub := w.rdb.Subscribe(ctx)
				err := pubsub.Subscribe(ctx, timelines...)
				if err != nil {
					log.Printf("worker/message/%v pubsub.Subscribe %v", userID, err)
					continue
				}

				workerctx, cancel := context.WithCancel(context.Background())
				workers[userID] = cancel
				states[userID] = newState

				go func(ctx context.Context, publisherUserID string, subscriberInboxes []string) {
					for {
						select {
						case <-ctx.Done():
							return
						default:
							pubsubMsg, err := pubsub.ReceiveMessage(ctx)
							if ctx.Err() != nil {
								delete(workers, publisherUserID)
								delete(states, publisherUserID)
								return
							}
							if err != nil {
								log.Printf("worker/message/%v pubsub.ReceiveMessage %v", publisherUserID, err)
								delete(workers, publisherUserID)
								delete(states, publisherUserID)
								return
							}

							var streamEvent core.Event
							err = json.Unmarshal([]byte(pubsubMsg.Payload), &streamEvent)
							if err != nil {
								log.Printf("worker/message/%v json.Unmarshal streamEvent %v", publisherUserID, err)
								continue
							}

							var document core.DocumentBase[any]
							err = json.Unmarshal([]byte(streamEvent.Document), &document)
							if err != nil {
								log.Printf("worker/message/%v json.Unmarshal document %v", publisherUserID, err)
								continue
							}

							if document.Signer != ownerID {
								continue
							}

							var object *types.ApObject

							switch document.Type {
							case "message":
								{
									messageID := streamEvent.Item.ResourceID
									note, err := w.bridge.MessageToNote(ctx, messageID)
									if err != nil {
										log.Printf("worker/message/%v MessageToNote %v", publisherUserID, err)
										continue
									}

									if note.Type == "Announce" {
										announce := types.ApObject{
											Context: []string{"https://www.w3.org/ns/activitystreams"},
											Type:    "Announce",
											ID:      "https://" + w.config.FQDN + "/ap/note/" + messageID + "/activity",
											Actor:   "https://" + w.config.FQDN + "/ap/acct/" + publisherUserID,
											Content: "",
											Object:  note.Object,
											To:      []string{"https://www.w3.org/ns/activitystreams#Public"},
										}
										object = &announce
									} else {
										create := types.ApObject{
											Context: []string{"https://www.w3.org/ns/activitystreams"},
											Type:    "Create",
											ID:      "https://" + w.config.FQDN + "/ap/note/" + messageID + "/activity",
											Actor:   "https://" + w.config.FQDN + "/ap/acct/" + publisherUserID,
											To:      []string{"https://www.w3.org/ns/activitystreams#Public"},
											Object:  note,
										}
										object = &create
									}
								}
							case "delete":
								{
									var deleteDoc core.DeleteDocument
									err = json.Unmarshal([]byte(streamEvent.Document), &deleteDoc)
									if err != nil {
										log.Printf("worker/message/%v json.Unmarshal deleteDoc %v", publisherUserID, err)
										continue
									}

									if deleteDoc.Target[0] != 'm' {
										continue
									}

									deleteObj := types.ApObject{
										Context: "https://www.w3.org/ns/activitystreams",
										Type:    "Delete",
										ID:      "https://" + w.config.FQDN + "/ap/note/" + deleteDoc.Target + "/delete",
										Actor:   "https://" + w.config.FQDN + "/ap/acct/" + publisherUserID,
										Object: types.ApObject{
											Type: "Tombstone",
											ID:   "https://" + w.config.FQDN + "/ap/note/" + deleteDoc.Target,
										},
									}
									object = &deleteObj
								}
							default:
								{
									continue
								}
							}

							if object != nil {
								for _, subscriberInbox := range subscriberInboxes {
									err = w.apclient.PostToInbox(ctx, subscriberInbox, *object, entity)
									if err != nil {
										log.Printf("worker/message/%v PostToInbox %v %v", publisherUserID, subscriberInbox, err)
										continue
									}
								}
							}

						}
					}
				}(workerctx, userID, newState.Dests)
			}
		}

		// create job id list
		var validUsers []string
		for userID := range workers {
			validUsers = append(validUsers, userID)
		}

		for routineID, cancel := range workers {
			if !slices.Contains(validUsers, routineID) {
				log.Printf("worker/message/%v cancel worker\n", routineID)
				cancel()
				delete(workers, routineID)
			}
		}
	}
}
