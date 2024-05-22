package worker

import (
	"context"
	"encoding/json"
	"log"

	"github.com/totegamma/concurrent/core"

	"github.com/concrnt/ccworld-ap-bridge/types"
	"github.com/concrnt/ccworld-ap-bridge/world"
)

func (w *Worker) StartAssociationWorker() {

	ctx := context.Background()
	notificationStream := world.UserNotifyStream + "@" + w.config.ProxyCCID
	timeline, err := w.client.GetTimeline(ctx, w.config.FQDN, notificationStream)
	if err != nil {
		log.Printf("error: %v", err)
		return
	}

	normalized := timeline.ID + "@" + w.config.FQDN

	pubsub := w.rdb.Subscribe(ctx)
	pubsub.Subscribe(ctx, normalized)

	for {
		pubsubMsg, err := pubsub.ReceiveMessage(ctx)
		if err != nil {
			log.Printf("error: %v", err)
			continue
		}

		log.Printf("received association: %v", pubsubMsg.Payload)

		var streamEvent core.Event
		err = json.Unmarshal([]byte(pubsubMsg.Payload), &streamEvent)
		if err != nil {
			log.Printf("error: %v", err)
			continue
		}

		associationID := streamEvent.Item.ResourceID

		association, err := w.client.GetAssociation(ctx, w.config.FQDN, associationID)
		if err != nil {
			log.Printf("error: %v", err)
		}

		if association.Target[0] != 'm' { // assert association target is message
			continue
		}

		assauthor, err := w.store.GetEntityByCCID(ctx, association.Author) // TODO: handle remote
		if err != nil {
			log.Printf("get ass author entity failed: %v", err)
			continue
		}

		msg, err := w.client.GetMessage(ctx, w.config.FQDN, association.Target)
		if err != nil {
			log.Printf("error: %v", err)
			continue
		}

		var messageDoc core.MessageDocument[world.MarkdownMessage]
		err = json.Unmarshal([]byte(msg.Document), &messageDoc)
		if err != nil {
			log.Printf("error: %v", err)
			continue
		}

		msgMeta, ok := messageDoc.Meta.(map[string]interface{})
		ref, ok := msgMeta["apObjectRef"].(string)
		if !ok {
			log.Printf("target Message is not activitypub message")
			continue
		}
		dest, ok := msgMeta["apPublisherInbox"].(string)
		if !ok {
			log.Printf("target Message is not activitypub message")
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
				log.Printf("error: %v", err)
				continue
			}
			break
		case world.ReactionAssociationSchema:
			var reactionDoc core.AssociationDocument[world.ReactionAssociation]
			err = json.Unmarshal([]byte(association.Document), &reactionDoc)
			if err != nil {
				log.Printf("error: %v", err)
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
				log.Printf("error: %v", err)
				continue
			}
		case world.ReplyAssociationSchema:
			var replyDoc core.AssociationDocument[world.ReplyAssociation]
			err = json.Unmarshal([]byte(association.Document), &replyDoc)
			if err != nil {
				log.Printf("error: %v", err)
				continue
			}

			reply, err := w.client.GetMessage(ctx, w.config.FQDN, replyDoc.Body.MessageID) // TODO: handle remote
			if err != nil {
				log.Printf("error: %v", err)
				continue
			}

			var replyMessage core.MessageDocument[world.ReplyMessage]
			err = json.Unmarshal([]byte(reply.Document), &replyMessage)
			if err != nil {
				log.Printf("error: %v", err)
				continue
			}

			create := types.ApObject{
				Context: []string{"https://www.w3.org/ns/activitystreams"},
				Type:    "Create",
				ID:      "https://" + w.config.FQDN + "/ap/note/" + replyDoc.Body.MessageID + "/activity",
				Actor:   "https://" + w.config.FQDN + "/ap/acct/" + replyDoc.Body.MessageAuthor,
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
				log.Printf("error: %v", err)
				continue
			}

		case world.RerouteAssociationSchema:
			var rerouteDoc core.AssociationDocument[world.RerouteAssociation]
			err = json.Unmarshal([]byte(association.Document), &rerouteDoc)
			if err != nil {
				log.Printf("error: %v", err)
				continue
			}

			announce := types.ApObject{
				Context: []string{"https://www.w3.org/ns/activitystreams"},
				Type:    "Announce",
				ID:      "https://" + w.config.FQDN + "/ap/note/" + rerouteDoc.Body.MessageID,
				Actor:   "https://" + w.config.FQDN + "/ap/acct/" + assauthor.ID,
				Content: "",
				Object:  ref,
			}
			err = w.apclient.PostToInbox(ctx, dest, announce, assauthor)
			if err != nil {
				log.Printf("error: %v", err)
				continue
			}
		}
	}
}
