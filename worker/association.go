package worker

import (
	"context"
	"encoding/json"
	"log"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"

	"github.com/totegamma/concurrent/client"
	"github.com/totegamma/concurrent/core"
	"github.com/totegamma/concurrent/x/jwt"

	"github.com/concrnt/ccworld-ap-bridge/types"
	"github.com/concrnt/ccworld-ap-bridge/world"
)

func createToken(domain, ccid, priv string) (string, error) {
	token, err := jwt.Create(jwt.Claims{
		JWTID:          uuid.New().String(),
		IssuedAt:       strconv.FormatInt(time.Now().Unix(), 10),
		ExpirationTime: strconv.FormatInt(time.Now().Add(5*time.Minute).Unix(), 10),
		Audience:       domain,
		Issuer:         ccid,
		Subject:        "concrnt",
	}, priv)
	return token, err
}

func (w *Worker) StartAssociationWorker() {

	ctx := context.Background()
	notificationStream := world.UserNotifyStream + "@" + w.config.ProxyCCID
	timeline, err := w.client.GetTimeline(ctx, w.config.FQDN, notificationStream, nil)
	if err != nil {
		log.Printf("worker/association GetTimeline: %v", err)
		return
	}

	normalized := timeline.ID + "@" + w.config.FQDN

	pubsub := w.rdb.Subscribe(ctx)
	err = pubsub.Subscribe(ctx, normalized)
	if err != nil {
		log.Printf("worker/association Subscribe: %v", err)
		panic(err)
	}

	for {
		pubsubMsg, err := pubsub.ReceiveMessage(ctx)
		if err != nil {
			log.Printf("worker/association ReceiveMessage: %v", err)
			continue
		}

		var streamEvent core.Event
		err = json.Unmarshal([]byte(pubsubMsg.Payload), &streamEvent)
		if err != nil {
			log.Printf("worker/association unmarshal streamEvent: %v", err)
			continue
		}

		var document core.DocumentBase[any]
		err = json.Unmarshal([]byte(streamEvent.Document), &document)
		if err != nil {
			log.Printf("worker/association unmarshal document: %v", err)
			continue
		}

		// FIXME: fix this marshall -> unmarshall
		str, err := json.Marshal(streamEvent.Resource)
		if err != nil {
			log.Printf(errors.Wrap(err, "failed to marshal resource").Error())
			continue
		}
		var association core.Association
		err = json.Unmarshal(str, &association)
		if err != nil {
			log.Printf(errors.Wrap(err, "failed to unmarshal association").Error())
			continue
		}

		switch document.Type {
		case "association":
			{
				if association.Target[0] != 'm' { // assert association target is message
					continue
				}

				assauthor, err := w.store.GetEntityByCCID(ctx, association.Author) // TODO: handle remote
				if err != nil {
					log.Printf("worker/association GetEntityByCCID: %v", err)
					continue
				}

				token, err := createToken(w.config.FQDN, w.config.ProxyCCID, w.config.ProxyPriv)
				if err != nil {
					log.Printf("worker/association createToken %v", err)
					continue
				}

				msg, err := w.client.GetMessage(ctx, w.config.FQDN, association.Target, &client.Options{
					AuthToken: token,
				})
				if err != nil {
					log.Printf("worker/association GetMessage: %v", err)
					continue
				}

				var messageDoc core.MessageDocument[world.MarkdownMessage]
				err = json.Unmarshal([]byte(msg.Document), &messageDoc)
				if err != nil {
					log.Printf("worker/association unmarshal messageDoc: %v", err)
					continue
				}

				msgMeta, ok := messageDoc.Meta.(map[string]interface{})
				ref, ok := msgMeta["apObjectRef"].(string)
				if !ok {
					log.Printf("worker/association target Message is not activitypub message")
					continue
				}
				dest, ok := msgMeta["apPublisherInbox"].(string)
				if !ok {
					log.Printf("worker/association target Message is not activitypub message")
					continue
				}

				switch association.Schema {
				case world.LikeAssociationSchema:
					like := types.ApObject{
						Context: []string{"https://www.w3.org/ns/activitystreams"},
						Type:    "Like",
						ID:      "https://" + w.config.FQDN + "/ap/likes/" + association.ID,
						Actor:   "https://" + w.config.FQDN + "/ap/acct/" + assauthor.ID,
						Content: "⭐",
						Object:  ref,
					}

					err = w.apclient.PostToInbox(ctx, dest, like, assauthor)
					if err != nil {
						log.Printf("worker/association/like PostToInbox: %v", err)
						continue
					}
					break
				case world.ReactionAssociationSchema:
					var reactionDoc core.AssociationDocument[world.ReactionAssociation]
					err = json.Unmarshal([]byte(association.Document), &reactionDoc)
					if err != nil {
						log.Printf("worker/association/reaction unmarshal reactionDoc: %v", err)
						continue
					}

					shortcode := ":" + reactionDoc.Body.Shortcode + ":"
					tag := []types.Tag{
						{
							Type: "Emoji",
							ID:   reactionDoc.Body.ImageURL,
							Name: ":" + reactionDoc.Body.Shortcode + ":",
							Icon: types.Icon{
								Type:      "Image",
								MediaType: "image/png",
								URL:       reactionDoc.Body.ImageURL,
							},
						},
					}

					like := types.ApObject{
						Context: []string{"https://www.w3.org/ns/activitystreams"},
						Type:    "Like",
						ID:      "https://" + w.config.FQDN + "/ap/likes/" + association.ID,
						Actor:   "https://" + w.config.FQDN + "/ap/acct/" + assauthor.ID,
						Content: shortcode,
						Tag:     tag,
						Object:  ref,
					}

					err = w.apclient.PostToInbox(ctx, dest, like, assauthor)
					if err != nil {
						log.Printf("worker/association/reaction PostToInbox: %v", err)
						continue
					}
					/*
						case world.ReplyAssociationSchema:
							var replyDoc core.AssociationDocument[world.ReplyAssociation]
							err = json.Unmarshal([]byte(association.Document), &replyDoc)
							if err != nil {
								log.Printf("worker/association/reply unmarshal replyDoc: %v", err)
								continue
							}

							token, err := createToken(w.config.FQDN, w.config.ProxyCCID, w.config.ProxyPriv)
							if err != nil {
								log.Printf("worker/association/reply createToken %v", err)
								continue
							}

							reply, err := w.client.GetMessage(ctx, w.config.FQDN, replyDoc.Body.MessageID, &client.Options{
								AuthToken: token,
							}) // TODO: handle remote
							if err != nil {
								log.Printf("worker/association/reply GetMessage: %v", err)
								continue
							}

							var replyMessage core.MessageDocument[world.ReplyMessage]
							err = json.Unmarshal([]byte(reply.Document), &replyMessage)
							if err != nil {
								log.Printf("worker/association/reply unmarshal replyMessage: %v", err)
								continue
							}

							create := types.ApObject{
								Context: []string{"https://www.w3.org/ns/activitystreams"},
								Type:    "Create",
								ID:      "https://" + w.config.FQDN + "/ap/note/" + replyDoc.Body.MessageID + "/activity",
								Actor:   "https://" + w.config.FQDN + "/ap/acct/" + assauthor.ID,
								Object: types.ApObject{
									Type:         "Note",
									ID:           "https://" + w.config.FQDN + "/ap/note/" + replyDoc.Body.MessageID,
									AttributedTo: "https://" + w.config.FQDN + "/ap/acct/" + assauthor.ID,
									Content:      replyMessage.Body.Body,
									InReplyTo:    ref,
									To:           []string{"https://www.w3.org/ns/activitystreams#Public"},
								},
							}

							err = w.apclient.PostToInbox(ctx, dest, create, assauthor)
							if err != nil {
								log.Printf("worker/association/reply PostToInbox: %v", err)
								continue
							}
					*/

					/*
						case world.RerouteAssociationSchema:
							var rerouteDoc core.AssociationDocument[world.RerouteAssociation]
							err = json.Unmarshal([]byte(association.Document), &rerouteDoc)
							if err != nil {
								log.Printf("worker/association/reroute unmarshal rerouteDoc: %v", err)
								continue
							}

							announce := types.ApObject{
								Context: []string{"https://www.w3.org/ns/activitystreams"},
								Type:    "Announce",
								ID:      "https://" + w.config.FQDN + "/ap/note/" + rerouteDoc.Body.MessageID,
								Actor:   "https://" + w.config.FQDN + "/ap/acct/" + assauthor.ID,
								Content: "",
								Object:  ref,
								To:      []string{"https://www.w3.org/ns/activitystreams#Public"},
							}
							err = w.apclient.PostToInbox(ctx, dest, announce, assauthor)
							if err != nil {
								log.Printf("worker/association/reroute PostToInbox: %v", err)
								continue
							}
					*/
				}
			}

		case "delete":
			{
				entity, err := w.store.GetEntityByCCID(ctx, association.Author)
				if err != nil {
					log.Printf("worker/association/delete GetEntityByCCID: %v", err)
					continue
				}

				token, err := createToken(w.config.FQDN, w.config.ProxyCCID, w.config.ProxyPriv)
				if err != nil {
					log.Printf("worker/association/delete createToken %v", err)
					continue
				}

				target, err := w.client.GetMessage(ctx, w.config.FQDN, association.Target, &client.Options{
					AuthToken: token,
				})
				if err != nil {
					log.Printf("worker/association/delete GetMessage: %v", err)
					continue
				}

				var messageDoc core.MessageDocument[world.MarkdownMessage]
				err = json.Unmarshal([]byte(target.Document), &messageDoc)
				if err != nil {
					log.Printf("worker/association/delete unmarshal messageDoc: %v", err)
					continue
				}

				messageMeta, ok := messageDoc.Meta.(map[string]any)
				if !ok {
					log.Printf("worker/association/delete target Message is not activitypub message")
					continue
				}

				ref, ok := messageMeta["apObjectRef"].(string)
				if !ok {
					log.Printf("worker/association/delete target Message is not activitypub message")
					continue
				}

				inbox, ok := messageMeta["apPublisherInbox"].(string)
				if !ok {
					log.Printf("worker/association/delete target Message is not activitypub message")
					continue
				}

				undo := types.ApObject{
					Context: "https://www.w3.org/ns/activitystreams",
					Type:    "Undo",
					Actor:   "https://" + w.config.FQDN + "/ap/acct/" + entity.ID,
					ID:      "https://" + w.config.FQDN + "/ap/likes/" + association.Target + "/undo",
					Object: types.ApObject{
						Context: "https://www.w3.org/ns/activitystreams",
						Type:    "Like",
						ID:      "https://" + w.config.FQDN + "/ap/likes/" + association.Target,
						Actor:   "https://" + w.config.FQDN + "/ap/acct/" + entity.ID,
						Object:  ref,
					},
				}

				err = w.apclient.PostToInbox(ctx, inbox, undo, entity)
				if err != nil {
					log.Printf("worker/association/delete PostToInbox: %v", err)
					continue
				}

			}
		default:
			{
				log.Printf("worker/association unknown document type %v", document.Type)
			}
		}
	}
}
