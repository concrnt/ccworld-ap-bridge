package worker

import (
	"context"
	"encoding/json"
	"log"
	"slices"
	"time"

	"github.com/totegamma/concurrent/core"

	"github.com/concrnt/ccworld-ap-bridge/types"
	"github.com/concrnt/ccworld-ap-bridge/world"
)

func (w *Worker) StartMessageWorker() {

	log.Printf("start message worker")

	ticker10 := time.NewTicker(10 * time.Second)
	workers := make(map[string]context.CancelFunc)

	for {
		<-ticker10.C
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		jobs, err := w.store.GetAllFollowers(ctx)
		if err != nil {
			log.Printf("error: %v", err)
		}

		for _, job := range jobs {
			if _, ok := workers[job.ID]; !ok {
				log.Printf("start worker %v\n", job.ID)
				ctx, cancel := context.WithCancel(context.Background())
				workers[job.ID] = cancel

				entity, err := w.store.GetEntityByID(ctx, job.PublisherUserID)
				if err != nil {
					log.Printf("error: %v", err)
				}
				ownerID := entity.CCID
				home := world.UserHomeStream + "@" + ownerID

				timeline, err := w.client.GetTimeline(ctx, w.config.FQDN, home)
				if err != nil {
					log.Printf("error: %v", err)
					continue
				}

				normalized := timeline.ID + "@" + w.config.FQDN

				pubsub := w.rdb.Subscribe(ctx)
				pubsub.Subscribe(ctx, normalized)

				log.Printf("subscribed to %v(%v)\n", normalized, home)

				go func(ctx context.Context, job types.ApFollower) {
					for {
						select {
						case <-ctx.Done():
							log.Printf("worker %v done", job.ID)
							return
						default:
							pubsubMsg, err := pubsub.ReceiveMessage(ctx)
							if ctx.Err() != nil {
								continue
							}
							if err != nil {
								log.Printf("error: %v", err)
								continue
							}

							var streamEvent core.Event
							err = json.Unmarshal([]byte(pubsubMsg.Payload), &streamEvent)
							if err != nil {
								log.Printf("error: %v", err)
								continue
							}

							var document core.DocumentBase[any]
							err = json.Unmarshal([]byte(streamEvent.Document), &document)
							if err != nil {
								log.Printf("error: %v", err)
								continue
							}

							/*
							   str, err := json.Marshal(streamEvent.Resource)
							   if err != nil {
							       log.Printf(errors.Wrap(err, "failed to marshal resource").Error())
							       continue
							   }
							   var message core.Message
							   err = json.Unmarshal(str, &message)
							   if err != nil {
							       log.Printf("error: %v", err)
							       continue
							   }
							*/

							switch document.Type {
							case "message":
								{
									messageID := streamEvent.Item.ResourceID
									messageAuthor := streamEvent.Item.Owner
									if streamEvent.Item.Author != nil {
										messageAuthor = *streamEvent.Item.Author
									}

									if messageAuthor != ownerID {
										log.Printf("message author is not owner: %v", messageAuthor)
										continue
									}

									note, err := w.apservice.MessageToNote(ctx, messageID)
									if err != nil {
										log.Printf("error while converting message to note: %v", err)
										continue
									}

									if note.Type == "Announce" {
										announce := types.ApObject{
											Context: []string{"https://www.w3.org/ns/activitystreams"},
											Type:    "Announce",
											ID:      "https://" + w.config.FQDN + "/ap/note/" + messageID + "/activity",
											Actor:   "https://" + w.config.FQDN + "/ap/acct/" + job.PublisherUserID,
											Content: "",
											Object:  note.Object,
											To:      []string{"https://www.w3.org/ns/activitystreams#Public"},
										}

										err = w.apclient.PostToInbox(ctx, job.SubscriberInbox, announce, entity)
										if err != nil {
											log.Printf("error: %v", err)
											continue
										}
										log.Printf("[worker %v] created", job.ID)
									} else {

										create := types.ApObject{
											Context: []string{"https://www.w3.org/ns/activitystreams"},
											Type:    "Create",
											ID:      "https://" + w.config.FQDN + "/ap/note/" + messageID + "/activity",
											Actor:   "https://" + w.config.FQDN + "/ap/acct/" + job.PublisherUserID,
											To:      []string{"https://www.w3.org/ns/activitystreams#Public"},
											Object:  note,
										}

										err = w.apclient.PostToInbox(ctx, job.SubscriberInbox, create, entity)
										if err != nil {
											log.Printf("error: %v", err)
											continue
										}
										log.Printf("[worker %v] created", job.ID)
									}
								}
							case "delete":
								{

									var deleteDoc core.DeleteDocument
									err = json.Unmarshal([]byte(streamEvent.Document), &deleteDoc)
									if err != nil {
										log.Printf("error: %v", err)
										continue
									}

									deleteObj := types.ApObject{
										Context: "https://www.w3.org/ns/activitystreams",
										Type:    "Delete",
										ID:      "https://" + w.config.FQDN + "/ap/note/" + deleteDoc.Target + "/delete",
										Actor:   "https://" + w.config.FQDN + "/ap/acct/" + job.PublisherUserID,
										Object: types.ApObject{
											Type: "Tombstone",
											ID:   "https://" + w.config.FQDN + "/ap/note/" + deleteDoc.Target,
										},
									}

									err = w.apclient.PostToInbox(ctx, job.SubscriberInbox, deleteObj, entity)
									if err != nil {
										log.Printf("error: %v", err)
										continue
									}
								}
							default:
								{
									log.Printf("unknown document type: %v", document.Type)
									continue
								}
							}
						}
					}
				}(ctx, job)
			}
		}

		// create job id list
		var jobIDs []string
		for _, job := range jobs {
			jobIDs = append(jobIDs, job.ID)
		}

		for routineID, cancel := range workers {
			if !slices.Contains(jobIDs, routineID) {
				log.Printf("cancel worker %v", routineID)
				cancel()
				delete(workers, routineID)
			}
		}
	}
}
