package ap

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"

	"github.com/concrnt/ccworld-ap-bridge/apclient"
	"github.com/concrnt/ccworld-ap-bridge/bridge"
	"github.com/concrnt/ccworld-ap-bridge/store"
	"github.com/concrnt/ccworld-ap-bridge/types"
	"github.com/concrnt/ccworld-ap-bridge/world"
	"github.com/totegamma/concurrent/client"
	"github.com/totegamma/concurrent/core"
	"github.com/totegamma/concurrent/x/jwt"
)

type Service struct {
	store    *store.Store
	client   client.Client
	apclient *apclient.ApClient
	bridge   *bridge.Service
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

func NewService(
	store *store.Store,
	client client.Client,
	apclient *apclient.ApClient,
	bridge *bridge.Service,
	info types.NodeInfo,
	config types.ApConfig,
) *Service {
	return &Service{
		store,
		client,
		apclient,
		bridge,
		info,
		config,
	}
}

func jsonPrint(title string, v interface{}) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println("----- : " + title + " : -----")
	fmt.Println(string(b))
	fmt.Println("--------------------------------")
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

	s.info.Metadata.ProxyCCID = s.config.ProxyCCID

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
	return "https://concrnt.world/" + entity.CCID, nil

}

func (s *Service) User(ctx context.Context, id string) (types.ApObject, error) {
	ctx, span := tracer.Start(ctx, "Ap.Service.User")
	defer span.End()

	entity, err := s.store.GetEntityByID(ctx, id)
	if err != nil {
		span.RecordError(err)
		return types.ApObject{}, err
	}

	profile, err := s.client.GetProfile(ctx, s.config.FQDN, entity.CCID+"/world.concrnt.p", nil)
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
		Endpoints: &types.PersonEndpoints{
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
		PublicKey: &types.Key{
			ID:           "https://" + s.config.FQDN + "/ap/acct/" + id + "#main-key",
			Type:         "Key",
			Owner:        "https://" + s.config.FQDN + "/ap/acct/" + id,
			PublicKeyPem: entity.Publickey,
		},
		AlsoKnownAs: entity.AlsoKnownAs,
	}, nil
}

func (s *Service) GetNoteWebURL(ctx context.Context, id string) (string, error) {
	ctx, span := tracer.Start(ctx, "Ap.Service.GetNoteWebURL")
	defer span.End()

	msg, err := s.client.GetMessage(ctx, s.config.FQDN, id, nil)
	if err != nil {
		span.RecordError(err)
		return "", err
	}

	return "https://concrnt.world/" + msg.Author + "/" + id, nil
}

func (s *Service) Note(ctx context.Context, id string) (types.ApObject, error) {
	ctx, span := tracer.Start(ctx, "Ap.Service.Note")
	defer span.End()

	note, err := s.bridge.MessageToNote(ctx, id)
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
		token, err := createToken(s.config.FQDN, s.config.ProxyCCID, s.config.ProxyPriv)
		if err != nil {
			log.Printf("failed to generate token %v", err)
			return types.ApObject{}, errors.New("Internal server error (generate token error)")
		}
		targetMsg, err := s.client.GetMessage(ctx, s.config.FQDN, targetID, &client.Options{
			AuthToken: token,
		})
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
					Owner:  targetMsg.Author,
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
					Owner:  targetMsg.Author,
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

		signatureBytes, err := core.SignBytes(document, s.config.ProxyPriv)
		if err != nil {
			span.RecordError(err)
			return types.ApObject{}, errors.New("Internal server error (sign error)")
		}

		signature := hex.EncodeToString(signatureBytes)

		commitObj := core.Commit{
			Document:  string(document),
			Signature: string(signature),
		}

		commit, err := json.Marshal(commitObj)
		if err != nil {
			span.RecordError(err)
			return types.ApObject{}, errors.New("Internal server error (json marshal error)")
		}

		var created core.ResponseBase[core.Association]
		_, err = s.client.Commit(ctx, s.config.FQDN, string(commit), &created, nil)
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

			created, err := s.bridge.NoteToMessage(ctx, note, person, destStreams)

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

			signatureBytes, err := core.SignBytes(document, s.config.ProxyPriv)
			if err != nil {
				span.RecordError(err)
				return types.ApObject{}, errors.New("Internal server error (sign error)")
			}

			signature := hex.EncodeToString(signatureBytes)

			commitObj := core.Commit{
				Document:  string(document),
				Signature: string(signature),
			}

			commit, err := json.Marshal(commitObj)
			if err != nil {
				span.RecordError(err)
				return types.ApObject{}, errors.New("Internal server error (json marshal error)")
			}

			_, err = s.client.Commit(ctx, s.config.FQDN, string(commit), nil, nil)
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
			jsonPrint("Unhandled Undo Object", object)
			return types.ApObject{}, nil
		}
	case "Delete":
		deleteObject, ok := object.Object.(map[string]interface{})
		if !ok {
			jsonPrint("Invalid delete object", object)
			return types.ApObject{}, nil
		}
		deleteID, ok := deleteObject["id"].(string)
		if !ok {
			jsonPrint("Invalid delete object", object)
			return types.ApObject{}, nil
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

		signatureBytes, err := core.SignBytes(document, s.config.ProxyPriv)
		if err != nil {
			span.RecordError(err)
			return types.ApObject{}, errors.New("Internal server error (sign error)")
		}

		signature := hex.EncodeToString(signatureBytes)

		commitObj := core.Commit{
			Document:  string(document),
			Signature: string(signature),
		}

		commit, err := json.Marshal(commitObj)
		if err != nil {
			span.RecordError(err)
			return types.ApObject{}, errors.New("Internal server error (json marshal error)")
		}

		_, err = s.client.Commit(ctx, s.config.FQDN, string(commit), nil, nil)
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
		jsonPrint("Unhandled Activitypub Object", object)
		return types.ApObject{}, nil
	}

}
