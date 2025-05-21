package worker

import (
	"context"
	"encoding/json"
	"log"
	"regexp"
	"slices"
	"time"

	"github.com/concrnt/concrnt/client"
	"github.com/concrnt/concrnt/core"

	"github.com/concrnt/ccworld-ap-bridge/apclient"
	"github.com/concrnt/ccworld-ap-bridge/types"
	"github.com/concrnt/ccworld-ap-bridge/world"
)

type DeliverState struct {
	Listens []string
}

func (d DeliverState) Equals(other DeliverState) bool {
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

	ctx := context.Background()

	for ; true; <-ticker10.C {
		entities, err := w.store.GetAllEntities(ctx)
		if err != nil {
			log.Printf("worker/message GetAllEntities: %v", err)
			return
		}

		for _, entity := range entities {

			if !entity.Enabled {
				continue
			}

			mustRestart := false
			existingWorker, workerFound := workers[entity.ID]
			existingState, stateFound := states[entity.ID]
			if !workerFound || !stateFound {
				mustRestart = true
			}

			listenTimelines := make([]string, 0)
			userSettings, err := w.store.GetUserSettings(ctx, entity.CCID)
			if err == nil {
				listenTimelines = append(listenTimelines, userSettings.ListenTimelines...)
			}

			if len(listenTimelines) == 0 {
				listenTimelines = append(listenTimelines, world.UserHomeStream+"@"+entity.CCID)
			}

			timelines := make([]string, 0)
			for _, listenTimeline := range listenTimelines {
				timeline, err := w.client.GetTimeline(ctx, listenTimeline, &client.Options{Resolver: w.config.FQDN})
				if err != nil {
					log.Printf("worker/message/%v GetTimeline: %v", entity.ID, err)
					continue
				}

				timelines = append(timelines, timeline.ID+"@"+w.config.FQDN)
			}

			if len(timelines) == 0 {
				log.Printf("worker/message/%v no timelines to listen", entity.ID)
				continue
			}

			var newState DeliverState
			newState.Listens = timelines

			if !newState.Equals(existingState) {
				mustRestart = true
			}

			if !mustRestart {
				continue
			}

			if workerFound {
				existingWorker()
			}

			runctx, cancel := context.WithCancel(ctx)
			workers[entity.ID] = cancel
			states[entity.ID] = newState

			go func(ctx context.Context, entity types.ApEntity, timelines []string) {

				pubsub := w.rdb.Subscribe(ctx)
				err := pubsub.Subscribe(ctx, timelines...)
				if err != nil {
					log.Printf("worker/message/%v pubsub.Subscribe %v", entity.ID, err)
				}

				for {
					select {
					case <-ctx.Done():
						return
					default:
						pubsubMsg, err := pubsub.ReceiveMessage(ctx)
						if ctx.Err() != nil {
							delete(workers, entity.ID)
							delete(states, entity.ID)
							return
						}
						if err != nil {
							log.Printf("worker/message/%v pubsub.ReceiveMessage %v", entity.ID, err)
							delete(workers, entity.ID)
							delete(states, entity.ID)
							return
						}

						var streamEvent core.Event
						err = json.Unmarshal([]byte(pubsubMsg.Payload), &streamEvent)
						if err != nil {
							log.Printf("worker/message/%v json.Unmarshal streamEvent %v", entity.ID, err)
							continue
						}

						var document core.DocumentBase[any]
						err = json.Unmarshal([]byte(streamEvent.Document), &document)
						if err != nil {
							log.Printf("worker/message/%v json.Unmarshal document %v", entity.ID, err)
							continue
						}

						if document.Signer != entity.CCID {
							continue
						}

						var object *types.ApObject
						var content string

						switch document.Type {
						case "message":
							{
								messageID := streamEvent.Item.ResourceID
								note, err := w.bridge.MessageToNote(ctx, messageID)
								if err != nil {
									log.Printf("worker/message/%v MessageToNote %v", entity.ID, err)
									continue
								}

								content = note.Content

								if note.Type == "Announce" {
									announce := types.ApObject{
										Context: []string{"https://www.w3.org/ns/activitystreams"},
										Type:    "Announce",
										ID:      "https://" + w.config.FQDN + "/ap/note/" + messageID + "/activity",
										Actor:   "https://" + w.config.FQDN + "/ap/acct/" + entity.ID,
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
										Actor:   "https://" + w.config.FQDN + "/ap/acct/" + entity.ID,
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
									log.Printf("worker/message/%v json.Unmarshal deleteDoc %v", entity.ID, err)
									continue
								}

								if deleteDoc.Target[0] != 'm' {
									continue
								}

								deleteObj := types.ApObject{
									Context: "https://www.w3.org/ns/activitystreams",
									Type:    "Delete",
									ID:      "https://" + w.config.FQDN + "/ap/note/" + deleteDoc.Target + "/delete",
									Actor:   "https://" + w.config.FQDN + "/ap/acct/" + entity.ID,
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

							additionalInboxes := make([]string, 0)

							mentionPattern := regexp.MustCompile(`@(\S+@\S+)`)
							mentions := mentionPattern.FindAllStringSubmatch(content, -1)
							if mentions != nil {
								for _, mention := range mentions {
									if len(mention) != 2 {
										continue
									}

									actorID, err := apclient.ResolveActor(ctx, mention[1])
									if err != nil {
										log.Printf("worker/message/%v ResolveActor %v", entity.ID, err)
										continue
									}

									person, err := w.apclient.FetchPerson(ctx, actorID, &entity)
									if err != nil {
										log.Printf("worker/message/%v FetchPerson %v", entity.ID, err)
										continue
									}

									additionalInboxes = append(additionalInboxes, person.MustGetString("inbox"))

									obj, ok := object.Object.(types.ApObject)
									if !ok {
										log.Printf("worker/message/%v object.Object %v", entity.ID, err)
										continue
									}

									objTags, ok := obj.Tag.([]types.Tag)
									if !ok {
										log.Printf("worker/message/%v obj.Tag %v", entity.ID, err)
										continue
									}

									obj.Tag = append(objTags, types.Tag{
										Type: "Mention",
										Name: mention[1],
										Href: person.MustGetString("id"),
									})

									objCCs, ok := obj.CC.([]string)
									if !ok {
										log.Printf("worker/message/%v obj.CC %v", entity.ID, err)
										continue
									}

									obj.CC = append(objCCs, person.MustGetString("id"))

									object.Object = obj
								}
							}

							followers, err := w.store.GetFollowers(ctx, entity.ID)
							if err != nil {
								log.Printf("worker/message/%v GetFollowers %v", entity.ID, err)
								continue
							}

							destinations := make(map[string]bool)
							for _, timeline := range additionalInboxes {
								destinations[timeline] = true
							}
							for _, follower := range followers {
								destinations[follower.SubscriberInbox] = true
							}

							for destination := range destinations {
								go func(ctx context.Context, destination string, object types.ApObject) {
									err = w.apclient.PostToInbox(ctx, destination, object, entity)
									if err != nil {
										log.Printf("worker/message/%v PostToInbox %v %v", entity.ID, destination, err)
										return
									}
								}(ctx, destination, *object)
							}
						}
					}
				}

			}(runctx, entity, timelines)
		}
	}
}
