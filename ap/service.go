package ap

import (
	"context"
	"errors"
	"strings"

	"github.com/concrnt/ccworld-ap-bridge/store"
	"github.com/concrnt/ccworld-ap-bridge/types"
	// "github.com/concrnt/ccworld-ap-bridge/apclient"
	"github.com/totegamma/concurrent/client"
)

type Service struct {
	store  *store.Store
	client *client.Client
	// apclient apclient.ApClient
	info   types.NodeInfo
	config types.ApConfig
}

func NewService(store *store.Store, client *client.Client, info types.NodeInfo, config types.ApConfig) *Service {
	return &Service{
		store,
		client,
		// apclient: apclient,
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

func (s *Service) User(ctx context.Context, id string) (types.ApObject, error) {
	ctx, span := tracer.Start(ctx, "Ap.Service.User")
	defer span.End()

	entity, err := s.store.GetEntityByID(ctx, id)
	if err != nil {
		span.RecordError(err)
		return types.ApObject{}, err
	}

	person, err := s.store.GetPersonByID(ctx, id)
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
		Name:              person.Name,
		Summary:           person.Summary,
		URL:               "https://" + s.config.FQDN + "/ap/acct/" + id,
		Icon: types.Icon{
			Type:      "Image",
			MediaType: "image/png",
			URL:       person.IconURL,
		},
		PublicKey: types.Key{
			ID:           "https://" + s.config.FQDN + "/ap/acct/" + id + "#main-key",
			Type:         "Key",
			Owner:        "https://" + s.config.FQDN + "/ap/acct/" + id,
			PublicKeyPem: entity.Publickey,
		},
	}, nil
}

/*
func (s *Service) Note(ctx context.Context, id string) (types.ApObject, error) {
    ctx, span := tracer.Start(ctx, "Ap.Service.Note")
    defer span.End()

	msg, err := s.client.GetMessage(ctx, s.apconfig.ProxyCCID, id)
	if err != nil {
		span.RecordError(err)
        return types.ApObject{}, err
	}

	note, err := h.MessageToNote(ctx, id)
	if err != nil {
		span.RecordError(err)
        return types.ApObject{}, err
	}

    return note, nil
}
*/

/*
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
		targetID := strings.Replace(object.Object.(string), "https://"+h.config.FQDN+"/ap/note/", "", 1)
		targetMsg, err := h.client.GetMessage(ctx, h.apconfig.ProxyCCID, targetID)
		if err != nil {
			span.RecordError(err)
			return c.String(http.StatusOK, "message not found")
		}

		err = h.store.CreateApObjectReference(ctx, ApObjectReference{
			ApObjectID: object.ID,
			CcObjectID: "",
		})

		if err != nil {
			span.RecordError(err)
			return c.String(http.StatusOK, "like already exists")
		}

		entity, err := h.store.GetEntityByCCID(ctx, targetMsg.Author)
		if err != nil {
			span.RecordError(err)
			return c.String(http.StatusOK, "entity not found")
		}

		person, err := h.FetchPerson(ctx, object.Actor, entity)
		if err != nil {
			span.RecordError(err)
			return c.String(http.StatusOK, "failed to fetch actor")
		}

		var obj association.SignedObject

		username := person.Name
		if len(username) == 0 {
			username = person.PreferredUsername
		}

		if (object.Tag == nil) || (object.Tag[0].Name[0] != ':') {
			obj = association.SignedObject{
				Signer: h.apconfig.ProxyCCID,
				Type:   "Association",
				Schema: "https://raw.githubusercontent.com/totegamma/concurrent-schemas/master/associations/like/0.0.1.json",
				Body: map[string]interface{}{
					"profileOverride": map[string]interface{}{
						"username":    username,
						"avatar":      person.Icon.URL,
						"description": person.Summary,
						"link":        object.Actor,
					},
				},
				Meta: map[string]interface{}{
					"apActor": object.Actor,
				},
				SignedAt: time.Now(),
				Target:   targetID,
			}
		} else {
			obj = association.SignedObject{
				Signer: h.apconfig.ProxyCCID,
				Type:   "Association",
				Schema: "https://raw.githubusercontent.com/totegamma/concurrent-schemas/master/associations/emoji/0.0.1.json",
				Body: map[string]interface{}{
					"shortcode": object.Tag[0].Name,
					"imageUrl":  object.Tag[0].Icon.URL,
					"profileOverride": map[string]interface{}{
						"username":    username,
						"avatar":      person.Icon.URL,
						"description": person.Summary,
						"link":        object.Actor,
					},
				},
				Meta: map[string]interface{}{
					"apActor": object.Actor,
				},
				SignedAt: time.Now(),
				Target:   targetID,
				Variant:  object.Tag[0].Icon.URL,
			}
		}

		objb, err := json.Marshal(obj)
		if err != nil {
			span.RecordError(err)
			return c.String(http.StatusOK, "Internal server error (json marshal error)")
		}

		objstr := string(objb)
		objsig, err := util.SignBytes(objb, h.apconfig.Proxy.PrivateKey)
		if err != nil {
			span.RecordError(err)
			return c.String(http.StatusOK, "Internal server error (sign error)")
		}

		created, err := h.client.Commit(ctx, objstr, objsig)
		if err != nil {
			span.RecordError(err)
			return c.String(http.StatusOK, "Internal server error (post association error)")
		}

		// save reference
		err = h.store.UpdateApObjectReference(ctx, ApObjectReference{
			ApObjectID: object.ID,
			CcObjectID: created.ID,
		})

		return c.String(http.StatusOK, "like accepted")

	case "Create":
		createObject, ok := object.Object.(map[string]interface{})
		if !ok {
			log.Println("Invalid create object", object.Object)
			return c.String(http.StatusBadRequest, "Invalid request body")
		}
		createType, ok := createObject["type"].(string)
		if !ok {
			log.Println("Invalid create object", object.Object)
			return c.String(http.StatusBadRequest, "Invalid request body")
		}
		createID, ok := createObject["id"].(string)
		if !ok {
			log.Println("Invalid create object", object.Object)
			return c.String(http.StatusBadRequest, "Invalid request body")
		}
		switch createType {
		case "Note":
			// check if the note is already exists
			_, err := h.store.GetApObjectReferenceByCcObjectID(ctx, createID)
			if err == nil {
				// already exists
				log.Println("note already exists")
				return c.String(http.StatusOK, "note already exists")
			}

			// preserve reference
			err = h.store.CreateApObjectReference(ctx, ApObjectReference{
				ApObjectID: createID,
				CcObjectID: "",
			})

			if err != nil {
				span.RecordError(err)
				return c.String(http.StatusOK, "note already exists")
			}

			// list up follows
			follows, err := h.store.GetFollowsByPublisher(ctx, object.Actor)
			if err != nil {
				log.Println("Internal server error (get follows error)", err)
				span.RecordError(err)
				return c.String(http.StatusInternalServerError, "Internal server error (get follows error)")
			}

			var rep ApEntity
			destStreams := []string{}
			for _, follow := range follows {
				entity, err := h.store.GetEntityByID(ctx, follow.SubscriberUserID)
				if err != nil {
					log.Println("Internal server error (get entity error)", err)
					span.RecordError(err)
					continue
				}
				rep = entity
				destStreams = append(destStreams, entity.FollowStream)
			}

			if len(destStreams) == 0 {
				log.Println("No followers")
				return c.String(http.StatusOK, "No followers")
			}

			person, err := h.FetchPerson(ctx, object.Actor, rep)
			if err != nil {
				span.RecordError(err)
				return c.String(http.StatusBadRequest, "failed to fetch actor")
			}

			// convertObject
			noteBytes, err := json.Marshal(createObject)
			if err != nil {
				log.Println("Internal server error (json marshal error)", err)
				span.RecordError(err)
				return c.String(http.StatusInternalServerError, "Internal server error (json marshal error)")
			}
			var note Note
			err = json.Unmarshal(noteBytes, &note)
			if err != nil {
				log.Println("Internal server error (json unmarshal error)", err)
				span.RecordError(err)
				return c.String(http.StatusInternalServerError, "Internal server error (json unmarshal error)")
			}

			created, err := h.NoteToMessage(ctx, note, person, destStreams)

			// save reference
			err = h.store.UpdateApObjectReference(ctx, ApObjectReference{
				ApObjectID: createID,
				CcObjectID: created.ID,
			})

			return c.String(http.StatusOK, "note accepted")
		default:
			// print request body
			b, err := json.Marshal(object)
			if err != nil {
				span.RecordError(err)
				return c.String(http.StatusInternalServerError, "Internal server error (json marshal error)")
			}
			log.Println("Unhandled Create Object", string(b))
			return c.String(http.StatusOK, "OK but not implemented")
		}

	case "Accept":
		acceptObject, ok := object.Object.(map[string]interface{})
		if !ok {
			log.Println("Invalid accept object", object.Object)
			return c.String(http.StatusBadRequest, "Invalid request body")
		}
		acceptType, ok := acceptObject["type"].(string)
		if !ok {
			log.Println("Invalid accept object", object.Object)
			return c.String(http.StatusBadRequest, "Invalid request body")
		}
		switch acceptType {
		case "Follow":
			objectID, ok := acceptObject["id"].(string)
			if !ok {
				log.Println("Invalid accept object", object.Object)
				return c.String(http.StatusBadRequest, "Invalid request body")
			}
			apFollow, err := h.store.GetFollowByID(ctx, objectID)
			if err != nil {
				span.RecordError(err)
				return c.String(http.StatusNotFound, "follow not found")
			}
			apFollow.Accepted = true

			_, err = h.store.UpdateFollow(ctx, apFollow)
			if err != nil {
				span.RecordError(err)
				return c.String(http.StatusInternalServerError, "Internal server error (update follow error)")
			}

			return c.String(http.StatusOK, "follow accepted")
		default:
			// print request body
			b, err := json.Marshal(object)
			if err != nil {
				span.RecordError(err)
				return c.String(http.StatusInternalServerError, "Internal server error (json marshal error)")
			}
			log.Println("Unhandled accept object", string(b))
			return c.String(http.StatusOK, "OK but not implemented")

		}

	case "Undo":
		undoObject, ok := object.Object.(map[string]interface{})
		if !ok {
			log.Println("Invalid undo object", object.Object)
			return c.String(http.StatusBadRequest, "Invalid request body")
		}
		undoType, ok := undoObject["type"].(string)
		if !ok {
			log.Println("Invalid undo object", object.Object)
			return c.String(http.StatusBadRequest, "Invalid request body")
		}
		switch undoType {
		case "Follow":

			remote, ok := undoObject["actor"].(string)
			if !ok {
				log.Println("Invalid undo object", object.Object)
				return c.String(http.StatusBadRequest, "Invalid request body")
			}

			obj, ok := undoObject["object"].(string)
			if !ok {
				log.Println("Invalid undo object", object.Object)
				return c.String(http.StatusBadRequest, "Invalid request body")
			}

			local := strings.TrimPrefix(obj, "https://"+h.config.FQDN+"/ap/acct/")

			// check follow already deleted
			_, err = h.store.GetFollowerByTuple(ctx, local, remote)
			if err != nil {
				log.Println("follow already undoed", local, remote)
				return c.String(http.StatusOK, "follow already undoed")
			}
			_, err = h.store.RemoveFollower(ctx, local, remote)
			if err != nil {
				log.Println("remove follower failed error", err)
				span.RecordError(err)
			}
			return c.String(http.StatusOK, "OK")

		case "Like":
			likeID, ok := undoObject["id"].(string)
			if !ok {
				log.Println("Invalid undo object", object.Object)
				return c.String(http.StatusOK, "Invalid request body")
			}
			deleteRef, err := h.store.GetApObjectReferenceByApObjectID(ctx, likeID)
			if err != nil {
				span.RecordError(err)
				return c.String(http.StatusNotFound, "like not found")
			}

			_, err = h.client.Commit(ctx, deleteRef.CcObjectID, h.apconfig.ProxyCCID) // TODO: ちゃんとdocumentを与える
			if err != nil {
				span.RecordError(err)
				return c.String(http.StatusInternalServerError, "Internal server error (delete like error)")
			}

			err = h.store.DeleteApObjectReference(ctx, deleteRef.ApObjectID)
			if err != nil {
				span.RecordError(err)
				return c.String(http.StatusInternalServerError, "Internal server error (delete reference error)")
			}
			return c.String(http.StatusOK, "like undoed")

		default:
			// print request body
			b, err := json.Marshal(object)
			if err != nil {
				span.RecordError(err)
				return c.String(http.StatusInternalServerError, "Internal server error (json marshal error)")
			}
			log.Println("Unhandled Undo Object", string(b))
			return c.String(http.StatusOK, "OK but not implemented")
		}
	case "Delete":
		deleteObject, ok := object.Object.(map[string]interface{})
		if !ok {
			log.Println("Invalid delete object", object.Object)
			return c.String(http.StatusOK, "Invalid request body")
		}
		deleteID, ok := deleteObject["id"].(string)
		if !ok {
			log.Println("Invalid delete object", object.Object)
			return c.String(http.StatusOK, "Invalid request body")
		}

		deleteRef, err := h.store.GetApObjectReferenceByApObjectID(ctx, deleteID)
		if err != nil {
			span.RecordError(err)
			return c.String(http.StatusOK, "Object Already Deleted")
		}

		_, err = h.client.Commit(ctx, deleteRef.CcObjectID)
		if err != nil {
			span.RecordError(err)
			return c.String(http.StatusInternalServerError, "Internal server error (delete error)")
		}

		err = h.store.DeleteApObjectReference(ctx, deleteRef.ApObjectID)
		if err != nil {
			span.RecordError(err)
			return c.String(http.StatusInternalServerError, "Internal server error (delete error)")
		}
		return c.String(http.StatusOK, "Deleted")

	default:
		// print request body
		b, err := json.Marshal(object)
		if err != nil {
			span.RecordError(err)
			return c.String(http.StatusInternalServerError, "Internal server error (json marshal error)")
		}
		log.Println("Unhandled Activitypub Object", string(b))
		return c.String(http.StatusOK, "OK but not implemented")
	}

}
*/
