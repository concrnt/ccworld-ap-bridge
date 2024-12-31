package worker

import (
	"context"
	"encoding/json"
	"log"
	"slices"
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
	pubsub := w.rdb.Subscribe(ctx)

	var lastEntities []string
	tlDict := make(map[string]string)

	go func() {
		for {
			entities, err := w.store.GetAllEntities(ctx)
			if err != nil {
				time.Sleep(10 * time.Second)
				log.Printf("worker/association GetAllEntities: %v", err)
				continue
			}

			if len(entities) == 0 {
				time.Sleep(10 * time.Second)
				continue
			}

			if len(lastEntities) == len(entities) {
				time.Sleep(10 * time.Second)
				continue
			}

			same := true
			for _, entity := range entities {
				if !slices.Contains(lastEntities, entity.CCID) {
					same = false
					break
				}
			}

			if same {
				time.Sleep(10 * time.Second)
				continue
			}

			var listeners []string
			for _, entity := range entities {

				if _, ok := tlDict[entity.CCID]; ok {
					listeners = append(listeners, tlDict[entity.CCID])
				} else {
					associationStream := world.UserAssocStream + "@" + entity.CCID
					log.Printf("worker/association lookup %v", associationStream)
					timeline, err := w.client.GetTimeline(ctx, w.config.FQDN, associationStream, nil)
					if err != nil {
						log.Printf("worker/association GetTimeline: %v", err)
						continue
					}

					normalized := timeline.ID + "@" + w.config.FQDN

					listeners = append(listeners, normalized)
					tlDict[entity.CCID] = normalized
				}
				lastEntities = append(lastEntities, entity.CCID)
			}

			err = pubsub.Subscribe(ctx, listeners...)
			if err != nil {
				log.Printf("worker/association Subscribe: %v", err)
				time.Sleep(10 * time.Second)
				continue
			}
			log.Println("worker/association subscribe success: ", listeners)

			time.Sleep(10 * time.Second)
		}
	}()

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

				assauthor, err := w.store.GetEntityByCCID(ctx, association.Author)
				if err != nil {
					log.Printf("worker/association GetEntityByCCID: %v", err)
					continue
				}

				messageAuthor, err := w.client.GetEntity(ctx, w.config.FQDN, association.Owner, nil)
				if err != nil {
					log.Printf("worker/association GetEntity: %v", err)
					continue
				}

				token, err := createToken(messageAuthor.Domain, w.config.ProxyCCID, w.config.ProxyPriv)
				if err != nil {
					log.Printf("worker/association createToken %v", err)
					continue
				}

				msg, err := w.client.GetMessage(ctx, messageAuthor.Domain, association.Target, &client.Options{
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
				}
			}

		case "delete":
			{
				entity, err := w.store.GetEntityByCCID(ctx, association.Author)
				if err != nil {
					log.Printf("worker/association/delete GetEntityByCCID: %v", err)
					continue
				}

				targetEntity, err := w.client.GetEntity(ctx, w.config.FQDN, association.Target, nil)
				if err != nil {
					log.Printf("worker/association/delete GetEntityByCCID: %v", err)
					continue
				}

				token, err := createToken(targetEntity.Domain, w.config.ProxyCCID, w.config.ProxyPriv)
				if err != nil {
					log.Printf("worker/association/delete createToken %v", err)
					continue
				}

				target, err := w.client.GetMessage(ctx, targetEntity.Domain, association.Target, &client.Options{
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
