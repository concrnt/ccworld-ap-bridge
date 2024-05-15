package ap

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/pkg/errors"

	"github.com/concrnt/ccworld-ap-bridge/apclient"
	"github.com/concrnt/ccworld-ap-bridge/store"
	"github.com/concrnt/ccworld-ap-bridge/types"
	"github.com/concrnt/ccworld-ap-bridge/world"
	"github.com/totegamma/concurrent/client"
	"github.com/totegamma/concurrent/core"
)

type Service struct {
	store    *store.Store
	client   client.Client
	apclient apclient.ApClient
	info     types.NodeInfo
	config   types.ApConfig
}

func printJson(v interface{}) {
	b, err := json.Marshal(v)
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(string(b))
}

func NewService(
	store *store.Store,
	client client.Client,
	apclient apclient.ApClient,
	info types.NodeInfo,
	config types.ApConfig,
) *Service {
	return &Service{
		store,
		client,
		apclient,
		info,
		config,
	}
}

func (s *Service) WebFinger(ctx context.Context, resource string) (types.WebFinger, error) {
	ctx, span := tracer.Start(ctx, "Ap.Service.WebFinger")
	defer span.End()

	split := strings.Split(resource, ":")
	if len(split) != 2 {
		return types.WebFinger{}, errors.New("invalid resource")
	}
	rt, id := split[0], split[1]
	if rt != "acct" {
		return types.WebFinger{}, errors.New("invalid resource type")
	}

	split = strings.Split(id, "@")
	if len(split) != 2 {
		return types.WebFinger{}, errors.New("invalid resource")
	}
	username, domain := split[0], split[1]
	if domain != s.config.FQDN {
		return types.WebFinger{}, errors.New("domain not found")
	}
	_, err := s.store.GetEntityByID(ctx, username)
	if err != nil {
		return types.WebFinger{}, err
	}

	return types.WebFinger{
		Subject: resource,
		Links: []types.WebFingerLink{
			{
				Rel:  "self",
				Type: "application/activity+json",
				Href: "https://" + s.config.FQDN + "/ap/acct/" + username,
			},
		},
	}, nil
}

func (s *Service) NodeInfo(ctx context.Context) (types.NodeInfo, error) {
	_, span := tracer.Start(ctx, "Ap.Service.NodeInfo")
	defer span.End()
	return s.info, nil
}

func (s *Service) NodeInfoWellKnown(ctx context.Context) (types.WellKnown, error) {
	_, span := tracer.Start(ctx, "Ap.Service.NodeInfoWellKnown")
	defer span.End()
	return types.WellKnown{
		Links: []types.WellKnownLink{
			{
				Rel:  "http://nodeinfo.diaspora.software/ns/schema/2.0",
				Href: "https://" + s.config.FQDN + "/ap/nodeinfo/2.0",
			},
		},
	}, nil
}

// -

func (s *Service) GetUserWebURL(ctx context.Context, id string) (string, error) {
	ctx, span := tracer.Start(ctx, "Ap.Service.GetUserWebURL")
	defer span.End()

	entity, err := s.store.GetEntityByID(ctx, id)
	if err != nil {
		span.RecordError(err)
		return "", err
	}
	return "https://concurrent.world/entity/" + entity.CCID, nil

}

func (s *Service) User(ctx context.Context, id string) (types.ApObject, error) {
	ctx, span := tracer.Start(ctx, "Ap.Service.User")
	defer span.End()

	entity, err := s.store.GetEntityByID(ctx, id)
	if err != nil {
		span.RecordError(err)
		return types.ApObject{}, err
	}

	profile, err := s.client.GetProfile(ctx, s.config.FQDN, entity.CCID+"/world.concrnt.p")
	if err != nil {
		span.RecordError(err)
		return types.ApObject{}, err
	}

	var profileDocument core.ProfileDocument[world.Profile]
	err = json.Unmarshal([]byte(profile.Document), &profileDocument)
	if err != nil {
		span.RecordError(err)
		return types.ApObject{}, err
	}

	return types.ApObject{
		Context:     "https://www.w3.org/ns/activitystreams",
		Type:        "Person",
		ID:          "https://" + s.config.FQDN + "/ap/acct/" + id,
		Inbox:       "https://" + s.config.FQDN + "/ap/acct/" + id + "/inbox",
		Outbox:      "https://" + s.config.FQDN + "/ap/acct/" + id + "/outbox",
		SharedInbox: "https://" + s.config.FQDN + "/ap/inbox",
		Endpoints: types.PersonEndpoints{
			SharedInbox: "https://" + s.config.FQDN + "/ap/inbox",
		},
		PreferredUsername: id,
		Name:              profileDocument.Body.Username,
		Summary:           profileDocument.Body.Description,
		URL:               "https://" + s.config.FQDN + "/ap/acct/" + id,
		Icon: types.Icon{
			Type:      "Image",
			MediaType: "image/png",
			URL:       profileDocument.Body.Avatar,
		},
		PublicKey: types.Key{
			ID:           "https://" + s.config.FQDN + "/ap/acct/" + id + "#main-key",
			Type:         "Key",
			Owner:        "https://" + s.config.FQDN + "/ap/acct/" + id,
			PublicKeyPem: entity.Publickey,
		},
	}, nil
}

func (s *Service) GetNoteWebURL(ctx context.Context, id string) (string, error) {
	ctx, span := tracer.Start(ctx, "Ap.Service.GetNoteWebURL")
	defer span.End()

	msg, err := s.client.GetMessage(ctx, s.config.ProxyCCID, id)
	if err != nil {
		span.RecordError(err)
		return "", err
	}

	return "https://concurrent.world/message/" + id + "@" + msg.Author, nil
}

func (s *Service) Note(ctx context.Context, id string) (types.ApObject, error) {
	ctx, span := tracer.Start(ctx, "Ap.Service.Note")
	defer span.End()

	note, err := s.MessageToNote(ctx, id)
	if err != nil {
		span.RecordError(err)
		return types.ApObject{}, err
	}

	return note, nil
}

func (s *Service) Inbox(ctx context.Context, object types.ApObject, id string) (types.ApObject, error) {
	ctx, span := tracer.Start(ctx, "Ap.Service.Inbox")
	defer span.End()

	switch object.Type {
	case "Follow":

		if id == "" {
			log.Println("Invalid username")
			return types.ApObject{}, errors.New("Invalid username")
		}

		entity, err := s.store.GetEntityByID(ctx, id)
		if err != nil {
			log.Println("entity not found", err)
			span.RecordError(err)
			return types.ApObject{}, errors.New("entity not found")
		}

		requester, err := s.apclient.FetchPerson(ctx, object.Actor, entity)
		if err != nil {
			log.Println("error fetching person", err)
			span.RecordError(err)
			return types.ApObject{}, errors.New("Invalid request body")
		}
		accept := types.ApObject{
			Context: "https://www.w3.org/ns/activitystreams",
			ID:      "https://" + s.config.FQDN + "/ap/acct/" + id + "/follows/" + url.PathEscape(requester.ID),
			Type:    "Accept",
			Actor:   "https://" + s.config.FQDN + "/ap/acct/" + id,
			Object:  object,
		}

		split := strings.Split(object.Object.(string), "/")
		userID := split[len(split)-1]

		err = s.apclient.PostToInbox(ctx, requester.Inbox, accept, entity)
		if err != nil {
			log.Println("error posting to inbox", err)
			span.RecordError(err)
			return types.ApObject{}, errors.New("Internal server error")
		}

		// check follow already exists
		_, err = s.store.GetFollowerByTuple(ctx, userID, requester.ID)
		if err == nil {
			log.Println("follow already exists")
			return types.ApObject{}, nil
		}

		// save follow
		err = s.store.SaveFollower(ctx, types.ApFollower{
			ID:                  object.ID,
			SubscriberInbox:     requester.Inbox,
			SubscriberPersonURL: requester.ID,
			PublisherUserID:     userID,
		})
		if err != nil {
			log.Println("error saving follow", err)
			span.RecordError(err)
			return types.ApObject{}, errors.New("Internal server error(save follow error)")
		}

		return types.ApObject{}, nil

	case "Like":
		targetID := strings.Replace(object.Object.(string), "https://"+s.config.FQDN+"/ap/note/", "", 1)
		targetMsg, err := s.client.GetMessage(ctx, s.config.ProxyCCID, targetID)
		if err != nil {
			span.RecordError(err)
			return types.ApObject{}, errors.New("message not found")
		}

		err = s.store.CreateApObjectReference(ctx, types.ApObjectReference{
			ApObjectID: object.ID,
			CcObjectID: "",
		})

		if err != nil {
			span.RecordError(err)
			return types.ApObject{}, errors.New("like already exists")
		}

		entity, err := s.store.GetEntityByCCID(ctx, targetMsg.Author)
		if err != nil {
			span.RecordError(err)
			return types.ApObject{}, errors.New("entity not found")
		}

		person, err := s.apclient.FetchPerson(ctx, object.Actor, entity)
		if err != nil {
			span.RecordError(err)
			return types.ApObject{}, errors.New("failed to fetch actor")
		}

		//var obj association.SignedObject
		var doc core.AssociationDocument[world.ReactionAssociation] // ReactionはLikeを包含する

		username := person.Name
		if len(username) == 0 {
			username = person.PreferredUsername
		}

		if (object.Tag == nil) || (object.Tag[0].Name[0] != ':') {
			doc = core.AssociationDocument[world.ReactionAssociation]{
				DocumentBase: core.DocumentBase[world.ReactionAssociation]{
					Signer: s.config.ProxyCCID,
					Type:   "association",
					Schema: world.LikeAssociationSchema,
					Body: world.ReactionAssociation{
						ProfileOverride: world.ProfileOverride{
							Username:    username,
							Avatar:      person.Icon.URL,
							Description: person.Summary,
							Link:        object.Actor,
						},
					},
					Meta: map[string]interface{}{
						"apActor": object.Actor,
					},
					SignedAt: time.Now(),
				},
				Target: targetID,
			}
		} else {
			doc = core.AssociationDocument[world.ReactionAssociation]{
				DocumentBase: core.DocumentBase[world.ReactionAssociation]{
					Signer: s.config.ProxyCCID,
					Type:   "association",
					Schema: world.ReactionAssociationSchema,
					Body: world.ReactionAssociation{
						Shortcode: object.Tag[0].Name,
						ImageURL:  object.Tag[0].Icon.URL,
						ProfileOverride: world.ProfileOverride{
							Username:    username,
							Avatar:      person.Icon.URL,
							Description: person.Summary,
							Link:        object.Actor,
						},
					},
					Meta: map[string]interface{}{
						"apActor": object.Actor,
					},
					SignedAt: time.Now(),
				},
				Target:  targetID,
				Variant: object.Tag[0].Icon.URL,
			}
		}

		document, err := json.Marshal(doc)
		if err != nil {
			span.RecordError(err)
			return types.ApObject{}, errors.New("Internal server error (json marshal error)")
		}

		signature, err := core.SignBytes(document, s.config.ProxyPriv)
		if err != nil {
			span.RecordError(err)
			return types.ApObject{}, errors.New("Internal server error (sign error)")
		}

		var created core.ResponseBase[core.Association]
		_, err = s.client.Commit(ctx, string(document), string(signature), &created)
		if err != nil {
			span.RecordError(err)
			return types.ApObject{}, errors.New("Internal server error (post association error)")
		}

		// save reference
		err = s.store.UpdateApObjectReference(ctx, types.ApObjectReference{
			ApObjectID: object.ID,
			CcObjectID: created.Content.ID,
		})

		return types.ApObject{}, nil

	case "Create":
		createObject, ok := object.Object.(map[string]interface{})
		if !ok {
			log.Println("Invalid create object", object.Object)
			return types.ApObject{}, errors.New("Invalid request body")
		}
		createType, ok := createObject["type"].(string)
		if !ok {
			log.Println("Invalid create object", object.Object)
			return types.ApObject{}, errors.New("Invalid request body")
		}
		createID, ok := createObject["id"].(string)
		if !ok {
			log.Println("Invalid create object", object.Object)
			return types.ApObject{}, errors.New("Invalid request body")
		}
		switch createType {
		case "Note":
			// check if the note is already exists
			_, err := s.store.GetApObjectReferenceByCcObjectID(ctx, createID)
			if err == nil {
				// already exists
				log.Println("note already exists")
				return types.ApObject{}, nil
			}

			// preserve reference
			err = s.store.CreateApObjectReference(ctx, types.ApObjectReference{
				ApObjectID: createID,
				CcObjectID: "",
			})

			if err != nil {
				span.RecordError(err)
				return types.ApObject{}, nil
			}

			// list up follows
			follows, err := s.store.GetFollowsByPublisher(ctx, object.Actor)
			if err != nil {
				log.Println("Internal server error (get follows error)", err)
				span.RecordError(err)
				return types.ApObject{}, errors.New("Internal server error (get follows error)")
			}

			var rep types.ApEntity
			destStreams := []string{}
			for _, follow := range follows {
				entity, err := s.store.GetEntityByID(ctx, follow.SubscriberUserID)
				if err != nil {
					log.Println("Internal server error (get entity error)", err)
					span.RecordError(err)
					continue
				}
				rep = entity
				destStreams = append(destStreams, world.UserApStream+"@"+entity.CCID)
			}

			if len(destStreams) == 0 {
				log.Println("No followers")
				return types.ApObject{}, nil
			}

			person, err := s.apclient.FetchPerson(ctx, object.Actor, rep)
			if err != nil {
				span.RecordError(err)
				return types.ApObject{}, errors.New("failed to fetch actor")
			}

			// convertObject
			noteBytes, err := json.Marshal(createObject)
			if err != nil {
				log.Println("Internal server error (json marshal error)", err)
				span.RecordError(err)
				return types.ApObject{}, errors.New("Internal server error (json marshal error)")
			}

			var note types.ApObject
			err = json.Unmarshal(noteBytes, &note)
			if err != nil {
				log.Println("Internal server error (json unmarshal error)", err)
				span.RecordError(err)
				return types.ApObject{}, errors.New("Internal server error (json unmarshal error)")
			}

			created, err := s.NoteToMessage(ctx, note, person, destStreams)

			// save reference
			err = s.store.UpdateApObjectReference(ctx, types.ApObjectReference{
				ApObjectID: createID,
				CcObjectID: created.ID,
			})

			return types.ApObject{}, nil
		default:
			// print request body
			b, err := json.Marshal(object)
			if err != nil {
				span.RecordError(err)
				return types.ApObject{}, errors.New("Internal server error (json marshal error)")
			}
			log.Println("Unhandled Create Object", string(b))
			return types.ApObject{}, nil
		}

	case "Accept":
		acceptObject, ok := object.Object.(map[string]interface{})
		if !ok {
			log.Println("Invalid accept object", object.Object)
			return types.ApObject{}, errors.New("Invalid request body")
		}
		acceptType, ok := acceptObject["type"].(string)
		if !ok {
			log.Println("Invalid accept object", object.Object)
			return types.ApObject{}, errors.New("Invalid request body")
		}
		switch acceptType {
		case "Follow":
			objectID, ok := acceptObject["id"].(string)
			if !ok {
				log.Println("Invalid accept object", object.Object)
				return types.ApObject{}, errors.New("Invalid request body")
			}
			apFollow, err := s.store.GetFollowByID(ctx, objectID)
			if err != nil {
				span.RecordError(err)
				return types.ApObject{}, errors.New("follow not found")
			}
			apFollow.Accepted = true

			_, err = s.store.UpdateFollow(ctx, apFollow)
			if err != nil {
				span.RecordError(err)
				return types.ApObject{}, errors.New("Internal server error (update follow error)")
			}

			return types.ApObject{}, nil
		default:
			// print request body
			b, err := json.Marshal(object)
			if err != nil {
				span.RecordError(err)
				return types.ApObject{}, errors.New("Internal server error (json marshal error)")
			}
			log.Println("Unhandled accept object", string(b))
			return types.ApObject{}, nil

		}

	case "Undo":
		undoObject, ok := object.Object.(map[string]interface{})
		if !ok {
			log.Println("Invalid undo object", object.Object)
			return types.ApObject{}, errors.New("Invalid request body")
		}
		undoType, ok := undoObject["type"].(string)
		if !ok {
			log.Println("Invalid undo object", object.Object)
			return types.ApObject{}, errors.New("Invalid request body")
		}
		switch undoType {
		case "Follow":

			remote, ok := undoObject["actor"].(string)
			if !ok {
				log.Println("Invalid undo object", object.Object)
				return types.ApObject{}, errors.New("Invalid request body")
			}

			obj, ok := undoObject["object"].(string)
			if !ok {
				log.Println("Invalid undo object", object.Object)
				return types.ApObject{}, errors.New("Invalid request body")
			}

			local := strings.TrimPrefix(obj, "https://"+s.config.FQDN+"/ap/acct/")

			// check follow already deleted
			_, err := s.store.GetFollowerByTuple(ctx, local, remote)
			if err != nil {
				log.Println("follow already undoed", local, remote)
				return types.ApObject{}, nil
			}
			_, err = s.store.RemoveFollower(ctx, local, remote)
			if err != nil {
				log.Println("remove follower failed error", err)
				span.RecordError(err)
			}
			return types.ApObject{}, nil

		case "Like":
			likeID, ok := undoObject["id"].(string)
			if !ok {
				log.Println("Invalid undo object", object.Object)
				return types.ApObject{}, errors.New("Invalid request body")
			}
			deleteRef, err := s.store.GetApObjectReferenceByApObjectID(ctx, likeID)
			if err != nil {
				span.RecordError(err)
				return types.ApObject{}, errors.New("like not found")
			}

			doc := core.DeleteDocument{
				DocumentBase: core.DocumentBase[any]{
					Signer:   s.config.ProxyCCID,
					Type:     "delete",
					SignedAt: time.Now(),
				},
				Target: deleteRef.CcObjectID,
			}

			document, err := json.Marshal(doc)
			if err != nil {
				span.RecordError(err)
				return types.ApObject{}, errors.New("Internal server error (json marshal error)")
			}

			signature, err := core.SignBytes(document, s.config.ProxyPriv)
			if err != nil {
				span.RecordError(err)
				return types.ApObject{}, errors.New("Internal server error (sign error)")
			}

			_, err = s.client.Commit(ctx, string(document), string(signature), nil)
			if err != nil {
				span.RecordError(err)
				return types.ApObject{}, errors.New("Internal server error (delete like error)")
			}

			err = s.store.DeleteApObjectReference(ctx, deleteRef.ApObjectID)
			if err != nil {
				span.RecordError(err)
				return types.ApObject{}, errors.New("Internal server error (delete reference error)")
			}
			return types.ApObject{}, nil

		default:
			// print request body
			b, err := json.Marshal(object)
			if err != nil {
				span.RecordError(err)
				return types.ApObject{}, errors.New("Internal server error (json marshal error)")
			}
			log.Println("Unhandled Undo Object", string(b))
			return types.ApObject{}, nil
		}
	case "Delete":
		deleteObject, ok := object.Object.(map[string]interface{})
		if !ok {
			log.Println("Invalid delete object", object.Object)
			return types.ApObject{}, errors.New("Invalid request body")
		}
		deleteID, ok := deleteObject["id"].(string)
		if !ok {
			log.Println("Invalid delete object", object.Object)
			return types.ApObject{}, errors.New("Invalid request body")
		}

		deleteRef, err := s.store.GetApObjectReferenceByApObjectID(ctx, deleteID)
		if err != nil {
			span.RecordError(err)
			return types.ApObject{}, nil
		}

		doc := core.DeleteDocument{
			DocumentBase: core.DocumentBase[any]{
				Signer:   s.config.ProxyCCID,
				Type:     "delete",
				SignedAt: time.Now(),
			},
			Target: deleteRef.CcObjectID,
		}

		document, err := json.Marshal(doc)
		if err != nil {
			span.RecordError(err)
			return types.ApObject{}, errors.New("Internal server error (json marshal error)")
		}

		signature, err := core.SignBytes(document, s.config.ProxyPriv)
		if err != nil {
			span.RecordError(err)
			return types.ApObject{}, errors.New("Internal server error (sign error)")
		}

		_, err = s.client.Commit(ctx, string(document), string(signature), nil)
		if err != nil {
			span.RecordError(err)
			return types.ApObject{}, errors.New("Internal server error (delete error)")
		}

		err = s.store.DeleteApObjectReference(ctx, deleteRef.ApObjectID)
		if err != nil {
			span.RecordError(err)
			return types.ApObject{}, errors.New("Internal server error (delete error)")
		}
		return types.ApObject{}, nil

	default:
		// print request body
		b, err := json.Marshal(object)
		if err != nil {
			span.RecordError(err)
			return types.ApObject{}, errors.New("Internal server error (json marshal error)")
		}
		log.Println("Unhandled Activitypub Object", string(b))
		return types.ApObject{}, nil
	}

}

func (s Service) MessageToNote(ctx context.Context, messageID string) (types.ApObject, error) {
	ctx, span := tracer.Start(ctx, "MessageToNote")
	defer span.End()

	message, err := s.client.GetMessage(ctx, s.config.FQDN, messageID)
	if err != nil {
		span.RecordError(err)
		return types.ApObject{}, errors.New("message not found")
	}

	authorEntity, err := s.store.GetEntityByCCID(ctx, message.Author)
	if err != nil {
		span.RecordError(err)
		return types.ApObject{}, errors.New("entity not found")
	}

	var document core.MessageDocument[world.MarkdownMessage]
	err = json.Unmarshal([]byte(message.Document), &document)
	if err != nil {
		return types.ApObject{}, errors.New("invalid payload")
	}

	var emojis []types.Tag
	var images []string

	text := document.Body.Body

	// extract image url of markdown notation
	imagePattern := regexp.MustCompile(`!\[.*\]\((.*)\)`)
	matches := imagePattern.FindAllStringSubmatch(text, -1)
	for _, match := range matches {
		images = append(images, match[1])
	}

	// remove markdown notation
	text = imagePattern.ReplaceAllString(text, "")

	if len(document.Body.Emojis) > 0 {
		for k, v := range document.Body.Emojis {
			//imageURL, ok := v.(map[string]interface{})["imageURL"].(string)
			emoji := types.Tag{
				ID:   v.ImageURL,
				Type: "Emoji",
				Name: ":" + k + ":",
				Icon: types.Icon{
					Type:      "Image",
					MediaType: "image/png",
					URL:       v.ImageURL,
				},
			}
			emojis = append(emojis, emoji)
		}
	}

	attachments := []types.Attachment{}
	for _, imageURL := range images {
		attachment := types.Attachment{
			Type:      "Document",
			MediaType: "image/png",
			URL:       imageURL,
		}
		attachments = append(attachments, attachment)
	}

	if document.Schema == world.MarkdownMessageSchema { // Note

		return types.ApObject{
			Context:      "https://www.w3.org/ns/activitystreams",
			Type:         "Note",
			ID:           "https://" + s.config.FQDN + "/ap/note/" + message.ID,
			AttributedTo: "https://" + s.config.FQDN + "/ap/acct/" + authorEntity.ID,
			Content:      text,
			Published:    document.SignedAt.Format(time.RFC3339),
			To:           []string{"https://www.w3.org/ns/activitystreams#Public"},
			Tag:          emojis,
			Attachment:   attachments,
		}, nil

	} else if document.Schema == world.ReplyMessageSchema { // Reply

		var replyDocument core.MessageDocument[world.ReplyMessage]
		err = json.Unmarshal([]byte(message.Document), &replyDocument)
		if err != nil {
			return types.ApObject{}, errors.New("invalid payload")
		}

		replyAuthor, err := s.client.GetEntity(ctx, s.config.ProxyCCID, replyDocument.Body.ReplyToMessageAuthor)
		if err != nil {
			span.RecordError(err)
			return types.ApObject{}, errors.New("entity not found")
		}

		replySource, err := s.client.GetMessage(ctx, replyAuthor.Domain, replyDocument.Body.ReplyToMessageID)
		if err != nil {
			span.RecordError(err)
			return types.ApObject{}, errors.New("message not found")
		}

		var sourceDocument core.MessageDocument[world.MarkdownMessage]
		err = json.Unmarshal([]byte(replySource.Document), &sourceDocument)
		if err != nil {
			return types.ApObject{}, errors.New("invalid payload")
		}

		replyMeta, ok := sourceDocument.Meta.(map[string]interface{})
		if !ok {
			return types.ApObject{}, errors.New("invalid meta")
		}

		ref, ok := replyMeta["apObjectRef"].(string)
		if !ok {
			ref = "https://" + replyAuthor.Domain + "/ap/note/" + replyDocument.Body.ReplyToMessageID
		}

		return types.ApObject{
			Context:      "https://www.w3.org/ns/activitystreams",
			Type:         "Note",
			ID:           "https://" + s.config.FQDN + "/ap/note/" + message.ID,
			AttributedTo: "https://" + s.config.FQDN + "/ap/acct/" + authorEntity.ID,
			Content:      text,
			InReplyTo:    ref,
			To:           []string{"https://www.w3.org/ns/activitystreams#Public"},
		}, nil

	} else if document.Schema == world.RerouteMessageSchema { // Boost or Quote

		var rerouteDocument core.MessageDocument[world.RerouteMessage]
		err = json.Unmarshal([]byte(message.Document), &rerouteDocument)
		if err != nil {
			return types.ApObject{}, errors.New("invalid payload")
		}

		rerouteAuthor, err := s.client.GetEntity(ctx, s.config.ProxyCCID, rerouteDocument.Body.RerouteToMessageAuthor)
		if err != nil {
			span.RecordError(err)
			return types.ApObject{}, errors.New("entity not found")
		}

		rerouteSource, err := s.client.GetMessage(ctx, rerouteAuthor.Domain, rerouteDocument.Body.RerouteToMessageID)
		if err != nil {
			span.RecordError(err)
			return types.ApObject{}, errors.New("message not found")
		}

		var sourceDocument core.MessageDocument[world.MarkdownMessage]
		err = json.Unmarshal([]byte(rerouteSource.Document), &sourceDocument)
		if err != nil {
			return types.ApObject{}, errors.New("invalid payload")
		}

		rerouteMeta, ok := sourceDocument.Meta.(map[string]interface{})
		if !ok {
			return types.ApObject{}, errors.New("invalid meta")
		}

		ref, ok := rerouteMeta["apObjectRef"].(string)
		if !ok {
			ref = "https://" + rerouteAuthor.Domain + "/ap/note/" + rerouteDocument.Body.RerouteToMessageID
		}

		if text == "" {
			return types.ApObject{
				Context: "https://www.w3.org/ns/activitystreams",
				Type:    "Announce",
				ID:      "https://" + s.config.FQDN + "/ap/note/" + message.ID,
				Object:  ref,
			}, nil
		}

		return types.ApObject{
			Context:      "https://www.w3.org/ns/activitystreams",
			Type:         "Note",
			ID:           "https://" + s.config.FQDN + "/ap/note/" + message.ID,
			AttributedTo: "https://" + s.config.FQDN + "/ap/acct/" + authorEntity.ID,
			Content:      text,
			QuoteURL:     ref,
			To:           []string{"https://www.w3.org/ns/activitystreams#Public"},
		}, nil
	} else {
		return types.ApObject{}, errors.New("invalid schema")
	}
}

func (s Service) NoteToMessage(ctx context.Context, object types.ApObject, person types.ApObject, destStreams []string) (core.Message, error) {

	content := object.Content

	for _, attachment := range object.Attachment {
		if attachment.Type == "Document" {
			content += "\n\n![image](" + attachment.URL + ")"
		}
	}

	var emojis map[string]world.Emoji = make(map[string]world.Emoji)
	for _, tag := range object.Tag {
		if tag.Type == "Emoji" {
			name := strings.Trim(tag.Name, ":")
			emojis[name] = world.Emoji{
				ImageURL: tag.Icon.URL,
			}
		}
	}

	if len(content) == 0 {
		return core.Message{}, errors.New("empty note")
	}

	if len(content) > 4096 {
		return core.Message{}, errors.New("note too long")
	}

	if object.Sensitive {
		summary := "CW"
		if object.Summary != "" {
			summary = object.Summary
		}
		content = "<details>\n<summary>" + summary + "</summary>\n" + content + "\n</details>"
	}

	username := person.Name
	if len(username) == 0 {
		username = person.PreferredUsername
	}

	date, err := time.Parse(time.RFC3339Nano, object.Published)
	if err != nil {
		date = time.Now()
	}

	doc := core.MessageDocument[world.MarkdownMessage]{
		DocumentBase: core.DocumentBase[world.MarkdownMessage]{
			Signer: s.config.ProxyCCID,
			Type:   "message",
			Schema: world.MarkdownMessageSchema,
			Body: world.MarkdownMessage{
				Body: content,
				ProfileOverride: world.ProfileOverride{
					Username:    username,
					Avatar:      person.Icon.URL,
					Description: person.Summary,
					Link:        person.URL,
				},
				Emojis: emojis,
			},
			Meta: map[string]interface{}{
				"apActor":          person.URL,
				"apObjectRef":      object.ID,
				"apPublisherInbox": person.Inbox,
			},
			SignedAt: date,
		},
	}

	document, err := json.Marshal(doc)
	if err != nil {
		return core.Message{}, errors.Wrap(err, "json marshal error")
	}

	signature, err := core.SignBytes(document, s.config.ProxyPriv)
	if err != nil {
		return core.Message{}, errors.Wrap(err, "sign error")
	}

	var created core.ResponseBase[core.Message]
	_, err = s.client.Commit(ctx, string(document), string(signature), &created)
	if err != nil {
		return core.Message{}, err
	}

	return created.Content, nil
}
